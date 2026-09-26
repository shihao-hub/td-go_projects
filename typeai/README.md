# typeai

`typeai` 是一个单进程、无守护的终端 AI 对话工具。在 TTY 下无参数启动进入 Bubble Tea TUI；AI 回复按增量接收并流式刷新基础 Markdown 视图。

GLM 等思考型模型返回的 `reasoning_content` 默认折叠，只显示字符统计；可按键展开查看最近 64 KiB。思考内容只用于当前进程界面，不写入 session 历史，session 的 assistant 消息仍只保存正式回答。

## 命令

```powershell
typeai                        # 在 TTY 下进入 TUI 连续对话
typeai config get [--json]    # 查看有效配置
typeai config set --base-url U --api-key K --model M [--json]
typeai schema                 # 导出 CLI 契约目录
typeai version [--json]
typeai help
```

对话内命令：

```text
/exit
/quit
```

交互按键：

```text
Enter       发送输入
Ctrl+J      输入换行
Ctrl+T      展开或折叠当前 thinking
PgUp/PgDn   翻页滚动
Up/Down     逐行滚动
Ctrl+C      请求进行中先取消；空闲时退出
/exit/quit  退出
```

Markdown 基础渲染覆盖标题、段落、粗体/斜体、行内代码、fenced code block、有序/无序列表、引用、分隔线和链接。流式增量先以原始文本兜底，渲染节流后刷新为终端富文本；Glamour 渲染失败时继续显示原始文本，不中断请求。用户输入保持纯文本，不作为 Markdown 解析。

非 TTY 下执行无参数 `typeai` 会返回清晰错误；管理命令和轻量入口不受影响。设置 `NO_COLOR` 后不使用彩色输出。

## 配置

配置文件位于：

```text
%APPDATA%\language_projects\typeai\config.json
```

取不到 `APPDATA` 时回退 `~/.language_projects/typeai/config.json`。

有效配置优先级为：

```text
TYPEAI_BASE_URL / TYPEAI_API_KEY / TYPEAI_MODEL
-> config.json
-> 内置默认值
```

默认 `base_url` 是 `https://api.openai.com/v1`，默认 `model` 是 `gpt-4o-mini`。任何 OpenAI 兼容服务都可以通过 `config set` 接入。`config get` 只显示 `api_key_set`，不输出密钥。

## 会话持久化

每次进程启动会预生成一个 session 文件名：

```text
%APPDATA%\language_projects\typeai\sessions\YYYYMMDD-HHMMSS-<短ID>.json
```

首轮成功回答前不写文件。每轮 AI 完整回答成功后，本轮 `user` 与 `assistant` 消息会追加到内存历史，随后把完整历史重新序列化为一个合法 JSON，并用临时文件原子替换当前 session 文件。这是“业务追加、文件级全量重写”，不会丢失旧消息。

LLM 请求失败或 session 写入失败时，本轮不提交到正式历史，也不产生部分成功的 session 版本。第一阶段不支持 resume、会话列表、搜索或上下文裁剪。

JSON 结构：

```json
{
  "schema_version": 1,
  "id": "短ID",
  "model": "模型名",
  "created_at": "时间",
  "updated_at": "时间",
  "messages": [
    { "role": "user", "content": "问题", "created_at": "时间" },
    { "role": "assistant", "content": "回答", "created_at": "时间" }
  ]
}
```

## 构建与验收

```powershell
cd D:\Users\language_projects\go_projects\typeai
go build ./...
.\build.ps1 -Version dev
.\build\typeai.exe schema
```

`schema`、`help`、`version` 不读取配置、不创建数据目录、不发起网络请求。

## CLI 标准说明

`typeai` 提供管理命令 `--json` 与零 I/O `schema` 导出。第一阶段不提供 MCP：主用例是持续占用的终端交互和流式 stdout，普通 MCP tools/call 不提供通用 PTY/TUI 终端透传；后续如增加一次性问答或配置查询用例，再按需评估 MCP 壳。

## 第二期 backlog

- `export`：将 session JSON 导出为人类阅读的 HTML。
- 更多界面优化：主题配置、表格/图片等扩展 Markdown、更细的滚动和状态提示。
- 会话列表、搜索、resume。
- 上下文裁剪与历史压缩策略。
