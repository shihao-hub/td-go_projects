package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

var ErrResearchBusy = errors.New("研究正在被其他操作修改，请稍后重试")
var ErrResearchNotFound = errors.New("研究记录不存在")

type ResearchDocument struct {
	ID         string `json:"id"`
	Question   string `json:"question"`
	Revision   int64  `json:"revision"`
	UpdatedAt  string `json:"updatedAt"`
	Data       string `json:"-"`
	LeaseUntil int64  `json:"-"`
}

func (s *Store) initResearchSchema() error {
	var exists int
	if err := s.db.QueryRow("SELECT count(1) FROM sqlite_master WHERE type='table' AND name='research_sessions'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		// 一致备份包含 WAL 中已提交的数据，禁止直接复制 .db。
		backup := filepath.Join(filepath.Dir(s.dbPath), "deepseeksearch-pre-research-"+time.Now().UTC().Format("20060102T150405.000000000")+".db")
		if _, err := s.db.Exec("VACUUM INTO ?", backup); err != nil {
			return fmt.Errorf("迁移前备份失败: %w", err)
		}
	}
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS research_sessions (
 id TEXT PRIMARY KEY, question TEXT NOT NULL, data TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL,
 lease_token TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0
 ); CREATE TABLE IF NOT EXISTS research_cache (
 cache_key TEXT PRIMARY KEY, data TEXT NOT NULL, created_at INTEGER NOT NULL
 );`)
	return err
}

func (s *Store) CreateResearch(ctx context.Context, id, question, data string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO research_sessions(id,question,data,updated_at) VALUES(?,?,?,?)", id, question, data, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) ListResearch(ctx context.Context) ([]ResearchDocument, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,question,revision,updated_at FROM research_sessions ORDER BY updated_at DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ResearchDocument{}
	for rows.Next() {
		var d ResearchDocument
		if err := rows.Scan(&d.ID, &d.Question, &d.Revision, &d.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}
func (s *Store) GetResearch(ctx context.Context, id string) (ResearchDocument, error) {
	var d ResearchDocument
	err := s.db.QueryRowContext(ctx, "SELECT id,question,data,revision,updated_at,lease_until FROM research_sessions WHERE id=?", id).Scan(&d.ID, &d.Question, &d.Data, &d.Revision, &d.UpdatedAt, &d.LeaseUntil)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrResearchNotFound
	}
	return d, err
}
func (s *Store) ClaimResearch(ctx context.Context, id, token string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE research_sessions SET lease_token=?,lease_until=? WHERE id=? AND lease_until<=?", token, time.Now().Unix()+180, id, time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := s.GetResearch(ctx, id); err != nil {
			return err
		}
		return ErrResearchBusy
	}
	return nil
}
func (s *Store) SaveResearch(ctx context.Context, id, token, data string) error {
	if len(data) > 32<<20 {
		return errors.New("研究记录超过 32 MB，请新建研究")
	}
	res, err := s.db.ExecContext(ctx, "UPDATE research_sessions SET data=?,revision=revision+1,updated_at=? WHERE id=? AND lease_token=? AND lease_until>?", data, time.Now().UTC().Format(time.RFC3339Nano), id, token, time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrResearchBusy
	}
	return nil
}
func (s *Store) ReleaseResearch(ctx context.Context, id, token string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE research_sessions SET lease_token='',lease_until=0 WHERE id=? AND lease_token=?", id, token)
	return err
}
func (s *Store) GetResearchCache(ctx context.Context, key string) (string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT data FROM research_cache WHERE cache_key=? AND created_at>?", key, time.Now().Add(-24*time.Hour).Unix()).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return raw, err
}
func (s *Store) PutResearchCache(ctx context.Context, key, data string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO research_cache(cache_key,data,created_at) VALUES(?,?,?) ON CONFLICT(cache_key) DO UPDATE SET data=excluded.data,created_at=excluded.created_at", key, data, time.Now().Unix())
	return err
}
