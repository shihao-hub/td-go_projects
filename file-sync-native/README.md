# file-sync-native

带忽略规则的**本地目录同步工具**（Wails v2 + WebView2 原生窗口，无 HTTP 端口、无浏览器）。

是 [file-sync](../file-sync) 的重写版：去掉了 Web 服务器层（REST/SSE/浏览器），
改为 Wails 绑定调用 + 事件推送；同步引擎改为流式三级判定（见下）。

## 功能

- 手动同步 / 强制同步（全量内容校验，绕过缓存）
- gitignore 语法忽略规则（双侧生效）
- 流式进度：已复制字节、滑动窗口速率、预计剩余时间
- 删除二次确认：待删清单先展示，用户确认后才执行（拒绝则保留）
- 任务配置持久化，与旧版共享 `~/.file-sync/config.json`
- 系托盘：关窗隐藏到托盘，托盘菜单退出

## 同步引擎（与旧版的差异）

三级判定替代旧版的全量哈希对比：

1. 目标缺文件 → 复制
2. `size` 不同 → 复制
3. `size+mtime` 相同 → 跳过（零文件读取）
4. `size` 同 `mtime` 不同 → 双侧哈希比对（缓存感知）；内容相同则修正目标 mtime

哈希缓存 `~/.file-sync/hash-cache.gob`（跨任务共享，LRU，损坏自动弃用）。
基准（2000 文件×16KB）：稳态重扫 **67ms** vs 旧版全量哈希 **5432ms**（约 80x）。

已知边界：内容被篡改但 `size+mtime` 均未变的文件，普通模式会漏检——
这是快速判定的固有代价，用「强制同步」抓回（有测试覆盖）。

## 用法

```
wails build          # 产出 build/bin/file-sync-native.exe
```

双击运行：主窗口 + 托盘；关闭窗口最小化到托盘，托盘菜单「退出」结束进程。

## 结构

- `main.go` / `app.go` / `tray.go` — Wails 入口、绑定层（替代旧版 REST API）、托盘
- `engine/` — 同步引擎：流式扫描、三级判定、哈希缓存、进度（速率/ETA）、删除确认流
- `config/` `ignore/` `models/` `logging/` — 自旧版移植，行为一致
- `frontend/` — vanilla JS + Vite，事件驱动进度更新

## 开发

```
go test ./...        # 引擎与绑定层测试
wails dev            # 热重载开发
```
