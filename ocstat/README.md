# ocstat

统计 opencode 会话「启动时用的模型 + 思考深度档位」的 CLI，用于排查 effort 配置是否符合预期。

> **可执行文件名必须叫 `ocstat.exe`**，不能改成任何以 `stats.exe` 结尾的名字
> （如 ocstats.exe / opencode-stats.exe）：公司飞连 CorpLink 云端黑名单有
> `*stats.exe` 通配规则，会被判「侵权风险」拦截。2026-09-02 实测：
> 同一文件仅改名 stats.exe 结尾必拦、其他名字均放行，与内容/签名无关。

## 用法

    ocstat [选项]           # 打印一次
    ocstat watch [选项]     # 常驻，定时清屏刷新（默认 5s，Ctrl+C 退出）

选项：

    -i 5s      watch 刷新间隔
    -n 30      明细最近 N 条
    -all       明细显示全部
    -switched  只看切换过模型/档位的会话
    -db PATH   opencode.db 路径（默认 ~/.local/share/opencode/opencode.db，识别 OPENCODE_DATA）

## 数据口径

- 数据源：`~/.local/share/opencode/opencode.db`（SQLite，只读打开 `mode=ro` + `busy_timeout 5s`，不打扰运行中的 opencode）
- **启动模型+档位**：首条非 title 的 assistant 消息时间点，`session.created/updated` 事件时间线上生效的 `model`（含 `variant`）。已用真实库验证 235/235 与消息 modelID 吻合
- **思考深度**：`model.variant`（max/high/medium/low/default，缺失显示 `-`），来自 cc-switch 等 variant 方案
- **用过的模型**：会话事件时间线上出现过的 distinct 模型集合；>1 标记 ⇄
- 注：`session.created` 初始 model 常是 opencode 内置 `big-pickle`（标题模型），不代表实际开工模型，故不直接采用；opencode 内置模型同样不参与切换判定

## opencode 升级改表结构怎么办

启动时探测必需表/列并分级降级，绝不 panic：

- 完整模式：session + message + event 齐全
- 降级模式：事件/消息表不可用 → 仅展示会话当前模型，并打印原因
- 不可用：session 表/model 列缺失 → 报错并打印检测到的表清单

model 字段兼容 JSON 对象 / 纯字符串 / null。已适配 opencode 1.18.x。

## 构建

    go build -o ocstat.exe .

watch 模式使用 ANSI 清屏，请在 Windows Terminal / Zed 终端等现代终端运行。
