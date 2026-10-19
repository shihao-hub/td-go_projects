# docsearch-gui

`docsearch-gui` 是 [docsearch](../../python_projects/docsearch) 的专用桌面托盘外壳（基于 Go + Wails v3）。

它直接加载 `docsearch` 原生静态 WebUI（`http://127.0.0.1:8765`），免去前端工程链重复构建与维护，并彻底解决桌面托盘小工具的后台常驻高内存与渲染开销痛点。

---

## 核心设计与生命周期

遵循工程手记 `kb-engineering-notes`（条目 071）的最佳实践：

```text
┌─────────────────────────────────────────────────────────────┐
│                      Windows 系统托盘                        │
│         (常驻 ~25MB: 状态监控 / 双击唤起 / 右键管理菜单)         │
└──────────────┬──────────────────────────────┬───────────────┘
               │ 最小化 (-)                   │ 关闭 (X)
               ▼                              ▼
    ┌──────────────────────┐       ┌────────────────────────┐
    │ Windows 原生任务栏    │       │ 彻底放行 WindowClosing  │
    │ (WebView2 保持渲染)  │       │ 销毁 WebView2 句柄      │
    │ 热态待命，点开零延迟 │       │ 6个 Chromium 进程全退出 │
    └──────────────────────┘       │ 内存暴降，后台零渲染    │
                                   └────────────────────────┘
                                              │ 双击托盘
                                              ▼
                                   ┌────────────────────────┐
                                   │ 按需毫秒级重建 WebView2 │
                                   │ 加载 http://127.0.0.1: │
                                   │       8765             │
                                   └────────────────────────┘
```

1. **关闭但在托盘时（`X`） -> WebView 不渲染**：
   * 放行窗口关闭事件（不 `Cancel`），让 Wails 彻底销毁原生窗口和 WebView2 句柄；
   * 底层 6 个 Edge/Chromium 子进程彻底退出，DOM / V8 / GPU 渲染管线全部释放，触发 Go GC 归还物理内存（常驻降至 ~25MB 级纯守护态）。
2. **最小化时（`-`） -> 保持渲染**：
   * 走 Windows 原生任务栏最小化（`SW_MINIMIZE`），不触发 `WindowClosing`；
   * 渲染管线、输入框内容与搜索状态全部保留，任务栏一点即开。
3. **托盘呼出 / 双击**：
   * 毫秒级按需瞬间重建 WebView2 窗口重新挂载，体验丝滑。
4. **后台守护与自动拉起**：
   * 壳子启动时自动检测目标端口；
   * 若未检测到 docsearch 服务，自动在后台静默拉起 `uv run docsearch serve --port <壳子端口>`（零黑框闪烁）；
   * 服务就绪后自动刷新已打开的窗口，无需手动关窗重开；
   * 托盘彻底退出时清理自己拉起的子进程。
5. **Chromium 参数剪枝**：
   * 注入 `--disable-gpu`、`--disable-features=NetworkServiceOutOfProcess` 等启动参数，窗口开启态峰值内存比常规浏览器降低 160MB+。

---

## 配置与排障

| 项 | 说明 |
| --- | --- |
| `--port` | 壳子连接**并透传**给 `docsearch serve` 的端口；单实例标识包含端口，不同端口实例互不干扰 |
| `--hidden` | 开机静默启动，仅常驻托盘 |
| `DOCSEARCH_DIR` | 显式指定 docsearch 项目目录（必须含 `pyproject.toml` 与 `src/docsearch`）；不设置时自动从可执行文件/工作目录逐级上溯查找 `python_projects/docsearch` |
| 数据目录 | `%APPDATA%\language_projects\docsearch-gui\`（含 `webview2\` 缓存） |
| 日志 | `%APPDATA%\language_projects\docsearch-gui\docsearch-gui.log`（windowsgui 无控制台，异常一律落盘） |

---

## 构建与运行

### 运行可执行文件
```powershell
.\bin\docsearch-gui.exe           # 正常启动并显示主窗口
.\bin\docsearch-gui.exe --hidden  # 开机静默启动（仅常驻托盘，不弹窗）
```

### 重新构建
```powershell
# 1. (可选) 如修改了图标，重新生成 syso
wails3 generate syso -arch amd64 -icon icon.ico -manifest wails.exe.manifest -info info.json -out wails_windows_amd64.syso

# 2. 编译无黑框 GUI 可执行文件
go build -ldflags="-w -s -H windowsgui" -o bin/docsearch-gui.exe
```

### 单元测试
```powershell
go test ./...        # 覆盖目录探测、端口透传、状态解析、重建索引请求
```
