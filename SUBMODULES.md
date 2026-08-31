# SUBMODULES.md —— submodules 的前世与来生

## 背景与决策

本仓库最初计划采用 **git submodules** 管理各个 Go 子项目：每个子目录挂载一个独立仓库，主仓库只跟踪子项目的 commit 指针。实际体验后发现维护成本太高，已退回 **monorepo**（单仓平铺）：

- **克隆麻烦**：每次 clone 都要记得 `--recurse-submodules`，忘了就得补 `git submodule update --init --recursive`。
- **提交链路长**：子项目改动要先在子仓库 commit + push，再回到主仓库更新指针、再 commit 一次，一次改动两步提交，心智负担大。
- **IDE 识别问题**：工具链对嵌套仓库的识别不友好，常把子目录当成"被忽略/隐藏"的路径。
- **个人练习仓库收益低**：子项目之间无共享、无权限隔离需求，submodules 的核心优势用不上。

## 现状

monorepo：所有子项目平铺在根目录，整个目录作为单一 Git 仓库管理，新增项目直接建子目录、不再单独 `git init`。

## 未了心愿

仍然想迁移到 submodules。等以下条件出现时再动手：

- 需要把某个子项目**单独分享 / 单独授权**给他人；
- 子项目需要**独立发版 / 独立 CI**；
- 出现多个工作区都要引用同一子项目的情况。

## 参考实现：creativault260820（`C:\WorkingProjects\creativault260820`）

一套真实在用的 submodules 工作区，迁移时照抄即可：

**`.gitmodules`（主仓库根目录）：**

```ini
[submodule "frontend"]
    path = frontend
    url = https://git.tec-do.com/powerdata/creativault-business-frontend.git
    ignore = all
[submodule "backend"]
    path = backend
    url = https://git.tec-do.com/powerdata/creativault-business-backend.git
    ignore = all
```

要点：

- `ignore = all`：主仓库不看子项目内部的改动，避免 `git status` 一直显示 modified 指针噪音。
- 主仓库**只跟踪子项目的 commit 指针**，代码本体在各自远端仓库。
- README 里写明克隆方式，新人一 Copy 就对：

```bash
git clone --recurse-submodules <主仓库地址>
```

## 未来迁移步骤草案（从本 monorepo 拆到 submodules）

1. **建远端仓库**：为每个要拆出去的子项目在托管平台（如 git.tec-do.com）建独立仓库。
2. **拆历史**（二选一）：
   - 保留历史：主仓库内 `git subtree split -P <子目录> -b <分支名>`，把分支推到新远端；
   - 不要历史：子目录内重新 `git init`、提交、推送（本项目当初 monorepo 化时已清过一次历史，多数情况下这条路更省事）。
3. **主仓库摘除子目录**：`git rm -r <子目录>` 并提交。
4. **挂载 submodule**：`git submodule add <远端URL> <子目录>`，随后在 `.gitmodules` 里为该条目补 `ignore = all`。
5. **收尾**：README 写明 `git clone --recurse-submodules`；AGENTS.md 的仓库结构说明改回 submodules 口径；把本文件标记为"已完成迁移"。
6. **日常命令速查**：`git submodule update --init --recursive`（拉取）、`git submodule update --remote`（跟进子仓库新提交）。
