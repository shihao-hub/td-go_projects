# Zed ACP Agent 图标机制与代理拦截可行性调研报告

- **调研时间**：2026-10-31
- **调研对象**：Zed 官方仓库（`zed-industries/zed` main 分支）与本地安装实例（Windows）
- **核心结论**：此前推测的“在 `settings.json` 的 `type: "registry"` 下指定 `path` / `args` 覆盖执行路径”**不成立**。Zed 官方源码中并未实现该字段；但通过第一手源码的深挖，弄清了图标渲染与执行链的全部底层逻辑，并给出了确定可行的工程解法。

---

## 1. 核心事实核查与一手源码证据（Primary Source Evidence）

### 证据 1：`settings.json` 中 `Registry` 模式不存在 `path` / `args`
在 Zed 源码 [`crates/project/src/agent_server_store.rs`](https://github.com/zed-industries/zed/blob/main/crates/project/src/agent_server_store.rs) 的 `CustomAgentServerSettings` 枚举中：

```rust
pub enum CustomAgentServerSettings {
    Custom {
        command: AgentServerCommand, // 仅 Custom 变体包含 command (path, args)
        default_mode: Option<String>,
        default_config_options: HashMap<String, AgentConfigOptionValue>,
        favorite_config_option_values: HashMap<String, Vec<String>>,
    },
    Registry {
        env: HashMap<String, String>,
        default_mode: Option<String>,
        default_config_options: HashMap<String, AgentConfigOptionValue>,
        favorite_config_option_values: HashMap<String, Vec<String>>,
    },
}
```

- **事实**：`Registry` 变体**完全没有 `path` 和 `args` 字段**。在配置中书写会被 Rust 的 serde 反序列化静默忽略或报错。
- **事实溯源**：此前网络搜索提及的“Issue #50828 提供了 path 覆盖”实为该 Issue 中的用户提案讨论，并未被官方合并进主干。

---

### 证据 2：自定义 Agent 的图标被硬编码写死为终端标
在 [`crates/agent_servers/src/custom.rs`](https://github.com/zed-industries/zed/blob/main/crates/agent_servers/src/custom.rs)（L38-L40）：

```rust
impl AgentServer for CustomAgentServer {
    fn agent_id(&self) -> AgentId {
        self.agent_id.clone()
    }

    fn logo(&self) -> IconName {
        IconName::Terminal
    }
    // ...
}
```

在 [`crates/project/src/agent_server_store.rs`](https://github.com/zed-industries/zed/blob/main/crates/project/src/agent_server_store.rs)（L362-L375）：

```rust
CustomAgentServerSettings::Custom { command, .. } => {
    let agent_name = AgentId(name.clone().into());
    self.external_agents.insert(
        agent_name.clone(),
        ExternalAgentEntry::new(
            Box::new(LocalCustomAgent {
                command: command.clone(),
                project_environment: project_environment.clone(),
            }) as Box<dyn ExternalAgentServer>,
            ExternalAgentSource::Custom,
            None, // <--- icon_path 硬编码传 None！
            None,
        ),
    );
}
```

- **事实**：所有 `type: "custom"` 的 Agent，其 UI Logo 直接写死为 `IconName::Terminal`，且其 `icon_path` 为 `None`。因此只要配成 `custom`，在侧边栏和下拉列表中必定全是同一个默认终端灰色图标。

---

### 证据 3：Registry Agent 的真实加载与执行路径
在同一文件的 L377-L417：

```rust
CustomAgentServerSettings::Registry { env, .. } => {
    let Some(agent) = registry_agents_by_id.get(name) else { ... };
    match agent {
        RegistryAgent::Binary(agent) => {
            self.external_agents.insert(
                agent_name.clone(),
                ExternalAgentEntry::new(
                    Box::new(LocalRegistryArchiveAgent {
                        installation_dir: paths::external_agents_dir()
                            .join("registry")
                            .join(sanitize_path_component(name)),
                        // ...
                    }),
                    ExternalAgentSource::Registry,
                    agent.metadata.icon_path.clone(), // <--- 只有从 Registry 解析才有专属 SVG 图标！
                    Some(agent.metadata.name.clone()),
                ),
            );
        }
        RegistryAgent::Npx(agent) => { ... }
    }
}
```

结合本地真实环境（Windows）验证：
- 注册表元数据位置：`%LOCALAPPDATA%\Zed\external_agents\registry\registry.json`
- 图标缓存位置：`%LOCALAPPDATA%\Zed\external_agents\registry\icons\*.svg`
- 官方下载的实际二进制位置：
  - Antigravity：`%LOCALAPPDATA%\Zed\external_agents\registry\antigravity-acp\v_1.4.5_...\agy_acp_server.exe`
  - OpenCode：`%LOCALAPPDATA%\Zed\external_agents\registry\opencode\v_2.2.5_...\opencode.exe`

Zed 启动 Registry Agent 时，**强制硬编码调用上述 `installation_dir` 中的可执行文件**。

---

## 2. 为什么“挂载注入程序”不可行？

1. **图形栈阻断**：Zed 不是基于 Web/Electron 的编辑器，没有 DOM 树或 CSS 文件；它采用自研的 **GPUI**，直接调用 **Direct3D / Vulkan** 将顶点提交给 GPU 渲染。没有任何简单样式表可供注入。
2. **内存结构不稳定**：Zed 为 Rust 原生编译产物，Release 模式下开启了深度内联与符号剥离（No Stable C-ABI）。即便使用 Windows Hook（Detours/MinHook）逆向特定函数偏移，Zed 每隔几天就推送的小版本热更会导致偏移立即失效并引发崩溃。

---

## 3. 可落地的真实工程解决方案对比

针对“想保留官方品牌图标以作区分，又想拥有代理拦截/自动补充标题能力”的诉求，有以下三种可行方案：

| 方案 | 原理 | 图标表现 | 侵入性 | 稳定性 |
| :--- | :--- | :--- | :--- | :--- |
| **方案 1：差异化分流（推荐）**<br>AGY 走 Custom 代理，OpenCode 走原生 Registry + Watch 库同步 | AGY 独占 custom 终端标；OpenCode 保持原生 registry 显示品牌绿标；`syncer.go` 轮询 SQLite 同步标题 | **AGY（终端标）与 OpenCode（原生标）天然区分**，彻底消除混淆 | **零侵入**，仅改配置文件与开启后台 watch | 极高，完全官方机制 |
| **方案 2：二进制替身（Shim 劫持）** | 将 `external_agents\registry\...\agy_acp_server.exe` 替换为代理或包装脚本 | **完全继承官方品牌图标** | 低，仅替换目录下的 .exe | 中（Agent 跨大版本升级重新下载时需重新覆盖） |
| **方案 3：本地拉取 Zed 源码修改并编译** | 在 `CustomAgentServerSettings::Custom` 中增加 `icon_path` 字段并编译原生 Zed | **任意自定义本地 SVG 图标** | 高，需 Rust 编译链 | 需自行维护 Zed 构建 |

---

## 4. 推荐落地路径（方案 1 的完整闭环）

由于你最初的困扰正是“两个都挂 Custom 时全变成了同一个灰图标分不清”：

1. **Zed 配置隔离**：
   - `antigravity-acp`（或自定义命名的 `Google Antigravity`）：保留在 `type: "custom"`，走 `acp-title-proxy.exe` 代理，显示终端图标。
   - `opencode`：直接保持 `"type": "registry"`，**不走任何自定义代理，直接使用 Zed 原生加载**，显示官方品牌图标。
   - 此时在 Zed 的会话列表和新建选择器中，**一个有图标，一个无图标（终端标），视觉上一目了然，再也不会搞混**。
2. **OpenCode 标题自动补充**：
   - 运行 `acp-title-proxy --mode=watch`（或由守护进程常驻）。
   - 如前一次推演所证明：`syncer.go` 带有 `(title_override IS NULL OR title_override = '')` 守卫，即使人在 Zed 没重启时手工修改了标题，也**绝对不会**被后台写入脏数据冲掉。
