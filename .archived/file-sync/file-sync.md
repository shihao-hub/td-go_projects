# file-sync

带忽略规则的**本地目录同步工具**，支持 gitignore 语法，通过 Web UI 可视化管理。

## 功能

- 手动触发同步；强制同步（类似 `git push --force`，以源目录为准）
- 支持 gitignore 语法的忽略规则
- 实时显示同步进度
- 差异对比：新增 / 修改 / 删除文件逐项列出
- 任务配置持久化（`~/.file-sync/config.json`）

## 用法

默认带系统托盘图标，启动后自动打开管理界面：

```
file-sync.exe                  # 默认监听 http://localhost:8080
file-sync.exe -addr :9090      # 换监听端口
file-sync.exe -no-tray         # 控制台模式，不显示托盘
file-sync.exe -config x.json   # 指定配置文件
file-sync.exe -log x.log       # 指定日志文件
```

## 结构

- `sync/` 扫描（应用忽略规则）、差异对比、执行复制/删除、进度追踪
- `ignore/` gitignore 规则匹配器
- `web/` HTTP 服务器 + API + 前端静态页
