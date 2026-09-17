package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type snapshot struct {
	ID      int64
	TS      string
	Folders []string
}

type Store struct {
	db *sql.DB
}

// DataDir 返回 %APPDATA%\sublime-folders，存放记录库与日志。
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "sublime-folders"), nil
}

func OpenStore() (*Store, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "records.db"))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS snapshot (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts TEXT NOT NULL,
		folders TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_snapshot_ts ON snapshot(ts)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// lastSnapshotFolders 返回最新一条记录的 folders JSON；无记录时返回空串。
func (s *Store) lastSnapshotFolders() string {
	var j string
	if err := s.db.QueryRow(`SELECT folders FROM snapshot ORDER BY id DESC LIMIT 1`).Scan(&j); err != nil {
		return ""
	}
	return j
}

func (s *Store) insertSnapshot(folders []string) error {
	j, err := json.Marshal(folders)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO snapshot (ts, folders) VALUES (?, ?)`,
		time.Now().Format("2006-01-02 15:04:05"), string(j))
	return err
}

func (s *Store) query(limit int64) ([]snapshot, error) {
	q := `SELECT id, ts, folders FROM snapshot ORDER BY id DESC`
	var args []any
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []snapshot
	for rows.Next() {
		var sn snapshot
		var j string
		if err := rows.Scan(&sn.ID, &sn.TS, &j); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(j), &sn.Folders); err != nil {
			return nil, fmt.Errorf("解析记录 %d: %w", sn.ID, err)
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *Store) latestN(n int) ([]snapshot, error) { return s.query(int64(n)) }
func (s *Store) all() ([]snapshot, error)          { return s.query(0) }

// prune 删除 retainDays 天前的记录，返回删除条数。
func (s *Store) prune(retainDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retainDays).Format("2006-01-02 15:04:05")
	res, err := s.db.Exec(`DELETE FROM snapshot WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
