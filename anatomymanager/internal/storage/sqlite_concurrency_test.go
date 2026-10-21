package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// openRaw 打开一个不走 NewStore 的连接，用于构造指定 DSN 的对照实验。
func openRaw(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open %q: %v", dsn, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS probe (id INTEGER PRIMARY KEY, v TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

// holdWriteLock 在独立协程里持写锁 hold 时长，模拟另一个进程（GUI 或并行 ingest）正在写。
func holdWriteLock(t *testing.T, db *sql.DB, hold time.Duration) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO probe (v) VALUES ('holder')"); err != nil {
		t.Fatalf("holder insert: %v", err)
	}
	go func() {
		time.Sleep(hold)
		_ = tx.Commit()
	}()
}

// TestConcurrentWriteWithDSNGuards 验证 NewStore 统一 DSN 后，第二个写者会等待锁释放
// 而不是立刻报 database is locked（GUI 内嵌服务与 CLI 并发共用同一库的回归保护）。
func TestConcurrentWriteWithDSNGuards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.db")
	holder := openRaw(t, path+sqliteDSNParams)
	waiter := openRaw(t, path+sqliteDSNParams)

	hold := 1200 * time.Millisecond
	holdWriteLock(t, holder, hold)

	start := time.Now()
	_, err := waiter.Exec("INSERT INTO probe (v) VALUES ('waiter')")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("带 busy_timeout 的写者不应失败: %v（耗时 %v）", err, elapsed)
	}
	if elapsed < hold/2 {
		t.Fatalf("写者未等待持锁方释放，耗时仅 %v（应 >= %v）", elapsed, hold/2)
	}
}

// TestConcurrentWriteWithoutDSNGuards 是对照组：裸 DSN（上游默认 busy_timeout=0）
// 会在锁被占用时直接失败，证明上面的测试确实在验证 DSN 参数而非偶然通过。
func TestConcurrentWriteWithoutDSNGuards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bare.db")
	holder := openRaw(t, path)
	waiter := openRaw(t, path)

	holdWriteLock(t, holder, 1200*time.Millisecond)

	start := time.Now()
	_, err := waiter.Exec("INSERT INTO probe (v) VALUES ('waiter')")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("裸 DSN 预期写冲突失败，实际成功（耗时 %v）", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("裸 DSN 应立即失败，实际耗时 %v", elapsed)
	}
	t.Logf("裸 DSN 如预期立即失败: %v", err)
}

// TestNewStoreAppliesConcurrencyPragmas 直接校验 NewStore 产出的连接确实带上了
// WAL 与 busy_timeout（防止后续有人把 DSN 参数删掉而没有测试报警）。
func TestNewStoreAppliesConcurrencyPragmas(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "pragma.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	var journalMode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode 期望 wal，实际 %q", journalMode)
	}

	var busyTimeout int
	if err := store.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout 期望 5000，实际 %d", busyTimeout)
	}
}
