package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Target 表示一个备份目标规则
type Target struct {
	ID              int64     `json:"id"`
	SourcePath      string    `json:"source_path"`
	SourceDir       string    `json:"source_dir"`
	FileName        string    `json:"file_name"`
	MappedDir       string    `json:"mapped_dir"`
	MaxSizeLimit    int64     `json:"max_size_limit"`   // 字节数，0 表示使用系统默认
	ScheduleEnabled bool      `json:"schedule_enabled"` // 是否开启定时备份
	KeepVersions    int       `json:"keep_versions"`    // 保留最大版本数，0 表示不限制
	LastBackupAt    *time.Time `json:"last_backup_at"`
	LastHash        string    `json:"last_hash"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Snapshot 表示历史快照记录
type Snapshot struct {
	ID               int64     `json:"id"`
	TargetID         int64     `json:"target_id"`
	SourcePath       string    `json:"source_path"`
	SnapshotFilename string    `json:"snapshot_filename"`
	FullNutstorePath string    `json:"full_nutstore_path"`
	FileSizeBytes    int64     `json:"file_size_bytes"`
	SHA256           string    `json:"sha256"`
	Status           string    `json:"status"` // success, skipped_unchanged, skipped_oversize, failed
	ErrorMessage     string    `json:"error_message"`
	CreatedAt        time.Time `json:"created_at"`
}

// TrafficLog 表示流量转移记录
type TrafficLog struct {
	ID               int64     `json:"id"`
	ActionType       string    `json:"action_type"` // backup / restore
	BytesTransferred int64     `json:"bytes_transferred"`
	TargetPath       string    `json:"target_path"`
	CreatedAt        time.Time `json:"created_at"`
}

// Store 数据库持久层接口
type Store struct {
	db *sql.DB
}

// NewStore 创建或打开 SQLite 数据库
func NewStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS configs (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS backup_targets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_path TEXT NOT NULL UNIQUE,
		source_dir TEXT NOT NULL,
		file_name TEXT NOT NULL,
		mapped_dir TEXT NOT NULL,
		max_size_limit INTEGER DEFAULT 0,
		schedule_enabled INTEGER DEFAULT 1,
		keep_versions INTEGER DEFAULT 30,
		last_backup_at DATETIME,
		last_hash TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS backup_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		target_id INTEGER NOT NULL,
		source_path TEXT NOT NULL,
		snapshot_filename TEXT NOT NULL,
		full_nutstore_path TEXT NOT NULL,
		file_size_bytes INTEGER NOT NULL,
		sha256 TEXT NOT NULL,
		status TEXT NOT NULL,
		error_message TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(target_id) REFERENCES backup_targets(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS traffic_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		action_type TEXT NOT NULL,
		bytes_transferred INTEGER NOT NULL,
		target_path TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_snapshots_target_id ON backup_snapshots(target_id);
	CREATE INDEX IF NOT EXISTS idx_traffic_created_at ON traffic_logs(created_at);
	`
	_, err := s.db.Exec(schema)
	return err
}

// Config 获取配置项
func (s *Store) GetConfig(key, defaultValue string) (string, error) {
	var val string
	err := s.db.QueryRow("SELECT value FROM configs WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return defaultValue, nil
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetConfig 设置配置项
func (s *Store) SetConfig(key, val string) error {
	_, err := s.db.Exec(`
		INSERT INTO configs(key, value, updated_at) VALUES(?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, key, val)
	return err
}

// GetAllConfigs 获取所有配置
func (s *Store) GetAllConfigs() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM configs")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		res[k] = v
	}
	return res, nil
}

// AddTarget 添加备份目标
func (s *Store) AddTarget(t *Target) (*Target, error) {
	res, err := s.db.Exec(`
		INSERT INTO backup_targets(
			source_path, source_dir, file_name, mapped_dir,
			max_size_limit, schedule_enabled, keep_versions, last_hash
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
	`, t.SourcePath, t.SourceDir, t.FileName, t.MappedDir, t.MaxSizeLimit, boolToInt(t.ScheduleEnabled), t.KeepVersions, t.LastHash)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	t.ID = id
	return t, nil
}

// UpdateTarget 更新备份目标参数
func (s *Store) UpdateTarget(t *Target) error {
	_, err := s.db.Exec(`
		UPDATE backup_targets SET
			max_size_limit = ?,
			schedule_enabled = ?,
			keep_versions = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, t.MaxSizeLimit, boolToInt(t.ScheduleEnabled), t.KeepVersions, t.ID)
	return err
}

// UpdateTargetBackupState 更新备份状态 (时间与哈希)
func (s *Store) UpdateTargetBackupState(id int64, lastBackupAt time.Time, lastHash string) error {
	_, err := s.db.Exec(`
		UPDATE backup_targets SET
			last_backup_at = ?,
			last_hash = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, lastBackupAt, lastHash, id)
	return err
}

// GetTargetByID 根据 ID 获取目标
func (s *Store) GetTargetByID(id int64) (*Target, error) {
	row := s.db.QueryRow(`
		SELECT id, source_path, source_dir, file_name, mapped_dir,
		       max_size_limit, schedule_enabled, keep_versions,
		       last_backup_at, last_hash, created_at, updated_at
		FROM backup_targets WHERE id = ?
	`, id)
	return scanTarget(row)
}

// GetTargetBySourcePath 根据源路径获取目标
func (s *Store) GetTargetBySourcePath(sourcePath string) (*Target, error) {
	row := s.db.QueryRow(`
		SELECT id, source_path, source_dir, file_name, mapped_dir,
		       max_size_limit, schedule_enabled, keep_versions,
		       last_backup_at, last_hash, created_at, updated_at
		FROM backup_targets WHERE source_path = ?
	`, sourcePath)
	return scanTarget(row)
}

// ListTargets 获取所有备份目标
func (s *Store) ListTargets() ([]Target, error) {
	rows, err := s.db.Query(`
		SELECT id, source_path, source_dir, file_name, mapped_dir,
		       max_size_limit, schedule_enabled, keep_versions,
		       last_backup_at, last_hash, created_at, updated_at
		FROM backup_targets ORDER BY id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Target
	for rows.Next() {
		t, err := scanTargetRows(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *t)
	}
	return list, nil
}

// DeleteTarget 删除目标（级联删除快照）
func (s *Store) DeleteTarget(id int64) error {
	_, err := s.db.Exec("DELETE FROM backup_targets WHERE id = ?", id)
	return err
}

// RecordSnapshot 记录一次快照
func (s *Store) RecordSnapshot(snap *Snapshot) (*Snapshot, error) {
	res, err := s.db.Exec(`
		INSERT INTO backup_snapshots(
			target_id, source_path, snapshot_filename, full_nutstore_path,
			file_size_bytes, sha256, status, error_message, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, snap.TargetID, snap.SourcePath, snap.SnapshotFilename, snap.FullNutstorePath,
		snap.FileSizeBytes, snap.SHA256, snap.Status, snap.ErrorMessage)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	snap.ID = id
	return snap, nil
}

// ListSnapshotsForTarget 获取目标的所有快照
func (s *Store) ListSnapshotsForTarget(targetID int64, limit int) ([]Snapshot, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT id, target_id, source_path, snapshot_filename, full_nutstore_path,
		       file_size_bytes, sha256, status, error_message, created_at
		FROM backup_snapshots WHERE target_id = ? ORDER BY id DESC LIMIT ?
	`, targetID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Snapshot
	for rows.Next() {
		var sp Snapshot
		if err := rows.Scan(
			&sp.ID, &sp.TargetID, &sp.SourcePath, &sp.SnapshotFilename, &sp.FullNutstorePath,
			&sp.FileSizeBytes, &sp.SHA256, &sp.Status, &sp.ErrorMessage, &sp.CreatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, sp)
	}
	return list, nil
}

// GetSnapshotByID 根据快照 ID 查询
func (s *Store) GetSnapshotByID(id int64) (*Snapshot, error) {
	var sp Snapshot
	err := s.db.QueryRow(`
		SELECT id, target_id, source_path, snapshot_filename, full_nutstore_path,
		       file_size_bytes, sha256, status, error_message, created_at
		FROM backup_snapshots WHERE id = ?
	`, id).Scan(
		&sp.ID, &sp.TargetID, &sp.SourcePath, &sp.SnapshotFilename, &sp.FullNutstorePath,
		&sp.FileSizeBytes, &sp.SHA256, &sp.Status, &sp.ErrorMessage, &sp.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &sp, nil
}

// RecordTraffic 记录流量消耗
func (s *Store) RecordTraffic(action string, bytes int64, targetPath string) error {
	_, err := s.db.Exec(`
		INSERT INTO traffic_logs(action_type, bytes_transferred, target_path, created_at)
		VALUES(?, ?, ?, CURRENT_TIMESTAMP)
	`, action, bytes, targetPath)
	return err
}

// GetMonthTrafficUsage 统计本月累计流量 (字节数)
func (s *Store) GetMonthTrafficUsage() (backupBytes int64, restoreBytes int64, err error) {
	rows, err := s.db.Query(`
		SELECT action_type, COALESCE(SUM(bytes_transferred), 0)
		FROM traffic_logs
		WHERE strftime('%Y-%m', created_at) = strftime('%Y-%m', 'now')
		GROUP BY action_type
	`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var action string
		var b int64
		if err := rows.Scan(&action, &b); err == nil {
			if action == "backup" {
				backupBytes = b
			} else if action == "restore" {
				restoreBytes = b
			}
		}
	}
	return backupBytes, restoreBytes, nil
}

// 工具函数
type scannable interface {
	Scan(dest ...any) error
}

func scanTarget(row scannable) (*Target, error) {
	var t Target
	var scheduleInt int
	var lastBackup sql.NullTime
	err := row.Scan(
		&t.ID, &t.SourcePath, &t.SourceDir, &t.FileName, &t.MappedDir,
		&t.MaxSizeLimit, &scheduleInt, &t.KeepVersions,
		&lastBackup, &t.LastHash, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.ScheduleEnabled = (scheduleInt == 1)
	if lastBackup.Valid {
		t.LastBackupAt = &lastBackup.Time
	}
	return &t, nil
}

func scanTargetRows(rows *sql.Rows) (*Target, error) {
	return scanTarget(rows)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
