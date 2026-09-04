# aiquick — Alt+S 唤起的 AI 快速助手

常驻托盘的小工具：全局热键秒开窗口 → 选预设（取变量名 / 翻译…）→ 输入文字回车发送 →
流式看 AI 输出 → 一键复制 → Esc 收起。支持唤起时自动抓取当前选中文本（划词预填）。

## 架构：前后端进程分离

```
aiquick.exe (Fyne UI 壳)                aiquickd.exe (后端 CLI 守护)
  输入框/预设/流式输出/热键/托盘           internal/server  行式 JSON 协议(io 抽象)
  internal/client  spawn+请求关联+心跳 ⇄  internal/backend  业务 handler
                                       internal/llm      OpenAI 兼容 SSE 客户端
                                       internal/store    %APPDATA%\aiquick\*.json
```

- 通信：常驻子进程 + stdin/stdout NDJSON；请求 `{"id","method","params"}`，
  应答 `{"id","ok","result|error"}`，事件 `{"event","rid","data"}`（流式 chunk 带 rid 关联）。
- 后端 stdout 只允许协议消息，日志走 stderr（UI「日志」按钮可查看）。
- UI 不发网络请求；后端不感知 UI，可独立运行：
  手动 `echo '{"id":1,"method":"hello"}' | aiquickd.exe` 即可调试。
- 后端崩溃自动懒重启（下一次调用时拉起）+ 10s 心跳；UI 关闭时优雅 shutdown 后端。

## 构建与运行

依赖：Go 1.26+、CGO 编译器（MSYS2 UCRT64 gcc，`C:\msys64\ucrt64\bin` 需在 PATH）。

```powershell
.\build.ps1          # 产出 bin\aiquick.exe + bin\aiquickd.exe
.\bin\aiquick.exe    # 两个 exe 须同目录
```

首次全量编译 Fyne 约 5~10 分钟，之后增量很快。exe 约 30MB。

## 使用

1. 首次启动 → 托盘图标 → 窗口打开后进「设置」填 API Key
   （默认端点 https://open.bigmodel.cn/api/paas/v4，默认模型 glm-4.7-flash，均可改）。
2. 在任意程序里选中文字 → 按 **Alt+S** → 窗口弹出并自动预填选中内容（可在设置关闭）。
3. 选预设 → 输入/修改 → **回车发送** → 流式输出 → 「复制结果」。
4. 生成中按钮变「停止」可随时取消；**Esc 或点 X 收起窗口**（进程留在托盘）；
   托盘右键菜单「退出」= 注销热键 + 优雅关闭后端后退出。

### 预设

纯用户数据（`%APPDATA%\aiquick\presets.json`），可新建/编辑/删除。
字段：名称、指令(system)、模板(可选，`{{input}}` 为输入占位符；无占位符时模板后拼接输入)。
首次启动写入 3 个示例预设（取变量名 / 取函数名 / 翻译成中文），可随意改删。

### 全局热键

默认 Alt+S，设置页可改：勾选修饰键 → 「点击捕获主键」→ 按一个键（A-Z/0-9/F1-F12）。
被其他程序占用时注册失败会弹窗提示换键。

### 划词预填的实现与代价

模拟一次 Ctrl+C 抓取前台选中文本：会把纯文本剪贴板内容临时覆盖，抓完**尽力还原**；
若原剪贴板是图片/文件则无法还原（保留划选文本）。介意可在设置页关闭该功能。

## 开发

```powershell
go vet ./...
go test ./... -count=1          # 含 e2e：真子进程全流程（构建/握手/流式/取消/强杀重启）
go run ./cmd/aiquickd           # 独立调试后端
```

- `internal/protocol` 线上消息结构（两侧共享）；`internal/api` 业务 DTO。
- `internal/server` 纯 io 读写循环，表驱动测试无需真实进程。
- `internal/client` UI 侧连接器（spawn/关联/订阅/心跳/懒重启）。
- `internal/hotkey` RegisterHotKey 封装（可运行期换键）；
  `internal/capture` 划词（SendInput + 剪贴板还原）。

## 复用这套壳做别的小工具

1. 复制本目录改名；2. `internal/backend` 换成你的 handler；3. UI 换页面。
protocol/server/client 三个包与业务无关，可直接搬走。长任务用
`emit(topic, data)` 推事件 + `ask.cancel` 同款取消模式即可。

## 已知边界 / backlog

- 输入框为单行（回车即发送）；多行粘贴会被压成一行。
- 划词依赖目标程序响应 Ctrl+C（终端/部分 PDF/PDF 阅读器可能抓不到，静默降级为空预填）。
- backlog：历史记录、多轮追问、预设专属快捷键、开机自启、多供应商切换、托盘图标美化。
