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

// GetZedDBTitles 返回该会话在 Zed 中记录的标题（title）与用户覆盖标题（title_override）。
//
// title 对 AGY 这类自身不产标题的 agent 而言，可能是 Zed 在首条消息时写入的占位副本
// （首条消息原文或其截断），因此调用方必须结合当前 prompt 判断它是否真的代表已命名，
// 不能仅凭 title 非空就跳过标题生成。title_override 则始终代表用户的显式命名意图。
func GetZedDBTitles(sessionID string, customDBPath string) (string, string, bool) {
	if sessionID == "" {
		return "", "", false
	}

	dbPath := customDBPath
	if dbPath == "" {
		dbPath = GetDefaultZedDBPath()
	}

	if dbPath == "" {
		return "", "", false
	}

	if _, err := os.Stat(dbPath); err != nil {
		return "", "", false
	}

	connStr := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(1000)", dbPath)
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return "", "", false
	}
	defer db.Close()

	var title, titleOverride sql.NullString
	query := "SELECT title, title_override FROM sidebar_threads WHERE session_id = ? LIMIT 1"
	if err := db.QueryRow(query, sessionID).Scan(&title, &titleOverride); err != nil {
		return "", "", false
	}

	return strings.TrimSpace(title.String), strings.TrimSpace(titleOverride.String), true
}
