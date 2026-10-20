# KB Manager (kbman)

`kbman` 是知识库（`kb-engineering-notes`）的时序防腐与生命周期治理引擎。它把「这条知识还靠不靠谱」从人的记忆，变成可机器校验的元数据：写入时注入双时间字段，检索前拦截废弃条目，踩坑后原子回写现场证据。

---

## 🌟 核心特性

1. **最小侵入升级（Minimal-diff Migration）**
   - 只解析 frontmatter、只追加缺失字段；原文件的 `name` / `description` / 正文逐字保留，不重排、不重写、不丢注释。
   - 对 80 篇历史笔记全量升级的实测 diff 仅 921 行新增，无正文改写。
2. **前置防御拦截（Pre-execution Guard）**
   - `check` 按 `verified / draft / suspect / deprecated` 给出执行建议；`deprecated` 直接以退出码 `2` 阻断执行。
3. **执行期自愈回写（In-situ Repair）**
   - `report` 原子更新状态并在正文末尾追加带时间戳的排障现场，`touch` 刷新 `last_verified`。
4. **标准 CLI 契约**
   - 全部命令支持 `--json`（`{"ok":true,"data":...}` 包络）与 `--schema`（离线零 I/O 契约导出），另有 `kbman schema` 目录。
5. **单文件二进制**
   - 纯 Go + `gopkg.in/yaml.v3`，无 CGO、无运行时依赖。

---

## 🚀 快速使用

### 编译

```bash
cd D:\Users\language_projects\go_projects\kbman
go build -o kbman.exe ./cmd/kbman
```

### 1. 批量升级历史笔记

```bash
kbman migrate --dir "C:\Users\29580\.gemini\config\skills\kb-engineering-notes"
```

### 2. 检索前健康检查

```bash
kbman check 002
kbman check --json 002
```

| 状态 | 退出码 | 行为建议 |
| --- | --- | --- |
| `verified` | 0 | 可直接照做 |
| `draft` | 0 | 暂行草案，注意实测 |
| `suspect` | 0 | 先只读探测验证，再执行 |
| `deprecated` | 2 | 禁止执行，改用 `superseded_by` |

### 3. 被证伪后回写

```bash
kbman report 002 --status suspect --reason "Win11 24H2 下句柄锁仍复现" --append "实际报错：..."
```

### 4. 验证通过后刷新

```bash
kbman touch 002
```

### 5. 机器调用契约

```bash
kbman schema
kbman check --schema
```

---

## 📋 元数据规范

迁移后每条笔记的 frontmatter 至少包含：

```yaml
id: "002"
domain: git
scope: rebase
type: fix
slug: 变基PermissionDenied文件锁排障与安全恢复
status: verified
created: "2026-09-22"
last_verified: "2026-10-20"
compatibility:
  os: ["windows"]
invocation:
  auto_trigger: true
```

- `status`：`verified | draft | suspect | deprecated`
- `superseded_by` / `deprecation_reason`：废弃时指向替代条目与原因
- `falsification_signals`：可观测的证伪条件
- 未知字段与既有字段原样保留（`yaml:",inline"` 兜底）

---

## 🗂️ 目录结构

```text
kbman/
├── cmd/kbman/main.go          # CLI 壳：参数、包络、退出码
├── internal/contract/         # 离线 JSON Schema 契约（反射同源）
├── internal/metadata/         # 元数据实体定义
├── internal/parser/           # frontmatter 宽容解析 + 最小侵入渲染
└── internal/service/          # migrate / check / report / touch 用例
```
