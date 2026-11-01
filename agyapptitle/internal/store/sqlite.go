package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"language_projects/go_projects/agyapptitle/internal/model"
)

// Environment 表示一个 Antigravity 环境目录。
type Environment struct {
	Name string
	Path string
}

// GetActiveEnvironments 获取本机已存在 conversation_summaries.db 的 Antigravity 环境列表。
func GetActiveEnvironments(target string) []Environment {
	home, _ := os.UserHomeDir()
	known := map[string]string{
		"app": filepath.Join(home, ".gemini", "antigravity"),
		"cli": filepath.Join(home, ".gemini", "antigravity-cli"),
		"ide": filepath.Join(home, ".gemini", "antigravity-ide"),
	}

	var envs []Environment
	if target != "" && target != "all" {
		if p, ok := known[target]; ok {
			dbPath := filepath.Join(p, "conversation_summaries.db")
			if _, err := os.Stat(dbPath); err == nil {
				envs = append(envs, Environment{Name: target, Path: p})
			}
		}
		return envs
	}

	for _, name := range []string{"app", "cli", "ide"} {
		p := known[name]
		dbPath := filepath.Join(p, "conversation_summaries.db")
		if _, err := os.Stat(dbPath); err == nil {
			envs = append(envs, Environment{Name: name, Path: p})
		}
	}
	return envs
}

// OpenDB 打开环境下的 SQLite 数据库。
func OpenDB(envPath string) (*sql.DB, error) {
	dbPath := filepath.Join(envPath, "conversation_summaries.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)", filepath.ToSlash(dbPath))
	return sql.Open("sqlite", dsn)
}

// HasRawSummary 检查表中是否存在 raw_summary 字段。
func HasRawSummary(db *sql.DB) bool {
	rows, err := db.Query("PRAGMA table_info(conversation_summaries)")
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err == nil {
			if name == "raw_summary" {
				return true
			}
		}
	}
	return false
}

// QuerySessions 从指定环境数据库查询全部会话。
func QuerySessions(env Environment) ([]model.SessionRecord, error) {
	db, err := OpenDB(env.Path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	hasRaw := HasRawSummary(db)
	query := "SELECT conversation_id, title, preview, last_modified_time, workspace_uris, step_count FROM conversation_summaries ORDER BY last_modified_time DESC"

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.SessionRecord
	for rows.Next() {
		var id, title, preview, lastMod, ws sql.NullString
		var stepCount sql.NullInt64

		if err := rows.Scan(&id, &title, &preview, &lastMod, &ws, &stepCount); err != nil {
			continue
		}

		cleanTitle := title.String
		cleanPrev := model.SanitizePreview(preview.String)

		rec := model.SessionRecord{
			ID:           id.String,
			Env:          env.Name,
			Title:        cleanTitle,
			Preview:      cleanPrev,
			StepCount:    int(stepCount.Int64),
			LastModified: lastMod.String,
			Workspace:    ws.String,
			HasRaw:       hasRaw,
			IsChinese:    model.IsChineseTitle(cleanTitle),
		}
		list = append(list, rec)
	}

	return list, nil
}

// ApplyBatchRename 对指定环境执行四重原子双写（SQLite + raw_summary + .pb + annotations + WAL Checkpoint）。
func ApplyBatchRename(env Environment, updates map[string]string) (int, error) {
	if len(updates) == 0 {
		return 0, nil
	}

	db, err := OpenDB(env.Path)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	hasRaw := HasRawSummary(db)
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	successCount := 0

	for cid, newTitle := range updates {
		if hasRaw {
			var rawBytes []byte
			row := tx.QueryRow("SELECT raw_summary FROM conversation_summaries WHERE conversation_id = ?", cid)
			_ = row.Scan(&rawBytes)

			if len(rawBytes) > 0 {
				newRaw := UpdateSummaryTitle(rawBytes, newTitle)
				_, err = tx.Exec("UPDATE conversation_summaries SET title = ?, raw_summary = ? WHERE conversation_id = ?", newTitle, newRaw, cid)
			} else {
				_, err = tx.Exec("UPDATE conversation_summaries SET title = ? WHERE conversation_id = ?", newTitle, cid)
			}
		} else {
			_, err = tx.Exec("UPDATE conversation_summaries SET title = ? WHERE conversation_id = ?", newTitle, cid)
		}

		if err != nil {
			return 0, fmt.Errorf("update sqlite %s failed: %w", cid, err)
		}

		// 同步更新 annotations/<id>.pbtxt
		annoDir := filepath.Join(env.Path, "annotations")
		_ = UpdateAnnotationFile(annoDir, cid, newTitle)

		successCount++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	// 强制执行 WAL Checkpoint TRUNCATE，确保内存协调不反向覆盖
	_, _ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")

	// 同步更新 agyhub_summaries_proto.pb
	pbPath := filepath.Join(env.Path, "agyhub_summaries_proto.pb")
	if _, err := os.Stat(pbPath); err == nil {
		_, _ = UpdatePbStore(pbPath, updates)
	}

	return successCount, nil
}
