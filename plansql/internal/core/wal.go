package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// WALManager 负责管理只增不减的 .sql 文件（Append-Only WAL）
type WALManager struct {
	filePath string
	mu       sync.Mutex
}

// NewWALManager 创建 WAL 管理器
func NewWALManager(filePath string) *WALManager {
	return &WALManager{
		filePath: filePath,
	}
}

// FilePath 返回管理的 SQL 文件路径
func (w *WALManager) FilePath() string {
	return w.filePath
}

// CheckResult 记录单个 SQL 语句的校验结果
type CheckResult struct {
	TotalStatements int      `json:"total_statements"`
	Valid           bool     `json:"valid"`
	Errors          []string `json:"errors"`
}

// SplitStatements 切分多条 SQL 语句（兼容换行与分号）
func SplitStatements(content string) []string {
	var statements []string
	lines := strings.Split(content, "\n")
	var currentStmt strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 忽略空行和以 -- 开头的注释
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		currentStmt.WriteString(line)
		currentStmt.WriteString("\n")

		if strings.HasSuffix(trimmed, ";") {
			stmt := strings.TrimSpace(currentStmt.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			currentStmt.Reset()
		}
	}

	remaining := strings.TrimSpace(currentStmt.String())
	if remaining != "" {
		statements = append(statements, remaining)
	}

	return statements
}

// Check 静态校验目标 .sql 文件的语法与有效性（通过在一个干净的临时内存库中逐句回放验证）
func (w *WALManager) Check() (*CheckResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	res := &CheckResult{Valid: true, Errors: []string{}}

	if _, err := os.Stat(w.filePath); os.IsNotExist(err) {
		return res, nil // 文件不存在视为空，无错误
	}

	bytes, err := os.ReadFile(w.filePath)
	if err != nil {
		return nil, fmt.Errorf("read sql file: %w", err)
	}

	stmts := SplitStatements(string(bytes))
	res.TotalStatements = len(stmts)

	// 使用独立的临时内存 db 进行重放测试
	tempStore, err := NewMemoryStore()
	if err != nil {
		return nil, fmt.Errorf("init temp store: %w", err)
	}
	defer tempStore.Close()

	for idx, stmt := range stmts {
		if err := tempStore.ExecDirect(stmt); err != nil {
			res.Valid = false
			res.Errors = append(res.Errors, fmt.Sprintf("Stmt #%d error: %v | SQL: %s", idx+1, err, stmt))
		}
	}

	return res, nil
}

// Replay 将 .sql 文件的所有操作重放到指定的 store 中
func (w *WALManager) Replay(store *Store) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := os.Stat(w.filePath); os.IsNotExist(err) {
		return nil // 无文件则保持初始表结构即可
	}

	bytes, err := os.ReadFile(w.filePath)
	if err != nil {
		return fmt.Errorf("read sql file for replay: %w", err)
	}

	stmts := SplitStatements(string(bytes))
	for idx, stmt := range stmts {
		if err := store.ExecDirect(stmt); err != nil {
			return fmt.Errorf("replay stmt #%d failed: %w | SQL: %s", idx+1, err, stmt)
		}
	}

	return nil
}

// AppendMutation 生成合法的 UPSERT SQL，并原子追加到文件末尾，随后在 store 中执行
func (w *WALManager) AppendMutation(store *Store, itemType, relPath, statusJSON string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 1. 基础校验
	relPath = filepath.ToSlash(filepath.Clean(relPath))
	if itemType != "plan" && itemType != "spec" {
		return "", fmt.Errorf("invalid type: %q, must be 'plan' or 'spec'", itemType)
	}

	// 校验 status 是否是合法 JSON
	var jsVal any
	if err := json.Unmarshal([]byte(statusJSON), &jsVal); err != nil {
		return "", fmt.Errorf("invalid status JSON: %w", err)
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	// 2. 转义处理
	escapedPath := strings.ReplaceAll(relPath, "'", "''")
	escapedStatus := strings.ReplaceAll(statusJSON, "'", "''")

	// 3. 构造具有原子幂等性的 UPSERT 语句
	sqlStmt := fmt.Sprintf(
		"INSERT INTO plan_spec_status (type, path, status, created_at, updated_at)\n"+
			"VALUES ('%s', '%s', '%s', '%s', '%s')\n"+
			"ON CONFLICT(path) DO UPDATE SET\n"+
			"    type = excluded.type,\n"+
			"    status = excluded.status,\n"+
			"    updated_at = excluded.updated_at;",
		itemType, escapedPath, escapedStatus, now, now,
	)

	// 4. 先在临时/当前 store 中试运行，确保绝对不会导致崩溃或约束失败
	if store != nil {
		if err := store.ExecDirect(sqlStmt); err != nil {
			return "", fmt.Errorf("exec mutation on projection failed: %w", err)
		}
	}

	// 5. 追加写入到 .sql 文件末尾
	dir := filepath.Dir(w.filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("ensure dir: %w", err)
		}
	}

	f, err := os.OpenFile(w.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("open sql file for append: %w", err)
	}
	defer f.Close()

	appendContent := fmt.Sprintf("\n-- %s mutation at %s\n%s\n", itemType, now, sqlStmt)
	if _, err := f.WriteString(appendContent); err != nil {
		return "", fmt.Errorf("write sql file: %w", err)
	}

	return sqlStmt, nil
}
