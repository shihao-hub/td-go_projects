# typeai

`typeai` 是一个单进程、无守护的终端 AI 对话工具。在 TTY 下无参数启动进入 Bubble Tea TUI；支持文本和本地图片输入，AI 回复流式显示纯文本预览，回答完成后渲染基础 Markdown。

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
/image <路径>
/detach
```

交互按键：

```text
Enter       发送输入；75ms 内有后续输入时转为换行
Ctrl+J      输入换行
Ctrl+V      读取系统剪贴板，支持多行
Alt+V       读取剪贴板图片并插入图片标记
终端粘贴    支持 bracketed paste 多行粘贴
Ctrl+T      展开或折叠当前 thinking
Ctrl+E      展开或折叠超过 2000 字符或 40 行的 user/assistant 文本
PgUp/PgDn   翻页滚动
Up/Down     多行输入中移动光标；已到边界时逐行滚动
Ctrl+C      请求进行中先取消；空闲时退出
/exit/quit  退出
```

输入区采用无前缀字符的多行编辑框，上下用彩色横线分隔；块状光标表示当前输入位置。

图片输入：

```text
/image <路径>   读取并暂存本地图片；路径含空格时可用引号包裹
/detach         清空已暂存图片
[[image:<路径>]] 图片标记；发送时读取该路径并作为图片 part
```

`Alt+V` 只在按键时读取一次剪贴板。Windows 剪贴板图片保存到：

```text
%TEMP%\language_projects\typeai\clipboard\
```

支持 PNG、JPEG、WebP 和 GIF；单张不超过 10 MiB，每条消息最多 4 张。图片通过 OpenAI-compatible `image_url` 的 `data:` URL 发送；AI 请求文本会移除 `[[image:...]]` 标记，避免重复发送路径。session 和 UI 用户记录保留原始标记，并单独记录图片路径、文件名、类型和大小；session 不保存 base64。同一会话内会缓存图片负载，避免临时文件被清理后无法重建多轮上下文。

Markdown 基础渲染覆盖标题、段落、粗体/斜体、行内代码、fenced code block、有序/无序列表、引用、分隔线和链接。流式增量以低频纯文本预览显示，回答完成后一次性渲染 Markdown；Glamour 渲染失败时继续显示纯文本，不中断请求。用户输入保持纯文本，不作为 Markdown 解析。

超长的 user/assistant 文本默认只保留前 6 行预览，按 `Ctrl+E` 可展开或折叠本轮会话中的所有长文本。流式回答完成前不折叠当前回复。

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
  "schema_version": 2,
  "id": "短ID",
  "model": "模型名",
  "created_at": "时间",
  "updated_at": "时间",
  "messages": [
    {
      "role": "user",
      "content": "问题 [[image:C:\\path\\clipboard.png]]",
      "images": [
        {
          "path": "绝对路径",
          "file_name": "图片名",
          "media_type": "image/png",
          "size": 12345
        }
      ],
      "created_at": "时间"
    },
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
