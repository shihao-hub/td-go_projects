# quickask

命令行快速问 AI：选定预设（取变量名 / 翻译 / 自定义），输入文字，流式看结果。

从 `aiquick`（fyne GUI + 托盘 + 全局热键版）复制改造而来。aiquick 本就是 C/S 架构——GUI 只是 `aiquickd` 后端守护（行式 JSON 协议）的一个客户端，因此 quickask 仅替换客户端层：fyne 窗口/托盘/热键换成 CLI 子命令，`backend/`、`llm/`、`store/`、`protocol/`、`server/`、`client/` 连接器原样继承（后端随项目更名 quickaskd）。

## 命令

```
quickask ask <text> --preset <名> [--json]   # 单次提问；text 多参数空格拼接，'-' 从 stdin 读全文
quickask preset <名> <text>                   # 按预设名快捷提问
quickask presets                              # 预设列表
quickask presets add <名> --system S [--template T]
quickask presets remove <名|ID>
quickask config get                           # 查看 LLM 配置
quickask config set --base-url U --api-key K --model M   # 部分更新
quickask                                      # 无参数进入交互 REPL
quickask help
```

## 行为说明

- **与 GUI 版共享数据**：预设与 LLM 配置同用 `%APPDATA%\aiquick\`（config.json + presets.json），两边互通
- **ask / preset 是流式命令**（对齐 clictl run 的透传豁免）：stdout 属于流式应答文本，错误 JSON 走 stderr；`--json` 切换为 JSONL 事件流（`{"event":"chunk","text":"..."}` … `{"event":"done","text":"全文"}`）供机器消费
- **presets / config 是管理命令**：stdout 永远是 JSON 包络，`--pretty` 缩进（需放在子命令前）
- 后端 quickaskd 由 CLI 自动拉起（exe 同目录 / cwd / bin 查找，环境变量 `QUICKASK_BACKEND` 可指定），本次调用结束即退出
- Ctrl+C 中断流式提问（收到 cancelled 错误）

## 构建

```
go build ./cmd/quickask    # CLI 客户端
go build ./cmd/quickaskd   # 后端守护
./build.ps1                # 两个一起
```

## 与 aiquick 的关系

aiquick（fyne GUI 版）已归档至父仓 `.archived/go_projects/aiquick`，由本项目替代；后端数据与协议同源，预设与 LLM 配置仍共享 `%APPDATA%\aiquick\`。
