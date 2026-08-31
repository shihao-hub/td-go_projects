# agent-reaper

找出并清理**闲置的 AI agent 进程**（opencode / Claude Code / Codex）。
判据是"距上次实际使用的时间"（CPU / IO 活动增量），不是进程启动时间。

## 原理

- 每次采样记录每个 agent 进程的累计 CPU 和 IO 计数器
- 相邻两次采样之间 CPU 增量 > 2 秒或 IO 增量 > 2MB，即视为"刚被用过"，刷新 LastUsed 时间戳（存在 `%LOCALAPPDATA%\agent-reaper\state.json`）
- 超过阈值小时数没有活动、且由 Zed 拉起的进程 → 判为 STALE，可整棵进程树强杀（`taskkill /T /F`）

## 用法

```
agent-reaper.exe                     # 显示表格，询问后清理 STALE
agent-reaper.exe -hours 4            # 闲置阈值改为 4 小时（默认 6）
agent-reaper.exe -scopes zed,vscode  # 清理范围（默认只管 zed）
agent-reaper.exe -all                # 所有来源都算
agent-reaper.exe -list               # 只看不动
agent-reaper.exe -install            # 注册计划任务：每 5 分钟静默采样
agent-reaper.exe -uninstall          # 移除该计划任务
agent-reaper.exe -watch 300 -auto    # 常驻模式：每 5 分钟采样并自动清理
```

推荐用法：`-install` 注册采样任务后，平时双击 exe 看一眼、按 y 清理即可。

## 注意

- 被杀的 agent 会话数据都落盘在各家本地存储里，强杀不丢会话，事后都能 resume
- Zed 已知问题：被杀的 external agent 面板会残留死连接，只有重启 Zed 才能恢复——所以本工具只该杀"闲置很久、大概率已被抛弃"的实例
- 闲置进程的物理内存早已被 Windows 换出，清理回收的主要是提交内存（commit）
- 清理记录写在 `%LOCALAPPDATA%\agent-reaper\reaper.log`
