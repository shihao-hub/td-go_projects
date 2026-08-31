# File-Sync 项目开发计划

## 项目概述

一个带忽略规则的目录同步工具，支持 gitignore 语法，通过 Web UI 进行可视化管理。

### 核心需求
- 手动触发同步
- 强制同步（类似 git push --force），以源目录为准
- 支持 gitignore 语法的忽略规则
- 显示同步进度
- 本地路径同步

## 项目结构

```
file-sync/
├── main.go                 # 程序入口
├── go.mod                  # Go 模块文件
├── config/
│   └── config.go          # 配置管理（加载/保存任务配置）
├── sync/
│   ├── scanner.go         # 目录扫描（应用忽略规则）
│   ├── differ.go          # 差异对比
│   ├── executor.go        # 同步执行（复制/删除文件）
│   └── progress.go        # 进度追踪
├── ignore/
│   └── matcher.go         # gitignore 规则匹配
├── web/
│   ├── server.go          # HTTP 服务器
│   ├── handlers.go        # API 处理器
│   └── static/            # 前端静态文件
│       ├── index.html
│       ├── style.css
│       └── app.js
└── models/
    └── types.go           # 数据结构定义
```

## 核心数据结构

### SyncTask - 同步任务
```go
type SyncTask struct {
    ID          string    `json:"id"`           // 唯一标识
    Name        string    `json:"name"`         // 任务名称
    SourcePath  string    `json:"source_path"`  // 源目录
    TargetPath  string    `json:"target_path"`  // 目标目录
    IgnoreRules []string  `json:"ignore_rules"` // 忽略规则列表
    LastSync    time.Time `json:"last_sync"`    // 最后同步时间
    Enabled     bool      `json:"enabled"`      // 是否启用
}
```

### FileDiff - 文件差异
```go
type FileDiff struct {
    Added    []string `json:"added"`    // 新增文件
    Modified []string `json:"modified"` // 修改文件
    Deleted  []string `json:"deleted"`  // 删除文件
}
```

### SyncProgress - 同步进度
```go
type SyncProgress struct {
    TaskID       string  `json:"task_id"`
    TotalFiles   int     `json:"total_files"`
    CurrentFile  int     `json:"current_file"`
    CurrentPath  string  `json:"current_path"`
    Status       string  `json:"status"`  // scanning/syncing/completed/error
    Percentage   float64 `json:"percentage"`
    ErrorMessage string  `json:"error_message,omitempty"`
}
```

## API 设计

### RESTful API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/tasks` | 获取所有任务列表 |
| POST | `/api/tasks` | 创建新任务 |
| GET | `/api/tasks/:id` | 获取任务详情 |
| PUT | `/api/tasks/:id` | 更新任务 |
| DELETE | `/api/tasks/:id` | 删除任务 |
| POST | `/api/tasks/:id/scan` | 扫描差异（不执行同步） |
| POST | `/api/tasks/:id/sync` | 执行同步 |
| GET | `/api/tasks/:id/progress` | 获取同步进度（SSE） |

### API 请求/响应示例

#### 创建任务
```json
POST /api/tasks
{
    "name": "项目备份",
    "source_path": "C:/Projects/my-app",
    "target_path": "D:/Backup/my-app",
    "ignore_rules": [
        "node_modules/",
        "*.log",
        ".git/",
        "dist/"
    ]
}
```

#### 扫描差异响应
```json
POST /api/tasks/:id/scan
{
    "added": ["src/new-file.go", "docs/readme.md"],
    "modified": ["config.json"],
    "deleted": ["old-file.txt"]
}
```

## 核心模块设计

### 1. 配置管理模块 (`config/config.go`)

**职责：**
- 加载配置文件（`~/.file-sync/config.json`）
- 保存任务配置
- CRUD 操作任务列表

**关键函数：**
```go
func Load() (*Config, error)
func (c *Config) Save() error
func (c *Config) AddTask(task *SyncTask) error
func (c *Config) UpdateTask(task *SyncTask) error
func (c *Config) DeleteTask(id string) error
func (c *Config) GetTask(id string) (*SyncTask, error)
func (c *Config) ListTasks() []*SyncTask
```

### 2. 忽略规则模块 (`ignore/matcher.go`)

**职责：**
- 解析 gitignore 语法规则
- 判断文件路径是否匹配忽略规则

**依赖：**
- `github.com/bmatcuk/doublestar/v4`

**关键函数：**
```go
func NewMatcher(rules []string) *Matcher
func (m *Matcher) Match(path string, isDir bool) bool
```

**支持的规则示例：**
- `*.log` - 忽略所有 .log 文件
- `node_modules/` - 忽略 node_modules 目录
- `!important.log` - 不忽略 important.log
- `**/temp/` - 忽略所有 temp 目录

### 3. 扫描模块 (`sync/scanner.go`)

**职责：**
- 递归扫描目录
- 应用忽略规则过滤文件
- 计算文件哈希（SHA256）用于对比
- 返回文件列表及元数据

**关键函数：**
```go
func ScanDirectory(path string, matcher *Matcher) (*FileTree, error)
func computeHash(filePath string) (string, error)
```

**FileTree 结构：**
```go
type FileTree struct {
    Files map[string]*FileInfo // key: 相对路径
}

type FileInfo struct {
    Path     string
    Hash     string
    Size     int64
    ModTime  time.Time
    IsDir    bool
}
```

### 4. 差异对比模块 (`sync/differ.go`)

**职责：**
- 对比源目录和目标目录
- 识别新增、修改、删除的文件
- 返回 `FileDiff` 结构

**关键函数：**
```go
func ComputeDiff(source, target *FileTree) *FileDiff
```

**对比逻辑：**
- **新增**：源中存在，目标中不存在
- **修改**：源和目标都存在，但哈希不同
- **删除**：目标中存在，源中不存在

### 5. 同步执行模块 (`sync/executor.go`)

**职责：**
- 执行文件复制（保持目录结构）
- 执行文件删除
- 实时更新进度
- 错误处理和日志记录

**关键函数：**
```go
func Execute(task *SyncTask, diff *FileDiff, progress *ProgressTracker) error
func copyFile(src, dst string) error
func deleteFile(path string) error
```

**执行顺序：**
1. 创建目标目录结构
2. 复制新增和修改的文件
3. 删除多余的文件

**错误处理：**
- 记录失败的文件
- 继续处理其他文件
- 返回错误汇总

### 6. 进度追踪模块 (`sync/progress.go`)

**职责：**
- 记录当前同步进度
- 支持并发安全的进度更新
- 通过 SSE（Server-Sent Events）推送给前端

**关键函数：**
```go
type ProgressTracker struct {
    mu           sync.RWMutex
    progress     *SyncProgress
    subscribers  []chan *SyncProgress
}

func NewProgressTracker(taskID string) *ProgressTracker
func (pt *ProgressTracker) Update(current int, total int, path string)
func (pt *ProgressTracker) Subscribe() <-chan *SyncProgress
func (pt *ProgressTracker) SetStatus(status string)
func (pt *ProgressTracker) SetError(err error)
```

### 7. Web 服务模块 (`web/`)

**职责：**
- HTTP 服务器（默认端口 8080）
- RESTful API 处理
- 静态文件服务
- SSE 进度推送

**关键函数：**
```go
func StartServer(addr string) error
func handleListTasks(w http.ResponseWriter, r *http.Request)
func handleCreateTask(w http.ResponseWriter, r *http.Request)
func handleSyncTask(w http.ResponseWriter, r *http.Request)
func handleProgress(w http.ResponseWriter, r *http.Request)
```

## 前端界面设计

### 页面布局

#### 1. 任务列表页（主页）
- **顶部导航栏**
  - 标题："File Sync"
  - 添加任务按钮
- **任务卡片列表**
  - 每个卡片显示：
    - 任务名称
    - 源路径 → 目标路径
    - 最后同步时间
    - 状态标签（空闲/同步中）
  - 操作按钮：
    - 扫描差异
    - 执行同步
    - 编辑
    - 删除

#### 2. 任务编辑页（模态框）
- **表单字段**
  - 任务名称（文本输入）
  - 源路径（文本输入 + 浏览按钮）
  - 目标路径（文本输入 + 浏览按钮）
  - 忽略规则（多行文本框）
- **操作按钮**
  - 保存
  - 取消

#### 3. 同步进度显示
- **进度信息**
  - 进度条（0-100%）
  - 当前处理文件路径
  - 状态文本（扫描中/同步中/完成/错误）
  - 文件计数（x / total）
- **实时更新**
  - 通过 SSE 接收进度更新

#### 4. 差异预览（可选功能）
- **三列布局**
  - 新增文件列表（绿色）
  - 修改文件列表（黄色）
  - 删除文件列表（红色）
- **统计信息**
  - 总计变更文件数

### UI 技术栈
- **HTML5** - 页面结构
- **原生 CSS** - 样式（使用 Flexbox/Grid）
- **原生 JavaScript** - 交互逻辑
- **EventSource API** - SSE 进度推送

## 实现步骤（分阶段）

### Phase 1：基础框架
- [x] 1. 初始化 Go 项目，创建目录结构
- [x] 2. 定义核心数据结构（`models/types.go`）
- [x] 3. 实现配置管理模块（加载/保存 JSON）
- [x] 4. 搭建基础 Web 服务器（静态文件服务）

**验收标准：**
- 项目可编译运行
- 访问 `http://localhost:8080` 显示基础页面
- 配置文件可正常加载和保存

---

### Phase 2：目录扫描
- [x] 5. 实现目录递归扫描
- [x] 6. 集成 gitignore 规则匹配
- [x] 7. 实现文件哈希计算
- [x] 8. 测试扫描功能

**验收标准：**
- 能正确扫描指定目录
- 忽略规则正确应用
- 文件哈希计算准确

---

### Phase 3：差异对比
- [x] 9. 实现源目录和目标目录对比
- [x] 10. 生成 FileDiff 结构
- [x] 11. 测试对比逻辑

**验收标准：**
- 正确识别新增/修改/删除的文件
- 边界情况处理（空目录、目标不存在等）

---

### Phase 4：同步执行
- [x] 12. 实现文件复制逻辑（保持目录结构）
- [x] 13. 实现文件删除逻辑
- [x] 14. 添加错误处理和重试机制
- [x] 15. 测试同步功能

**验收标准：**
- 文件复制成功且完整
- 目录结构正确创建
- 删除操作正确执行
- 错误不会中断整个流程

---

### Phase 5：进度追踪
- [x] 16. 实现进度追踪模块
- [x] 17. 集成 SSE 推送进度
- [x] 18. 前端实现进度条显示

**验收标准：**
- 进度实时更新
- SSE 连接稳定
- 前端进度条准确显示

---

### Phase 6：前端界面
- [x] 19. 实现任务列表页面
- [x] 20. 实现任务编辑功能
- [x] 21. 实现同步触发和进度显示
- [x] 22. 美化界面

**验收标准：**
- 所有功能可通过 UI 操作
- 界面响应流畅
- 视觉效果良好

---

### Phase 7：优化和测试
- [x] 23. 添加日志记录
- [x] 24. 性能优化（大文件处理）
- [x] 25. 边界情况测试
- [x] 26. 打包可执行文件

**验收标准：**
- 日志完整可追踪
- 大文件（GB 级）同步稳定
- 各种异常场景处理正确
- 生成独立可执行文件

## 技术依赖
```go
require (
    github.com/bmatcuk/doublestar/v4  // gitignore 模式匹配
    github.com/google/uuid            // 生成任务 ID
)
```

## 关键技术点

### 1. 文件哈希对比
- 使用 `crypto/sha256` 计算文件哈希
- 判断文件是否修改（而不是仅依赖修改时间）
- 优化：大文件分块计算，避免内存溢出

### 2. 进度计算
```go
percentage := (currentFile / totalFiles) * 100
```

### 3. SSE 推送
```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")

fmt.Fprintf(w, "data: %s\n\n", jsonData)
w.(http.Flusher).Flush()
```

### 4. 原子文件操作
- 先复制到临时文件（`.tmp` 后缀）
- 复制成功后再重命名到目标文件
- 失败时清理临时文件

### 5. 并发安全
- 使用 `sync.RWMutex` 保护共享数据
- 进度更新使用互斥锁
- 配置读写加锁保护

### 6. 目录遍历优化
- 使用 `filepath.WalkDir`（Go 1.16+）
- 比 `filepath.Walk` 更高效

### 7. 大文件处理
- 使用缓冲 I/O（`bufio`）
- 显示文件级进度
- 支持取消操作（context）

## 配置文件示例

### ~/.file-sync/config.json
```json
{
    "tasks": [
        {
            "id": "550e8400-e29b-41d4-a716-446655440000",
            "name": "项目备份",
            "source_path": "C:/Projects/my-app",
            "target_path": "D:/Backup/my-app",
            "ignore_rules": [
                "node_modules/",
                "*.log",
                ".git/",
                "dist/",
                "*.tmp"
            ],
            "last_sync": "2026-08-23T10:30:00Z",
            "enabled": true
        }
    ]
}
```

## 预计时间

| 阶段 | 内容 | 预计时间 |
|------|------|----------|
| Phase 1 | 基础框架 | 1-2 小时 |
| Phase 2 | 目录扫描 | 2-3 小时 |
| Phase 3 | 差异对比 | 1-2 小时 |
| Phase 4 | 同步执行 | 2-3 小时 |
| Phase 5 | 进度追踪 | 1-2 小时 |
| Phase 6 | 前端界面 | 2-3 小时 |
| Phase 7 | 优化测试 | 1-2 小时 |
| **总计** | | **10-17 小时** |

## 后续扩展功能（二期）

- [x] 定时自动同步
- [x] 双向同步
- [x] 冲突处理策略
- [x] 网络路径支持（SMB/NFS）
- [x] 同步历史记录
- [x] 文件压缩传输
- [x] 增量同步优化
- [x] 多任务并行执行
- [x] 云存储集成（S3/OSS）
- [x] 桌面通知

## 注意事项

1. **性能考虑**
   - 大目录（10万+ 文件）扫描可能较慢
   - 建议显示扫描进度
   - 考虑缓存文件树结构

2. **安全考虑**
   - 删除操作不可逆，需谨慎
   - 建议添加日志记录所有操作
   - 考虑添加回收站功能（可选）

3. **跨平台兼容**
   - 路径分隔符使用 `filepath` 包处理
   - 文件权限在 Windows 上需特殊处理
   - 符号链接处理策略

4. **错误处理**
   - 权限不足
   - 磁盘空间不足
   - 文件被占用
   - 路径不存在

---

**最后更新：** 2026-08-23
**作者：** Claude & User
**版本：** v1.0
