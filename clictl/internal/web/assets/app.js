// clictl WebUI：exe / 源码双目标注册、启动、控制台显隐与启动记录查看。
// 与 CLI 共用同一套 /api 契约（{ok, data} / {ok, error}）。
(function () {
  "use strict";

  const UNGROUPED = "未分组";
  const UNGROUPED_KEY = "__ungrouped__";
  const SORT_MODES = new Set(["name", "recent", "launches", "added"]);

  const state = {
    tools: [],
    running: new Map(), // name -> pids
    current: null,
    filter: "all",
    query: "",
    meta: null,
    viewMode: "flat",
    groupLayout: "tabs",
    sortMode: "name",
    activeGroup: "",
    collapsedGroups: new Set(),
    editingName: null,
    browseMode: "exe",
    startTarget: "",
    consolePID: 0,
  };

  const el = (id) => document.getElementById(id);
  const toolListEl = el("toolList");
  const countEl = el("toolCount");
  const searchEl = el("searchInput");
  const detailEl = el("detail");
  const emptyEl = el("emptyState");
  const toastEl = el("toast");

  let toastTimer = null;

  // ---------- 基础 ----------

  async function request(path, options) {
    const res = await fetch(path, options);
    let payload = null;
    try {
      payload = await res.json();
    } catch (err) {
      throw new Error(`接口返回非 JSON（HTTP ${res.status}）`);
    }
    if (!payload || payload.ok !== true) {
      const message = payload && payload.error ? payload.error.message : `HTTP ${res.status}`;
      const error = new Error(message);
      error.code = payload && payload.error ? payload.error.code : "";
      throw error;
    }
    return payload.data;
  }

  function post(path, body) {
    return request(path, {
      method: "POST",
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  }

  function put(path, body) {
    return request(path, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  }

  function toast(message, isError) {
    toastEl.textContent = message;
    toastEl.classList.toggle("error", !!isError);
    toastEl.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      toastEl.hidden = true;
    }, isError ? 5200 : 2600);
  }

  function escapeHtml(value) {
    return String(value == null ? "" : value)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#039;");
  }

  function formatBytes(n) {
    if (!n) return "0 B";
    const units = ["B", "KB", "MB", "GB"];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024;
      i++;
    }
    return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
  }

  function formatDuration(ms) {
    if (!ms) return "0s";
    const s = Math.round(ms / 1000);
    if (s < 60) return `${s}s`;
    const m = Math.floor(s / 60);
    const rs = s % 60;
    if (m < 60) return `${m}m ${rs}s`;
    const h = Math.floor(m / 60);
    return `${h}h ${m % 60}m`;
  }

  function formatTime(value) {
    if (!value) return "--";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return value;
    const pad = (v) => String(v).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  }

  async function withBusy(button, fn) {
    if (button) button.disabled = true;
    try {
      return await fn();
    } finally {
      if (button) button.disabled = false;
    }
  }

  function reportError(err) {
    toast(err.message || String(err), true);
    console.error(err);
  }

  // ---------- 目标辅助 ----------

  function defaultTarget(tool) {
    if (!tool) return "exe";
    if (tool.default_target === "source" && tool.source) return "source";
    if (tool.default_target === "exe" && tool.exe) return "exe";
    if (tool.exe) return "exe";
    if (tool.source) return "source";
    return "exe";
  }

  function targetData(tool, target) {
    if (!tool) return null;
    return target === "source" ? tool.source || null : tool.exe || null;
  }

  function targetActive(tool, target) {
    const t = targetData(tool, target);
    return !!(t && t.status === "active");
  }

  function targetConsole(tool, target) {
    if (target === "source") return !!(tool && tool.source && tool.source.type === "console");
    return !!(tool && tool.exe && tool.exe.console);
  }

  function targetMode(tool, target) {
    if (target === "source") return targetConsole(tool, target) ? "console" : "hidden";
    return targetConsole(tool, target) ? "console" : "detached";
  }

  function targetLabel(target) {
    return target === "source" ? "源码" : "exe";
  }

  function modeLabel(mode) {
    switch (mode) {
      case "run": return "前台";
      case "console": return "可见控制台";
      case "hidden": return "隐藏控制台";
      case "detached": return "无控制台";
      default: return mode || "--";
    }
  }

  function altAction(tool) {
    if (!tool) return null;
    const def = defaultTarget(tool);
    if (tool.exe && tool.source) {
      const other = def === "exe" ? "source" : "exe";
      const label = other === "source"
        ? (tool.source.type === "console" ? "终端启动" : "源码启动")
        : "启动 exe";
      return { target: other, mode: targetMode(tool, other), label };
    }
    if (def === "exe") {
      if (tool.exe && tool.exe.console) return { target: "exe", mode: "detached", label: "后台启动" };
      return null;
    }
    const mode = tool.source.type === "console" ? "hidden" : "console";
    return { target: "source", mode, label: mode === "console" ? "终端启动" : "后台启动" };
  }

  function primaryLabel(tool, target) {
    if (target === "source") return targetConsole(tool, target) ? "终端启动" : "源码启动";
    return targetConsole(tool, target) ? "终端启动" : "启动";
  }

  // ---------- 视图偏好 ----------

  let prefsTimer = null;

  async function loadPreferences() {
    try {
      const prefs = await request("/api/preferences");
      if (prefs) {
        const mode = prefs.view_mode === "tabs" || prefs.view_mode === "tree" ? prefs.view_mode : "flat";
        state.viewMode = mode;
        if (mode !== "flat") state.groupLayout = mode;
        state.sortMode = SORT_MODES.has(prefs.sort_mode) ? prefs.sort_mode : "name";
        state.activeGroup = typeof prefs.active_group === "string" ? prefs.active_group.trim() : "";
        state.collapsedGroups = new Set(
          Array.isArray(prefs.collapsed_groups)
            ? prefs.collapsed_groups.filter((name) => typeof name === "string" && name.trim())
            : []
        );
      }
    } catch (err) {
      console.error(err);
    }
    updateViewControls();
  }

  function savePreferences() {
    clearTimeout(prefsTimer);
    prefsTimer = setTimeout(async () => {
      try {
        await post("/api/preferences", {
          view_mode: state.viewMode,
          sort_mode: state.sortMode,
          active_group: state.activeGroup,
          collapsed_groups: Array.from(state.collapsedGroups),
        });
      } catch (err) {
        console.error(err);
      }
    }, 250);
  }

  function updateViewControls() {
    const grouped = state.viewMode !== "flat";
    el("viewToggle")
      .querySelectorAll(".view-btn")
      .forEach((btn) =>
        btn.classList.toggle("active", grouped ? btn.dataset.view === "grouped" : btn.dataset.view === "flat")
      );
    const layoutToggle = el("groupLayoutToggle");
    layoutToggle.hidden = !grouped;
    layoutToggle
      .querySelectorAll(".view-btn")
      .forEach((btn) => btn.classList.toggle("active", btn.dataset.layout === state.viewMode));
    el("sortSelect").value = state.sortMode;
  }

  // ---------- 分组与排序 ----------

  function groupOf(tool) {
    const group = tool.meta && typeof tool.meta.group === "string" ? tool.meta.group.trim() : "";
    return group || UNGROUPED;
  }

  function groupKey(tool) {
    const group = groupOf(tool);
    return group === UNGROUPED ? UNGROUPED_KEY : group;
  }

  function compareName(a, b) {
    return a.name.localeCompare(b.name, "zh-Hans-CN", { sensitivity: "base" });
  }

  function sortTools(list) {
    const arr = [...list];
    switch (state.sortMode) {
      case "recent":
        arr.sort((a, b) => {
          if (!a.last_launch && !b.last_launch) return compareName(a, b);
          if (!a.last_launch) return 1;
          if (!b.last_launch) return -1;
          if (a.last_launch === b.last_launch) return compareName(a, b);
          return a.last_launch < b.last_launch ? 1 : -1;
        });
        break;
      case "launches":
        arr.sort((a, b) => (b.launch_count || 0) - (a.launch_count || 0) || compareName(a, b));
        break;
      case "added":
        arr.sort((a, b) => {
          if (a.added_at === b.added_at) return compareName(a, b);
          return a.added_at < b.added_at ? 1 : -1;
        });
        break;
      default:
        arr.sort(compareName);
    }
    return arr;
  }

  // ---------- 列表 ----------

  async function loadMeta() {
    try {
      state.meta = await request("/api/meta");
      const badge = el("channelBadge");
      badge.textContent = (state.meta.channel || "?").toUpperCase();
      badge.className = `channel-badge ${state.meta.channel || ""}`;
      badge.title = `版本 ${state.meta.version}\n数据库 ${state.meta.dbPath}`;
    } catch (err) {
      console.error(err);
    }
  }

  async function loadTools() {
    try {
      const [tools, running] = await Promise.all([
        request("/api/tools"),
        request("/api/tools?running=1").catch(() => []),
      ]);
      state.tools = tools || [];
      state.running = new Map((running || []).map((item) => [item.name, item.running_pids || []]));
      renderList();
      if (!state.current && !state.autoSelected && state.tools.length > 0) {
        state.autoSelected = true;
        await selectTool(state.tools[0].name);
      }
      if (state.current) {
        const alive = state.tools.find((t) => t.name === state.current.tool.name);
        if (alive) {
          await selectTool(alive.name, { keepScroll: true, silent: true });
        }
      }
    } catch (err) {
      toolListEl.innerHTML = `<div class="empty-hint">加载失败：${escapeHtml(err.message)}</div>`;
    }
  }

  function filteredTools() {
    return state.tools.filter((tool) => {
      if (state.filter === "running" && !state.running.has(tool.name)) return false;
      if (state.filter === "active" && tool.status !== "active") return false;
      if (state.filter === "invalid" && tool.status !== "invalid") return false;
      if (!state.query) return true;
      const q = state.query.toLowerCase();
      const hay = [
        tool.name,
        tool.path,
        tool.description,
        tool.exe ? tool.exe.path : "",
        tool.source ? tool.source.program : "",
        tool.source ? tool.source.workdir : "",
        tool.source ? tool.source.command : "",
        tool.source ? (tool.source.args || []).join(" ") : "",
      ].join(" ").toLowerCase();
      return hay.includes(q);
    });
  }

  function renderCard(tool) {
    const running = state.running.has(tool.name);
    const active = state.current && state.current.tool.name === tool.name ? "active" : "";
    const invalid = tool.status === "invalid" ? "invalid" : "";
    const group = groupOf(tool);
    const groupTag =
      group !== UNGROUPED ? `<span class="card-group" title="分组">${escapeHtml(group)}</span>` : "";
    const exeBadge = tool.exe ? '<span class="card-kind exe">exe</span>' : "";
    const sourceBadge = tool.source ? '<span class="card-kind source">源码</span>' : "";
    let warn = "";
    if (tool.status === "invalid") {
      warn = '<span class="tag-invalid">失效</span>';
    } else if (tool.exe && tool.exe.status === "invalid") {
      warn = '<span class="tag-warn">exe 失效</span>';
    } else if (tool.source && tool.source.status === "invalid") {
      warn = '<span class="tag-warn">源码失效</span>';
    }
    const pathText = tool.exe ? tool.exe.path : tool.source ? tool.source.command : tool.path;
    return `
      <div class="tool-card ${active} ${invalid}" data-name="${escapeHtml(tool.name)}">
        <div class="card-top">
          ${running ? '<span class="pulse" title="后台运行中"></span>' : ""}
          <span class="card-name">${escapeHtml(tool.name)}</span>
          ${exeBadge}
          ${sourceBadge}
          ${groupTag}
          ${warn}
          <button type="button" class="card-group-btn" data-group-for="${escapeHtml(tool.name)}" title="设置分组">分组</button>
        </div>
        <div class="card-path" title="${escapeHtml(tool.path)}">${escapeHtml(pathText)}</div>
        <div class="card-foot">
          <span>${tool.launch_count || 0} 次启动</span>
          <span>${tool.last_launch ? formatTime(tool.last_launch) : "从未启动"}</span>
        </div>
      </div>`;
  }

  function bindCards() {
    toolListEl.querySelectorAll(".tool-card").forEach((card) => {
      card.addEventListener("click", () => {
        selectTool(card.dataset.name).catch(reportError);
      });
    });
    toolListEl.querySelectorAll(".card-group-btn").forEach((btn) => {
      btn.addEventListener("click", (event) => {
        event.stopPropagation();
        selectTool(btn.dataset.groupFor)
          .then(() => openGroupDialog())
          .catch(reportError);
      });
    });
  }

  function renderList() {
    const items = sortTools(filteredTools());
    toolListEl.classList.toggle("grouped", state.viewMode !== "flat");
    if (state.viewMode === "tabs") {
      renderTabs(items);
      return;
    }
    if (state.viewMode === "tree") {
      renderTree(items);
      return;
    }
    el("groupTabs").hidden = true;
    el("groupHint").hidden = true;
    countEl.textContent = `${items.length} 个`;
    if (items.length === 0) {
      toolListEl.innerHTML = `<div class="empty-hint">${state.tools.length === 0 ? "尚未注册任何工具" : "没有匹配的工具"}</div>`;
      return;
    }
    toolListEl.innerHTML = items.map(renderCard).join("");
    bindCards();
  }

  // 分组树：按组垂直折叠，状态持久化到 collapsed_groups，与标签页共用同一套偏好。
  function renderTree(items) {
    el("groupTabs").hidden = true;
    el("groupHint").hidden = true;

    const groups = new Map();
    for (const tool of items) {
      const key = groupKey(tool);
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(tool);
    }
    const keys = [...groups.keys()].sort((a, b) => {
      if (a === UNGROUPED_KEY) return 1;
      if (b === UNGROUPED_KEY) return -1;
      return String(a).localeCompare(String(b), "zh-Hans-CN", { sensitivity: "base" });
    });

    countEl.textContent = `${items.length} 个`;
    if (items.length === 0) {
      toolListEl.innerHTML = `<div class="empty-hint">${state.tools.length === 0 ? "尚未注册任何工具" : "没有匹配的工具"}</div>`;
      return;
    }

    toolListEl.innerHTML = keys
      .map((key) => {
        const groupItems = groups.get(key);
        const label = key === UNGROUPED_KEY ? UNGROUPED : key;
        const collapsed = state.collapsedGroups.has(key);
        const runningCount = groupItems.filter((tool) => state.running.has(tool.name)).length;
        return `
          <div class="tree-group ${collapsed ? "collapsed" : ""}">
            <div class="tree-group-header" data-group-toggle="${escapeHtml(key)}">
              <span class="tree-arrow">▾</span>
              <span class="tree-dot"></span>
              <span class="tree-title">${escapeHtml(label)}</span>
              ${runningCount ? `<span class="tree-running">${runningCount} 运行中</span>` : ""}
              <span class="tree-count">${groupItems.length}</span>
            </div>
            <div class="tree-group-items">${groupItems.map(renderCard).join("")}</div>
          </div>`;
      })
      .join("");

    toolListEl.querySelectorAll("[data-group-toggle]").forEach((header) => {
      header.addEventListener("click", () => {
        const key = header.dataset.groupToggle;
        const groupEl = header.closest(".tree-group");
        if (state.collapsedGroups.has(key)) {
          state.collapsedGroups.delete(key);
          groupEl.classList.remove("collapsed");
        } else {
          state.collapsedGroups.add(key);
          groupEl.classList.add("collapsed");
        }
        savePreferences();
      });
    });
    bindCards();
  }

  function renderTabs(items) {
    const groups = existingGroups();
    const validKeys = new Set(["", UNGROUPED_KEY, ...groups]);
    if (!validKeys.has(state.activeGroup)) {
      state.activeGroup = "";
      savePreferences();
    }

    const counts = new Map();
    const runningGroups = new Set();
    for (const tool of items) {
      const key = groupKey(tool);
      counts.set(key, (counts.get(key) || 0) + 1);
      if (state.running.has(tool.name)) runningGroups.add(key);
    }

    const tabs = [{ key: "", label: "全部", count: items.length, running: runningGroups.has(UNGROUPED_KEY) }];
    if (groups.length > 0) {
      for (const name of groups) {
        tabs.push({ key: name, label: name, count: counts.get(name) || 0, running: runningGroups.has(name) });
      }
      tabs.push({
        key: UNGROUPED_KEY,
        label: UNGROUPED,
        count: counts.get(UNGROUPED_KEY) || 0,
        running: runningGroups.has(UNGROUPED_KEY),
      });
    }

    const tabsEl = el("groupTabs");
    tabsEl.hidden = false;
    el("groupHint").hidden = groups.length > 0;
    tabsEl.innerHTML = tabs
      .map(
        (tab) => `
        <button type="button" class="group-tab ${tab.key === state.activeGroup ? "active" : ""}" data-group="${escapeHtml(tab.key)}" role="tab" aria-selected="${tab.key === state.activeGroup}">
          ${tab.running ? '<span class="tab-pulse" title="该组有后台实例"></span>' : ""}
          <span class="tab-label">${escapeHtml(tab.label)}</span>
          <span class="tab-count">${tab.count}</span>
        </button>`
      )
      .join("");

    const visible =
      state.activeGroup === "" ? items : items.filter((tool) => groupKey(tool) === state.activeGroup);
    countEl.textContent = `${visible.length} 个`;
    toolListEl.innerHTML = visible.length
      ? visible.map(renderCard).join("")
      : '<div class="empty-hint">该分组下没有匹配的工具</div>';
    bindCards();
  }

  // ---------- 详情 ----------

  async function selectTool(name, opts) {
    const options = opts || {};
    try {
      const info = await request(`/api/tools/${encodeURIComponent(name)}`);
      state.current = info;
      renderDetail(info);
      renderList();
      if (!options.keepScroll) {
        detailEl.scrollTop = 0;
      }
    } catch (err) {
      if (!options.silent) throw err;
    }
  }

  function renderDetail(info) {
    emptyEl.hidden = true;
    detailEl.hidden = false;

    const tool = info.tool;
    const def = defaultTarget(tool);
    const running = info.running && info.running.alive;
    el("detailName").textContent = tool.name;
    const instances = (info.running && info.running.instances) || [];

    let typeText = "窗口程序";
    let typeClass = "gui";
    if (tool.kind === "both") {
      typeText = "exe + 源码";
      typeClass = "both";
    } else if (tool.kind === "source" || (!tool.exe && tool.source)) {
      typeText = "源码命令";
      typeClass = "source";
    } else if (targetConsole(tool, "exe")) {
      typeText = "命令行";
      typeClass = "console";
    }
    el("detailType").textContent = typeText;
    el("detailType").className = `type-badge ${typeClass}`;

    const invalidTargets = [];
    if (tool.exe && tool.exe.status !== "active") invalidTargets.push("exe");
    if (tool.source && tool.source.status !== "active") invalidTargets.push("源码");
    let statusText;
    if (tool.status !== "active") {
      statusText = "失效";
    } else if (running) {
      statusText = "运行中";
    } else if (invalidTargets.length > 0) {
      statusText = "部分失效";
    } else {
      statusText = "就绪";
    }
    el("detailStatus").textContent = statusText;
    const dot = el("detailStatusDot");
    dot.className = `status-dot ${tool.status === "active" ? "ok" : "bad"}`;

    el("startBtn").textContent = primaryLabel(tool, def);
    el("startBtn").title = `默认目标：${targetLabel(def)}`;
    el("startBtn").disabled = !targetActive(tool, def);

    const alt = altAction(tool);
    el("startAltBtn").hidden = !alt;
    if (alt) {
      el("startAltBtn").textContent = alt.label;
      el("startAltBtn").title = `${targetLabel(alt.target)} · ${modeLabel(alt.mode)}`;
      el("startAltBtn").disabled = !targetActive(tool, alt.target);
    }

    el("consoleBtn").hidden = !instances.some((i) => i.mode === "hidden" || i.mode === "console");
    el("stopBtn").disabled = !running;

    el("detailDesc").textContent = tool.description || "未填写描述";
    el("statLaunches").textContent = String(tool.launch_count || 0);
    el("statDuration").textContent = formatDuration(info.total_duration_ms);
    if (tool.exe) {
      el("statSizeLabel").textContent = "exe 大小";
      el("statSize").textContent = formatBytes(tool.exe.size_bytes);
    } else {
      el("statSizeLabel").textContent = "启动方式";
      el("statSize").textContent = "源码命令";
    }
    el("statPids").textContent = running && info.running.pids.length ? info.running.pids.join(", ") : "无";

    const groupChip = el("detailGroupChip");
    const group = groupOf(tool);
    groupChip.textContent = group === UNGROUPED ? "设置分组" : group;
    groupChip.title = group === UNGROUPED ? "点击为该工具设置分组" : `当前分组「${group}」，点击修改`;
    groupChip.classList.toggle("unset", group === UNGROUPED);

    let metaText = "（无）";
    if (tool.meta && Object.keys(tool.meta).length > 0) {
      metaText = JSON.stringify(tool.meta, null, 2);
    }
    el("detailMeta").textContent = metaText;

    // 启动目标区块
    el("targetList").hidden = !tool.exe && !tool.source;
    el("exeTargetRow").hidden = !tool.exe;
    el("sourceTargetRow").hidden = !tool.source;
    if (tool.exe) {
      el("exeTargetPath").textContent = tool.exe.path;
      el("exeTargetStatus").textContent = tool.exe.status === "active" ? "就绪" : "失效";
      el("exeTargetStatus").className = `target-status ${tool.exe.status === "active" ? "ok" : "bad"}`;
      el("exeStartBtn").disabled = tool.exe.status !== "active";
      el("exeStartBtn").textContent = tool.exe.console ? "终端启动" : "启动 exe";
      el("exeRevealBtn").disabled = tool.exe.status !== "active";
    }
    if (tool.source) {
      el("sourceTargetCommand").textContent = tool.source.command || `${tool.source.program} ${(tool.source.args || []).join(" ")}`;
      el("sourceTargetStatus").textContent = tool.source.status === "active" ? "就绪" : "失效";
      el("sourceTargetStatus").className = `target-status ${tool.source.status === "active" ? "ok" : "bad"}`;
      el("sourceTargetDetail").textContent = `工作目录 ${tool.source.workdir} · ${tool.source.type === "console" ? "命令行" : "窗口程序"}`;
      const srcConsole = tool.source.type === "console";
      el("sourceStartBtn").textContent = srcConsole ? "终端启动" : "源码启动";
      el("sourceConsoleBtn").textContent = srcConsole ? "后台启动" : "终端启动";
      el("sourceStartBtn").disabled = tool.source.status !== "active";
      el("sourceConsoleBtn").disabled = tool.source.status !== "active";
      el("sourceRevealBtn").disabled = tool.source.status !== "active";
    }

    // 带参数启动目标下拉
    const targetSelect = el("startTarget");
    const options = [`<option value="">默认 (${targetLabel(def)})</option>`];
    if (tool.exe) options.push(`<option value="exe"${tool.exe.status === "active" ? "" : " disabled"}>exe</option>`);
    if (tool.source) options.push(`<option value="source"${tool.source.status === "active" ? "" : " disabled"}>源码</option>`);
    targetSelect.innerHTML = options.join("");
    const validChoices = new Set(["", ...(tool.exe ? ["exe"] : []), ...(tool.source ? ["source"] : [])]);
    if (!validChoices.has(state.startTarget)) state.startTarget = "";
    targetSelect.value = state.startTarget;
    targetSelect.disabled = !(tool.exe && tool.source);
    el("runRow").hidden = !tool.exe && !tool.source;

    // 字段区块
    el("exeFieldBlock").hidden = !tool.exe;
    if (tool.exe) el("detailPath").textContent = tool.exe.path;
    el("sourceFieldBlock").hidden = !tool.source;
    if (tool.source) {
      el("detailSourceProgram").textContent = tool.source.program;
      el("detailSourceWorkdir").textContent = tool.source.workdir;
      el("detailSourceArgs").textContent = (tool.source.args || []).length ? tool.source.args.join(" ") : "（无）";
      el("detailSourceType").textContent = tool.source.type === "console" ? "命令行" : "窗口程序";
      el("sourceWorkdirOpenBtn").disabled = tool.source.status !== "active";
    }

    // 运行实例
    el("instanceBlock").hidden = instances.length === 0;
    if (instances.length) {
      el("instanceList").innerHTML = instances
        .map((inst) => {
          const canConsole = inst.mode === "hidden" || inst.mode === "console";
          const actions = canConsole
            ? `<button type="button" class="btn" data-console-pid="${inst.pid}" data-console-visible="1">显示控制台</button>
               <button type="button" class="btn" data-console-pid="${inst.pid}" data-console-visible="0">隐藏</button>`
            : `<span class="instance-note">${modeLabel(inst.mode)}</span>`;
          return `<div class="instance-row">
            <span class="instance-pid">PID ${inst.pid}</span>
            <span class="instance-target">${targetLabel(inst.target)}</span>
            <span class="instance-mode">${modeLabel(inst.mode)}</span>
            <span class="instance-time">${formatTime(inst.started_at)}</span>
            <span class="instance-actions">${actions}</span>
          </div>`;
        })
        .join("");
      el("instanceList")
        .querySelectorAll("[data-console-pid]")
        .forEach((btn) => {
          btn.addEventListener("click", () => {
            consoleAction(Number(btn.dataset.consolePid), btn.dataset.consoleVisible === "1", btn);
          });
        });
    }

    const rows = info.recent_launches || [];
    el("launchBody").innerHTML = rows.length
      ? rows
          .map((launch) => {
            const duration = launch.duration_ms == null ? "未闭环" : formatDuration(launch.duration_ms);
            const code = launch.exit_code == null ? "--" : String(launch.exit_code);
            const pid = launch.pid == null ? "--" : String(launch.pid);
            const target = launch.target === "source" ? "源码" : launch.target === "exe" ? "exe" : "--";
            const mode = modeLabel(launch.mode);
            return `<tr><td>${formatTime(launch.started_at)}</td><td>${target}</td><td>${mode}</td><td>${duration}</td><td>${code}</td><td>${pid}</td></tr>`;
          })
          .join("")
      : '<tr><td colspan="6" class="muted">暂无启动记录</td></tr>';
  }

  // ---------- 动作 ----------

  async function startTool(target, args, button, mode) {
    if (!state.current) return;
    const tool = state.current.tool;
    const name = tool.name;
    if (!targetActive(tool, target)) {
      toast(`${targetLabel(target)} 目标不可用`, true);
      return;
    }
    await withBusy(button, async () => {
      try {
        const payload = { target, args: args || [] };
        payload.mode = mode || targetMode(tool, target);
        const res = await post(`/api/tools/${encodeURIComponent(name)}/start`, payload);
        const where = res.mode === "console" ? "可见控制台" : res.mode === "hidden" ? "隐藏控制台" : "后台";
        if (res.already_running) {
          toast(`${name} 已在${where}启动（${targetLabel(res.target)} · PID ${res.pid}），已有实例在运行`);
        } else {
          toast(`已在${where}启动 ${name}（${targetLabel(res.target)} · PID ${res.pid}）`);
        }
        await loadTools();
      } catch (err) {
        reportError(err);
      }
    });
  }

  async function startAlt(button) {
    if (!state.current) return;
    const alt = altAction(state.current.tool);
    if (!alt) return;
    await startTool(alt.target, [], button, alt.mode);
  }

  async function stopTool(button) {
    if (!state.current) return;
    const name = state.current.tool.name;
    await withBusy(button, async () => {
      try {
        const res = await post(`/api/tools/${encodeURIComponent(name)}/stop`);
        if (res.already_stopped) {
          toast(`${name} 没有后台实例`);
        } else if (res.failed && res.failed.length > 0) {
          toast(`${name} 有 ${res.failed.length} 个实例未能终止`, true);
        } else {
          toast(`已停止 ${name}（${(res.killed || []).length} 个实例）`);
        }
        await loadTools();
      } catch (err) {
        reportError(err);
      }
    });
  }

  async function consoleAction(pid, visible, button) {
    if (!state.current) return;
    const name = state.current.tool.name;
    await withBusy(button, async () => {
      try {
        const res = await post(`/api/tools/${encodeURIComponent(name)}/console`, { pid, visible });
        toast(`${visible ? "已显示" : "已隐藏"}控制台（PID ${res.pid}）`);
        await selectTool(name, { keepScroll: true, silent: true });
      } catch (err) {
        reportError(err);
      }
    });
  }

  async function reveal(target) {
    if (!state.current) return;
    try {
      await post(`/api/tools/${encodeURIComponent(state.current.tool.name)}/reveal`, { target });
    } catch (err) {
      reportError(err);
    }
  }

  function openConsoleDialog(pid) {
    if (!state.current) return;
    const instances = (state.current.running && state.current.running.instances) || [];
    let inst = null;
    if (pid) {
      inst = instances.find((i) => i.pid === pid) || null;
    } else {
      inst = [...instances].reverse().find((i) => i.mode === "hidden" || i.mode === "console") || null;
    }
    if (!inst) {
      toast("没有可操作的控制台实例", true);
      return;
    }
    state.consolePID = inst.pid;
    el("consoleHint").textContent =
      `PID ${inst.pid} · ${targetLabel(inst.target)} · ${modeLabel(inst.mode)}。显示/隐藏不会重启进程；关闭控制台窗口会结束进程。`;
    el("consoleDialog").showModal();
  }

  function existingGroups() {
    const groups = new Set();
    for (const tool of state.tools) {
      const group = groupOf(tool);
      if (group !== UNGROUPED && group !== UNGROUPED_KEY) groups.add(group);
    }
    return [...groups].sort((a, b) => a.localeCompare(b, "zh-Hans-CN", { sensitivity: "base" }));
  }

  function renderGroupOptions() {
    const groups = existingGroups();
    el("groupOptions").innerHTML = groups
      .map((group) => `<option value="${escapeHtml(group)}"></option>`)
      .join("");
    const chips = [`<button type="button" class="group-chip-option" data-group="">未分组</button>`].concat(
      groups.map(
        (group) =>
          `<button type="button" class="group-chip-option" data-group="${escapeHtml(group)}">${escapeHtml(group)}</button>`
      )
    );
    el("groupChips").innerHTML = chips.join("");
    el("groupChips")
      .querySelectorAll(".group-chip-option")
      .forEach((chip) => {
        chip.addEventListener("click", () => {
          el("groupDialogInput").value = chip.dataset.group || "";
          el("groupDialogInput").focus();
        });
      });
  }

  function openGroupDialog() {
    if (!state.current) return;
    renderGroupOptions();
    const group = groupOf(state.current.tool);
    el("groupDialogInput").value = group === UNGROUPED ? "" : group;
    el("groupDialog").showModal();
    el("groupDialogInput").focus();
  }

  async function submitGroup(event) {
    event.preventDefault();
    if (!state.current) return;
    const name = state.current.tool.name;
    const group = el("groupDialogInput").value.trim();
    const meta = Object.assign({}, state.current.tool.meta || {});
    if (group) {
      meta.group = group;
    } else {
      delete meta.group;
    }
    await withBusy(event.submitter, async () => {
      try {
        await put(`/api/tools/${encodeURIComponent(name)}/meta`, { meta });
        el("groupDialog").close();
        toast(group ? `已归入分组「${group}」` : "已移出分组");
        await selectTool(name);
        await loadTools();
      } catch (err) {
        reportError(err);
      }
    });
  }

  function parseArgs(input) {
    const args = [];
    let current = "";
    let quote = "";
    let hasToken = false;
    for (const ch of input) {
      if (quote) {
        if (ch === quote) {
          quote = "";
        } else {
          current += ch;
        }
        continue;
      }
      if (ch === '"' || ch === "'") {
        quote = ch;
        hasToken = true;
        continue;
      }
      if (/\s/.test(ch)) {
        if (hasToken || current) {
          args.push(current);
          current = "";
          hasToken = false;
        }
        continue;
      }
      current += ch;
      hasToken = true;
    }
    if (hasToken || current) args.push(current);
    return args;
  }

  // ---------- 注册 / 编辑 ----------

  function openCreateDialog() {
    openAddDialog(null);
  }

  function openEditDialog() {
    if (state.current) openAddDialog(state.current.tool);
  }

  function openAddDialog(tool) {
    state.editingName = tool ? tool.name : null;
    el("addDialogTitle").textContent = tool ? "编辑工具" : "注册工具";
    el("addSubmitBtn").textContent = tool ? "保存" : "注册";
    el("addForm").reset();
    el("addName").disabled = !!tool;

    el("addExeEnabled").checked = tool ? !!tool.exe : true;
    el("addSourceEnabled").checked = tool ? !!tool.source : false;
    el("addPath").value = tool && tool.exe ? tool.exe.path : "";
    el("addSourceProgram").value = tool && tool.source ? tool.source.program : "";
    el("addSourceArgs").value = tool && tool.source ? (tool.source.args || []).join(" ") : "";
    el("addSourceWorkdir").value = tool && tool.source ? tool.source.workdir : "";
    el("addSourceType").value = tool && tool.source ? tool.source.type : "gui";
    el("addDefaultTarget").value = tool ? defaultTarget(tool) : "exe";
    el("addName").value = tool ? tool.name : "";
    el("addDesc").value = tool ? tool.description : "";
    const meta = tool && tool.meta ? tool.meta : null;
    el("addMeta").value = meta && Object.keys(meta).length ? JSON.stringify(meta) : "";
    el("addGroup").value = meta && typeof meta.group === "string" ? meta.group : "";

    updateAddSections();
    renderGroupOptions();
    el("addDialog").showModal();
  }

  function updateAddSections() {
    const exeOn = el("addExeEnabled").checked;
    const srcOn = el("addSourceEnabled").checked;
    el("addExeFields").hidden = !exeOn;
    el("addSourceFields").hidden = !srcOn;
    el("addDefaultField").hidden = !(exeOn && srcOn);
    if (exeOn && !srcOn) {
      el("addDefaultTarget").value = "exe";
    } else if (!exeOn && srcOn) {
      el("addDefaultTarget").value = "source";
    } else if (exeOn && srcOn) {
      const v = el("addDefaultTarget").value;
      if (v !== "exe" && v !== "source") el("addDefaultTarget").value = "exe";
    }
  }

  function collectAddForm() {
    const exeOn = el("addExeEnabled").checked;
    const srcOn = el("addSourceEnabled").checked;
    if (!exeOn && !srcOn) throw new Error("exe 与源码命令至少启用一项");

    let meta = null;
    const metaText = el("addMeta").value.trim();
    if (metaText) {
      try {
        meta = JSON.parse(metaText);
      } catch (err) {
        throw new Error("meta 不是合法 JSON");
      }
    }
    const group = el("addGroup").value.trim();
    if (group) {
      if (!meta || typeof meta !== "object" || Array.isArray(meta)) meta = {};
      meta.group = group;
    } else if (meta && typeof meta === "object" && !Array.isArray(meta)) {
      delete meta.group;
    }

    const exePath = el("addPath").value.trim();
    const source = {
      program: el("addSourceProgram").value.trim(),
      workdir: el("addSourceWorkdir").value.trim(),
      args: parseArgs(el("addSourceArgs").value),
      type: el("addSourceType").value,
    };
    if (exeOn && !exePath) throw new Error("已启用可执行文件，请填写 exe 路径");
    if (srcOn && !source.program) throw new Error("已启用源码命令，请填写程序");
    if (srcOn && !source.workdir) throw new Error("已启用源码命令，请填写工作目录");

    return {
      name: el("addName").value.trim(),
      desc: el("addDesc").value.trim(),
      meta,
      default_target: exeOn && srcOn ? el("addDefaultTarget").value : exeOn ? "exe" : "source",
      exe: exeOn ? { path: exePath } : null,
      source: srcOn ? source : null,
    };
  }

  async function submitAdd(event) {
    event.preventDefault();
    let payload;
    try {
      payload = collectAddForm();
    } catch (err) {
      toast(err.message, true);
      return;
    }
    await withBusy(el("addSubmitBtn"), async () => {
      try {
        let tool;
        if (state.editingName) {
          tool = await put(`/api/tools/${encodeURIComponent(state.editingName)}`, payload);
        } else {
          tool = await post("/api/tools", payload);
        }
        el("addDialog").close();
        toast(`${state.editingName ? "已保存" : "已注册"} ${tool.name}`);
        state.editingName = null;
        await loadTools();
        await selectTool(tool.name);
      } catch (err) {
        reportError(err);
      }
    });
  }

  // ---------- 目录浏览 ----------

  let browseDir = "";

  async function openBrowse(mode) {
    state.browseMode = mode;
    el("browseTitle").textContent =
      mode === "workdir" ? "选择工作目录" : mode === "program" ? "选择程序" : "选择 exe";
    el("browseSelectDir").hidden = mode !== "workdir";
    el("browseDialog").showModal();
    let start = browseDir;
    if (mode === "workdir" && el("addSourceWorkdir").value.trim()) {
      start = el("addSourceWorkdir").value.trim();
    }
    await loadBrowse(start || "");
  }

  async function loadBrowse(dir) {
    try {
      const data = await request(`/api/fs${dir ? `?dir=${encodeURIComponent(dir)}` : ""}`);
      browseDir = data.dir || "";
      el("browsePath").textContent = browseDir || "选择磁盘";
      const list = el("browseList");
      const items = [];
      if (!browseDir) {
        (data.drives || []).forEach((drive) => {
          items.push(`<button type="button" class="browse-item" data-dir="${escapeHtml(drive)}"><span class="ico">▣</span>${escapeHtml(drive)}</button>`);
        });
      } else {
        (data.entries || []).forEach((entry) => {
          if (entry.isDir) {
            items.push(`<button type="button" class="browse-item" data-dir="${escapeHtml(entry.path)}"><span class="ico">▸</span>${escapeHtml(entry.name)}</button>`);
          } else {
            items.push(`<button type="button" class="browse-item" data-file="${escapeHtml(entry.path)}"><span class="ico">›</span>${escapeHtml(entry.name)}<span class="size">${formatBytes(entry.size)}</span></button>`);
          }
        });
      }
      list.innerHTML = items.join("") || '<div class="empty-hint">没有子目录或 .exe</div>';

      list.querySelectorAll(".browse-item").forEach((item) => {
        item.addEventListener("click", () => {
          if (item.dataset.dir) {
            loadBrowse(item.dataset.dir);
            return;
          }
          const file = item.dataset.file;
          if (!file) return;
          if (state.browseMode === "workdir") return;
          if (state.browseMode === "program") {
            el("addSourceProgram").value = file;
            if (!el("addName").value) {
              const base = file.split("\\").pop().replace(/\.exe$/i, "");
              el("addName").value = base.toLowerCase();
            }
            el("browseDialog").close();
            return;
          }
          el("addPath").value = file;
          if (!el("addName").value) {
            const base = file.split("\\").pop().replace(/\.exe$/i, "");
            el("addName").value = base.toLowerCase();
          }
          el("browseDialog").close();
        });
      });
    } catch (err) {
      reportError(err);
    }
  }

  function selectBrowseDir() {
    if (state.browseMode !== "workdir" || !browseDir) return;
    el("addSourceWorkdir").value = browseDir;
    el("browseDialog").close();
  }

  // ---------- meta ----------

  function openMetaDialog() {
    if (!state.current) return;
    const meta = state.current.tool.meta;
    el("metaInput").value = meta && Object.keys(meta).length ? JSON.stringify(meta, null, 2) : "{}";
    el("metaDialog").showModal();
  }

  async function submitMeta(event) {
    event.preventDefault();
    if (!state.current) return;
    let meta;
    try {
      meta = JSON.parse(el("metaInput").value || "{}");
    } catch (err) {
      toast("meta 不是合法 JSON", true);
      return;
    }
    try {
      await put(`/api/tools/${encodeURIComponent(state.current.tool.name)}/meta`, { meta });
      el("metaDialog").close();
      toast("meta 已更新");
      await selectTool(state.current.tool.name);
      await loadTools();
    } catch (err) {
      reportError(err);
    }
  }

  // ---------- 初始化 ----------

  function bind() {
    el("refreshBtn").addEventListener("click", () => {
      loadMeta();
      loadTools();
    });

    searchEl.addEventListener("input", () => {
      state.query = searchEl.value.trim();
      renderList();
    });

    el("filters").addEventListener("click", (event) => {
      const chip = event.target.closest(".chip");
      if (!chip) return;
      state.filter = chip.dataset.filter;
      el("filters").querySelectorAll(".chip").forEach((item) => item.classList.toggle("active", item === chip));
      renderList();
    });

    el("viewToggle").addEventListener("click", (event) => {
      const btn = event.target.closest(".view-btn");
      if (!btn) return;
      const next = btn.dataset.view === "grouped" ? state.groupLayout : "flat";
      if (next === state.viewMode) return;
      state.viewMode = next;
      updateViewControls();
      renderList();
      savePreferences();
    });

    el("groupLayoutToggle").addEventListener("click", (event) => {
      const btn = event.target.closest(".view-btn");
      if (!btn) return;
      const next = btn.dataset.layout === "tree" ? "tree" : "tabs";
      if (next === state.viewMode) return;
      state.viewMode = next;
      state.groupLayout = next;
      updateViewControls();
      renderList();
      savePreferences();
    });

    el("groupTabs").addEventListener("click", (event) => {
      const tab = event.target.closest(".group-tab");
      if (!tab) return;
      const next = tab.dataset.group || "";
      if (next === state.activeGroup) return;
      state.activeGroup = next;
      renderList();
      toolListEl.scrollTop = 0;
      savePreferences();
      const outside = next !== "" && (!state.current || groupKey(state.current.tool) !== next);
      const first = outside
        ? sortTools(filteredTools()).find((tool) => groupKey(tool) === next)
        : null;
      if (first) selectTool(first.name).catch(reportError);
    });

    el("sortSelect").addEventListener("change", () => {
      const value = el("sortSelect").value;
      state.sortMode = SORT_MODES.has(value) ? value : "name";
      renderList();
      savePreferences();
    });

    el("detailGroupChip").addEventListener("click", openGroupDialog);
    el("groupCancel").addEventListener("click", () => el("groupDialog").close());
    el("groupForm").addEventListener("submit", submitGroup);

    el("startBtn").addEventListener("click", (event) => {
      if (!state.current) return;
      const tool = state.current.tool;
      const target = defaultTarget(tool);
      startTool(target, [], event.currentTarget, targetMode(tool, target));
    });
    el("startAltBtn").addEventListener("click", (event) => startAlt(event.currentTarget));
    el("startArgsBtn").addEventListener("click", (event) => {
      if (!state.current) return;
      const tool = state.current.tool;
      const target = state.startTarget || defaultTarget(tool);
      startTool(target, parseArgs(el("startArgs").value), event.currentTarget, targetMode(tool, target));
    });
    el("startTarget").addEventListener("change", () => {
      state.startTarget = el("startTarget").value;
    });
    el("stopBtn").addEventListener("click", (event) => stopTool(event.currentTarget));
    el("consoleBtn").addEventListener("click", () => openConsoleDialog(0));

    el("exeStartBtn").addEventListener("click", (event) => {
      if (!state.current) return;
      startTool("exe", [], event.currentTarget, targetMode(state.current.tool, "exe"));
    });
    el("sourceStartBtn").addEventListener("click", (event) => {
      if (!state.current) return;
      startTool("source", [], event.currentTarget, targetMode(state.current.tool, "source"));
    });
    el("sourceConsoleBtn").addEventListener("click", (event) => {
      if (!state.current) return;
      const tool = state.current.tool;
      const mode = targetConsole(tool, "source") ? "hidden" : "console";
      startTool("source", [], event.currentTarget, mode);
    });
    el("exeRevealBtn").addEventListener("click", () => reveal("exe"));
    el("sourceRevealBtn").addEventListener("click", () => reveal("source"));
    el("sourceWorkdirOpenBtn").addEventListener("click", () => reveal("source"));

    el("revealBtn").addEventListener("click", (event) => {
      if (!state.current) return;
      withBusy(event.currentTarget, async () => {
        try {
          await reveal(defaultTarget(state.current.tool));
        } catch (err) {
          reportError(err);
        }
      });
    });

    el("editBtn").addEventListener("click", openEditDialog);

    el("deleteBtn").addEventListener("click", async (event) => {
      if (!state.current) return;
      const name = state.current.tool.name;
      if (!confirm(`确定删除注册项「${name}」？\n仅移除注册与启动记录，不删除 exe 或源码文件。`)) return;
      await withBusy(event.currentTarget, async () => {
        try {
          await request(`/api/tools/${encodeURIComponent(name)}`, { method: "DELETE" });
          toast(`已删除 ${name}`);
          state.current = null;
          detailEl.hidden = true;
          emptyEl.hidden = false;
          await loadTools();
        } catch (err) {
          reportError(err);
        }
      });
    });

    el("addBtn").addEventListener("click", openCreateDialog);
    el("addCancel").addEventListener("click", () => el("addDialog").close());
    el("addForm").addEventListener("submit", submitAdd);
    el("addExeEnabled").addEventListener("change", updateAddSections);
    el("addSourceEnabled").addEventListener("change", updateAddSections);
    el("browseExeBtn").addEventListener("click", () => openBrowse("exe"));
    el("browseProgramBtn").addEventListener("click", () => openBrowse("program"));
    el("browseWorkdirBtn").addEventListener("click", () => openBrowse("workdir"));

    el("browseUp").addEventListener("click", () => {
      if (!browseDir) return;
      const parts = browseDir.replace(/[\\/]+$/, "").split(/[\\/]/);
      parts.pop();
      const parent = parts.join("\\");
      loadBrowse(parent.length <= 2 ? "" : parent);
    });
    el("browseSelectDir").addEventListener("click", selectBrowseDir);
    el("browseCancel").addEventListener("click", () => el("browseDialog").close());

    el("metaBtn").addEventListener("click", openMetaDialog);
    el("metaCancel").addEventListener("click", () => el("metaDialog").close());
    el("metaForm").addEventListener("submit", submitMeta);

    el("consoleShowBtn").addEventListener("click", (event) => {
      consoleAction(state.consolePID, true, event.currentTarget);
    });
    el("consoleHideBtn").addEventListener("click", (event) => {
      consoleAction(state.consolePID, false, event.currentTarget);
    });
    el("consoleCancel").addEventListener("click", () => el("consoleDialog").close());

    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        document.querySelectorAll("dialog[open]").forEach((dialog) => dialog.close());
      }
    });
  }

  window.addEventListener("DOMContentLoaded", async () => {
    bind();
    await loadPreferences();
    await loadMeta();
    await loadTools();
    setInterval(() => {
      loadTools();
      loadMeta();
    }, 5000);
  });
})();
