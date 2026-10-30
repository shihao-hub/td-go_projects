package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// GetDefaultZedDBPath 返回 Windows 上 Zed SQLite 数据库的标准路径。
func GetDefaultZedDBPath() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return ""
	}
	return filepath.Join(localAppData, "Zed", "db", "0-stable", "db.sqlite")
}

// CheckZedDBTitle 检查该会话是否已存在于 Zed 的 sidebar_threads 表，
// 且已建立有效标题（title 或 title_override）。
func CheckZedDBTitle(sessionID string, customDBPath string) (string, bool) {
	if sessionID == "" {
		return "", false
	}

	dbPath := customDBPath
	if dbPath == "" {
		dbPath = GetDefaultZedDBPath()
	}

	if dbPath == "" {
		return "", false
	}

	if _, err := os.Stat(dbPath); err != nil {
		return "", false
	}

	connStr := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(1000)", dbPath)
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return "", false
	}
	defer db.Close()

	var title, titleOverride sql.NullString
	query := "SELECT title, title_override FROM sidebar_threads WHERE session_id = ? LIMIT 1"
	err = db.QueryRow(query, sessionID).Scan(&title, &titleOverride)
	if err != nil {
		return "", false
	}

	if titleOverride.Valid {
		to := strings.TrimSpace(titleOverride.String)
		if to != "" {
			return to, true
		}
	}

	if title.Valid {
		t := strings.TrimSpace(title.String)
		if t != "" {
			return t, true
		}
	}

	return "", false
}
