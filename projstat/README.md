# projstat

`language_projects` 总仓的项目状态标注 CLI：手工标注（每个项目目录下的 `PROJECT.toml`）+ git 元数据实时采集（最后提交时间、最近 tag），合并输出统一视图，一条命令回答"每个项目处于什么阶段、接下来做什么、是否读过源码、是否可用、是否测试过"。

- 管理范围：`go_projects / python_projects / rust_projects / typescript_projects` 四个子仓的直接子目录（`.archived/` 完全忽略）。
- 数据分层：静态标注来自 `PROJECT.toml`，动态数据每次运行并发现采、不落盘。
- 直接依赖仅 [BurntSushi/toml](https://github.com/BurntSushi/toml) 与 [golang.org/x/text](https://pkg.go.dev/golang.org/x/text)，单 exe 分发。

## 构建

```powershell
cd go_projects/projstat
go build -o projstat.exe .
```

## 命令表

| 命令 | 说明 | 常用 flag |
|---|---|---|
| `projstat` / `projstat list` | 全部项目表格视图（默认按 name 排序） | `--lang` `--stage` `--sort name\|commit\|due` |
| `projstat show <name>` | 单项目详情（含 git 小节） | 支持 `lang/name` 二段式消歧 |
| `projstat set <name> --<field> <value>` | 更新单个字段并维护 updated_at | 见下方字段表 |
| `projstat init` | 为缺 `PROJECT.toml` 的项目生成骨架（幂等） | — |
| `projstat next` | 待办视图：逾期置顶标红、due 升序、无 due 垫底 | — |
| 全局 | — | `--root <path>`（自动定位失败时）、`--json`（单行 JSON 信封） |

### 示例

```powershell
projstat list                          # 全量表格
projstat list --lang go --stage wip    # 组合过滤
projstat list --sort commit            # 按最后提交时间降序
projstat show zedhub                   # 单项目详情
projstat set zedhub --stage usable     # 更新阶段
projstat set zedhub --usable           # 布尔置 true（--usable=false 置 false）
projstat next                          # 看逾期与近期待办
projstat list --json                   # JSON 信封输出
```

> PowerShell 5.1 注意：清空字符串字段请用等号形式 `--next=`、`--due=`；
> 传空串参数 `--next ""` 会被 PowerShell 吞掉并触发"缺少值"报错。

## PROJECT.toml 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `name` | string | 项目名（目录名） |
| `lang` | string | go / python / rust / typescript（init 自动推导） |
| `stage` | enum | 阶段，见下表；空 = 未标注 |
| `source_read` | bool 三态 | 是否已读过源码；不写 = 未标注 |
| `usable` | bool 三态 | 是否可用；不写 = 未标注 |
| `tested` | bool 三态 | 是否测试过；不写 = 未标注 |
| `summary` | string | 一句话总结 |
| `next_action` | string | 下一步动作（next 视图数据源） |
| `next_due` | string | 下一步截止日 `YYYY-MM-DD`；逾期自动标红 |
| `notes` | string | 备注 |
| `updated_at` | string | 工具维护，set 时自动刷新 |

### stage 枚举

| 值 | 含义 |
|---|---|
| `idea` | 仅有想法，未动工 |
| `learning` | 学习/练手性质 |
| `wip` | 进行中 |
| `mvp` | 最小可用版本已成 |
| `usable` | 可日常使用 |
| `paused` | 暂停，稍后继续 |
| `dropped` | 放弃 |

## 注意事项

- **`set` 重写会丢弃手写注释**：Save 按固定字段顺序重写整个文件并生成组头注释，请勿在 `PROJECT.toml` 里维护自定义注释。
- git 元数据（最后提交时间、最近 tag）每次运行实时采集，不落盘；git 缺失或非仓库时相关列显示 `-`，不报错。
- 退出码：`0` 成功（含空结果）、`1` 运行错误、`2` 用法/参数错误。
- `--json` 输出不含 ANSI 颜色码；表格对齐按 CJK 显示宽度计算。
- 中文乱码时先执行 `chcp 65001` 切换控制台代码页（程序启动时也会自动尝试切换）。
