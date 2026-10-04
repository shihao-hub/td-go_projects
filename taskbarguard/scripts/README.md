# TaskbarGuard 脚本资产库 (scripts/) 使用指南

本文档介绍 `go_projects\taskbarguard\scripts\` 下各应用模块的脚本用途、全量可选图标款式说明、一键切换命令，以及图标的高清工业级生成工序流水线。

---

## 目录索引

- [一、图标工业级生成工序揭秘 (Pipeline Deep Dive)](#一图标工业级生成工序揭秘-pipeline-deep-dive)
- [二、DeepSeek Harness 模块 (全量款式支持)](#二deepseek-harness-模块-全量款式支持)
  - [1. 全套候选图标款式清单](#1-全套候选图标款式清单)
  - [2. 各款式手动切换命令](#2-各款式手动切换命令)
  - [3. 恢复官方原始状态](#3-恢复官方原始状态)
- [三、VS Code 模块](#三vs-code-模块)
  - [1. 支持款式说明](#1-支持款式说明)
  - [2. 各款式手动切换命令](#2-各款式手动切换命令)
  - [3. 恢复官方原始状态](#3-恢复官方原始状态)
- [四、执行须知与安全准则](#四执行须知与安全准则)

---

## 一、图标工业级生成工序揭秘 (Pipeline Deep Dive)

任务栏图标之所以能在 1080P/2K/4K 等各种 Windows DPI 缩放比下保持绝对锐利、微光弥散自然、无任何锯齿或色阶断层，得益于本工程采用的**“参数化矢量代码 + 无头浏览器物理渲染 + 多阶 LANCZOS 抽样”**工业级流水线：

```text
 ┌──────────────────────┐       ┌───────────────────────┐       ┌───────────────────────┐
 │  参数化数学建模 SVG  │ ────> │ Edge/Chromium 无头引擎│ ────> │ Pillow Lanczos 抽样   │
 │ (圆角/渐变/微光滤镜) │       │ (Skia 矢量物理光追)   │       │ (16~256px 全尺寸 ICO) │
 └──────────────────────┘       └───────────────────────┘       └───────────────────────┘
```

1. **工序 1：参数化数学建模与物理光泽 (Parametric SVG)**：与 Zed 1:1 对齐的 `988x988` 底板 (`rx=150`)，`#24272D -> #0E0F12` 黑曜石渐变与 `14px` 金属微高光边框；主体提取并引入双向地面反弹光与 `<feDropShadow>` 高斯模糊空间微光。
2. **工序 2：Chromium/Edge 无头物理光追渲染 (Skia Antialiasing)**：调用 Edge 无头引擎，利用顶级 Skia 2D 图形引擎实现视网膜级抗锯齿，导出 1024x1024 超清 PNG。
3. **工序 3：Pillow (PIL) 兰索斯多阶 MIP-MAP 抽样**：采用 `LANCZOS` 兰索斯高质量插值算法自顶向下压缩抽样生成 16~256px 全尺寸帧，封装入单文件 ICO。

---

## 二、DeepSeek Harness 模块 (全量款式支持)

模块路径：`go_projects\taskbarguard\scripts\deepseek_harness\`

### 1. 全套候选图标款式清单

所有图标文件均保存在 `custom_icons/` 目录下（底板尺寸 `988x988`，圆角半径 `rx=150`，与 Zed 1:1 绝对对齐）：

| 款式编号 | 款式名称 | 图标文件路径 | 设计与视觉特征说明 |
| :---: | :--- | :--- | :--- |
| **款式 8** | **环形底光通透立体款 (Underlit Chrome - 当前首选 ⭐)** | `custom_icons/8_underlit_chrome.ico` | **你当前的最终拍板款**。引入汽车工业级地面反弹光设计（背部天光纯白 ➔ 中腹银灰 ➔ 最底腹部被底光重新打亮回纯白），彻底消除下半身暗沉，上下通透灵动，与 Zed / VS Code 完美呼应！ |
| **款式 7** | **白金亮钛立体款 (Bright Titanium)** | `custom_icons/7_bright_titanium.ico` | 腹部最暗处提亮至 #949EB0，整体亮度提升 35%，背部与腹部过渡柔和。 |
| **款式 5** | **钛金属立体雕刻款 (Titanium Sculpted)** | `custom_icons/5_titanium_sculpted.ico` | 早期钛金属款（腹部暗灰 #555E6B，背部高亮）。 |
| **款式 6** | **灵动柔光微浮雕款 (Luminous Relief)** | `custom_icons/6_luminous_relief.ico` | 柔和丝绸白银微光（#FFFFFF -> #B8C2CC），比例内收至 0.90，空间通透悬浮。 |
| **款式 3** | **居中纯白微光款 (Centered Monochrome)** | `custom_icons/3_dark_monochrome_centered.ico` | 早期偏心校准版（纯白平面微光贴纸风）。 |
| **款式 1** | **暗黑科技蓝款 (Dark Pro Blue)** | `custom_icons/1_dark_pro_blue.ico` | Zed 同款暗黑圆角底板 + DeepSeek 官方发光科技蓝渐变鲸鱼。 |
| **款式 2** | **纯悬浮无底板款 (Pure Floating Whale)** | `custom_icons/2_pure_floating_whale.ico` | 彻底剔除底板，将官方科技蓝鲸鱼居中放大 118%，呼应 Chrome/飞书悬浮通透风格。 |
| **款式 4** | **官方深海蓝底款 (Official Deep Blue)** | `custom_icons/4_official_deep_blue.ico` | 官方深海蓝渐变圆角底板（Squircle）+ 纯白微雕鲸鱼。 |

---

### 2. 各款式手动切换命令

```powershell
# 【款式 8 · 当前首选】应用环形底光立体款
uv run scripts/deepseek_harness/patch_deepseek_harness_icon.py --custom-icon scripts/deepseek_harness/custom_icons/8_underlit_chrome.ico --kill

# 【款式 7】应用白金亮钛款
uv run scripts/deepseek_harness/patch_deepseek_harness_icon.py --custom-icon scripts/deepseek_harness/custom_icons/7_bright_titanium.ico --kill

# 【款式 5】应用钛金属立体款
uv run scripts/deepseek_harness/patch_deepseek_harness_icon.py --custom-icon scripts/deepseek_harness/custom_icons/5_titanium_sculpted.ico --kill

# 【款式 1】应用暗黑科技蓝款
uv run scripts/deepseek_harness/patch_deepseek_harness_icon.py --custom-icon scripts/deepseek_harness/custom_icons/1_dark_pro_blue.ico --kill
```

---

### 3. 恢复官方原始状态

```powershell
uv run scripts/deepseek_harness/patch_deepseek_harness_icon.py --restore
```

---

## 三、VS Code 模块

模块路径：`go_projects\taskbarguard\scripts\vscode\`

### 1. 支持款式说明

| 款式参数 (`--style`) | 图标文件路径 | 特色说明 |
| :--- | :--- | :--- |
| **`blue` (科技蓝微光 · 默认推荐)** | `vscode_dark_blue.ico` | 经典 VS Code 分层蝴蝶缎带 + 科技蓝发光投影，辨识度高且完美融入暗黑底板，视觉平衡最佳 |
| **`silver` (极客银白款)** | `vscode_dark_silver.ico` | 纯白/银灰金属微光质感，与 Zed、DSH 底光鲸鱼达成 100% 黑白极简统一 |

---

### 2. 各款式手动切换命令

```powershell
# 1. 应用科技蓝微光款 (默认推荐)
uv run scripts/vscode/patch_vscode_icon.py --style blue --kill

# 2. 切换为极客银白款
uv run scripts/vscode/patch_vscode_icon.py --style silver --kill
```

---

### 3. 恢复官方原始状态

```powershell
uv run scripts/vscode/patch_vscode_icon.py --restore
```

---

## 四、执行须知与安全准则

1. **零黑屏安全刷新**：
   坚决不调用 `taskkill explorer.exe`。补丁写入后，脚本仅会触碰任务栏 `.lnk` 快捷方式时间戳 + 调用系统原生 `ie4uinit.exe -show` + 广播系统外壳 API `SHChangeNotify(SHCNE_ASSOCCHANGED)`。
2. **任务栏图标何时更新显示？**
   注入成功后，若任务栏已有固定图标，直接点击打开软件启动窗口，Windows 会立即抓取全新 PE 资源并在任务栏同步重绘。
