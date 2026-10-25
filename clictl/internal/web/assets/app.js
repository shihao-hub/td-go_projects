// clictl WebUI：工具注册、启动、停止与启动记录查看。
// 与 CLI 共用同一套 /api 契约（{ok, data} / {ok, error}）。
(function () {
  "use strict";

  const UNGROUPED = "未分组";
  const SORT_MODES = new Set(["name", "recent", "launches", "added"]);

  const state = {
    tools: [],
    running: new Map(), // name -> pids
    current: null,
    filter: "all",
    query: "",
    meta: null,
    viewMode: "flat",
    sortMode: "name",
    activeGroup: "",
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

  // ---------- 视图偏好 ----------

  let prefsTimer = null;

  async function loadPreferences() {
    try {
      const prefs = await request("/api/preferences");
      if (prefs) {
        const mode = prefs.view_mode === "tabs" || prefs.view_mode === "tree" ? "tabs" : "flat";
        state.viewMode = mode;
        state.sortMode = SORT_MODES.has(prefs.sort_mode) ? prefs.sort_mode : "name";
        state.activeGroup = typeof prefs.active_group === "string" ? prefs.active_group.trim() : "";
      }
    } catch (err) {
      console.error(err);
    }
    updateViewControls();
  }

  // 连续切换视图/标签时合并写盘，避免每次都落盘一份 JSON。
  function savePreferences() {
    clearTimeout(prefsTimer);
    prefsTimer = setTimeout(async () => {
      try {
        await post("/api/preferences", {
          view_mode: state.viewMode,
          sort_mode: state.sortMode,
          active_group: state.activeGroup,
        });
      } catch (err) {
        console.error(err);
      }
    }, 250);
  }

  function updateViewControls() {
    el("viewToggle")
      .querySelectorAll(".view-btn")
      .forEach((btn) => btn.classList.toggle("active", btn.dataset.view === state.viewMode));
    el("sortSelect").value = state.sortMode;
  }

  // ---------- 分组与排序 ----------

  function groupOf(tool) {
    const group = tool.meta && typeof tool.meta.group === "string" ? tool.meta.group.trim() : "";
    return group || UNGROUPED;
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
      // 首次加载自动展开第一个工具，避免打开就是全空详情区。
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
      return (
        tool.name.toLowerCase().includes(q) ||
        (tool.path || "").toLowerCase().includes(q) ||
        (tool.description || "").toLowerCase().includes(q)
      );
    });
  }

  function renderCard(tool) {
    const running = state.running.has(tool.name);
    const active = state.current && state.current.tool.name === tool.name ? "active" : "";
    const invalid = tool.status === "invalid" ? "invalid" : "";
    const group = groupOf(tool);
    const groupTag =
      group !== UNGROUPED ? `<span class="card-group" title="分组">${escapeHtml(group)}</span>` : "";
    return `
      <div class="tool-card ${active} ${invalid}" data-name="${escapeHtml(tool.name)}">
        <div class="card-top">
          ${running ? '<span class="pulse" title="后台运行中"></span>' : ""}
          <span class="card-name">${escapeHtml(tool.name)}</span>
          ${groupTag}
          ${tool.status === "invalid" ? '<span class="tag-invalid">失效</span>' : ""}
          <button type="button" class="card-group-btn" data-group-for="${escapeHtml(tool.name)}" title="设置分组">分组</button>
        </div>
        <div class="card-path" title="${escapeHtml(tool.path)}">${escapeHtml(tool.path)}</div>
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
    // 卡片上的分组快捷入口：先选中再弹分组窗，不改变列表滚动位置。
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
    toolListEl.classList.toggle("grouped", state.viewMode === "tabs");
    if (state.viewMode === "tabs") {
      renderTabs(items);
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

  // 分组 tab：横向标签页切换，替代垂直折叠树，工具多时不再需要往下滚。
  function renderTabs(items) {
    const groups = existingGroups();
    if (state.activeGroup && groups.indexOf(state.activeGroup) === -1) {
      state.activeGroup = "";
      savePreferences();
    }

    const counts = new Map();
    const runningGroups = new Set();
    for (const tool of items) {
      const name = groupOf(tool);
      counts.set(name, (counts.get(name) || 0) + 1);
      if (state.running.has(tool.name)) runningGroups.add(name);
    }

    const tabs = [{ name: "", label: "全部", count: items.length, running: runningGroups.has(UNGROUPED) }];
    if (groups.length > 0) {
      for (const name of groups) {
        tabs.push({ name, label: name, count: counts.get(name) || 0, running: runningGroups.has(name) });
      }
      tabs.push({
        name: UNGROUPED,
        label: UNGROUPED,
        count: counts.get(UNGROUPED) || 0,
        running: runningGroups.has(UNGROUPED),
      });
    }

    const tabsEl = el("groupTabs");
    tabsEl.hidden = false;
    el("groupHint").hidden = groups.length > 0;
    tabsEl.innerHTML = tabs
      .map(
        (tab) => `
        <button type="button" class="group-tab ${tab.name === state.activeGroup ? "active" : ""}" data-group="${escapeHtml(tab.name)}" role="tab" aria-selected="${tab.name === state.activeGroup}">
          ${tab.running ? '<span class="tab-pulse" title="该组有后台实例"></span>' : ""}
          <span class="tab-label">${escapeHtml(tab.label)}</span>
          <span class="tab-count">${tab.count}</span>
        </button>`
      )
      .join("");

    const visible =
      state.activeGroup === "" ? items : items.filter((tool) => groupOf(tool) === state.activeGroup);
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
    const running = info.running && info.running.alive;
    const isConsole = !!info.console;
    el("detailName").textContent = tool.name;
    el("detailType").textContent = isConsole ? "命令行" : "窗口程序";
    el("detailType").className = `type-badge ${isConsole ? "console" : "gui"}`;
    el("startBtn").textContent = isConsole ? "终端启动" : "启动";
    el("startBtn").title = isConsole ? "在独立控制台窗口启动" : "后台分离启动";
    el("startAltBtn").hidden = !isConsole;
    el("detailDesc").textContent = tool.description || "未填写描述";
    el("detailStatus").textContent = tool.status === "active" ? (running ? "运行中" : "就绪") : "文件失效";
    const dot = el("detailStatusDot");
    dot.className = `status-dot ${tool.status === "active" ? "ok" : "bad"}`;

    el("statLaunches").textContent = String(tool.launch_count || 0);
    el("statDuration").textContent = formatDuration(info.total_duration_ms);
    el("statSize").textContent = formatBytes(tool.size_bytes);
    el("statPids").textContent = running && info.running.pids.length ? info.running.pids.join(", ") : "无";
    el("detailPath").textContent = tool.path;

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

    const rows = info.recent_launches || [];
    el("launchBody").innerHTML = rows.length
      ? rows
          .map((launch) => {
            const duration = launch.duration_ms == null ? "未闭环" : formatDuration(launch.duration_ms);
            const code = launch.exit_code == null ? "--" : String(launch.exit_code);
            const pid = launch.pid == null ? "--" : String(launch.pid);
            return `<tr><td>${formatTime(launch.started_at)}</td><td>${duration}</td><td>${code}</td><td>${pid}</td></tr>`;
          })
          .join("")
      : '<tr><td colspan="4" class="muted">暂无启动记录</td></tr>';

    el("stopBtn").disabled = !running;
    el("runRow").hidden = tool.status !== "active";
  }

  // ---------- 动作 ----------

  async function startTool(args, button, mode) {
    if (!state.current) return;
    const name = state.current.tool.name;
    await withBusy(button, async () => {
      try {
        const payload = { args: args || [] };
        if (mode) payload.mode = mode;
        const res = await post(`/api/tools/${encodeURIComponent(name)}/start`, payload);
        const where = res.mode === "console" ? "终端窗口" : "后台";
        if (res.already_running) {
          toast(`${name} 已在${where}启动（PID ${res.pid}），已有实例在运行`);
        } else {
          toast(`已在${where}启动 ${name}（PID ${res.pid}）`);
        }
        await loadTools();
      } catch (err) {
        reportError(err);
      }
    });
  }

  // 主启动模式：命令行工具开控制台窗口，窗口程序后台分离。
  function primaryMode() {
    return state.current && state.current.console ? "console" : "detached";
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

  function existingGroups() {
    const groups = new Set();
    for (const tool of state.tools) {
      const group = groupOf(tool);
      if (group !== UNGROUPED) groups.add(group);
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
        await request(`/api/tools/${encodeURIComponent(name)}/meta`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ meta }),
        });
        el("groupDialog").close();
        toast(group ? `已归入分组「${group}」` : "已移出分组");
        await selectTool(name);
        await loadTools();
      } catch (err) {
        reportError(err);
      }
    });
  }

  // 解析启动参数：支持单双引号包裹，避免空格被拆散。
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

  // ---------- 注册 / 浏览 ----------

  let browseDir = "";

  async function openBrowse() {
    el("browseDialog").showModal();
    await loadBrowse(browseDir || "");
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
          el("addPath").value = item.dataset.file;
          if (!el("addName").value) {
            const base = item.dataset.file.split("\\").pop().replace(/\.exe$/i, "");
            el("addName").value = base.toLowerCase();
          }
          el("browseDialog").close();
        });
      });
    } catch (err) {
      reportError(err);
    }
  }

  async function submitAdd(event) {
    event.preventDefault();
    const path = el("addPath").value.trim();
    if (!path) return;

    let meta = null;
    const metaText = el("addMeta").value.trim();
    if (metaText) {
      try {
        meta = JSON.parse(metaText);
      } catch (err) {
        toast("meta 不是合法 JSON", true);
        return;
      }
    }

    const group = el("addGroup").value.trim();
    if (group) {
      if (meta && typeof meta === "object" && !Array.isArray(meta)) {
        meta.group = group;
      } else {
        meta = { group };
      }
    }

    try {
      const tool = await post("/api/tools", {
        path,
        name: el("addName").value.trim(),
        desc: el("addDesc").value.trim(),
        meta,
      });
      el("addDialog").close();
      el("addForm").reset();
      toast(`已注册 ${tool.name}`);
      await loadTools();
      await selectTool(tool.name);
    } catch (err) {
      reportError(err);
    }
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
      await request(`/api/tools/${encodeURIComponent(state.current.tool.name)}/meta`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ meta }),
      });
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
      state.viewMode = btn.dataset.view === "tabs" ? "tabs" : "flat";
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
      // 切到具体分组时，若右侧详情还是别的组，就顺势选中该组第一个工具。
      const outside = next !== "" && (!state.current || groupOf(state.current.tool) !== next);
      const first = outside
        ? sortTools(filteredTools()).find((tool) => groupOf(tool) === next)
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

    el("startBtn").addEventListener("click", (event) => startTool([], event.currentTarget, primaryMode()));
    el("startAltBtn").addEventListener("click", (event) => startTool([], event.currentTarget, "detached"));
    el("startArgsBtn").addEventListener("click", (event) =>
      startTool(parseArgs(el("startArgs").value), event.currentTarget, primaryMode())
    );
    el("stopBtn").addEventListener("click", (event) => stopTool(event.currentTarget));

    el("revealBtn").addEventListener("click", async (event) => {
      if (!state.current) return;
      await withBusy(event.currentTarget, async () => {
        try {
          await post(`/api/tools/${encodeURIComponent(state.current.tool.name)}/reveal`);
        } catch (err) {
          reportError(err);
        }
      });
    });

    el("deleteBtn").addEventListener("click", async (event) => {
      if (!state.current) return;
      const name = state.current.tool.name;
      if (!confirm(`确定删除注册项「${name}」？\n仅移除注册与启动记录，不删除 exe 文件。`)) return;
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

    el("addBtn").addEventListener("click", () => {
      el("addForm").reset();
      renderGroupOptions();
      el("addDialog").showModal();
    });
    el("addCancel").addEventListener("click", () => el("addDialog").close());
    el("addForm").addEventListener("submit", submitAdd);

    el("browseBtn").addEventListener("click", openBrowse);
    el("browseUp").addEventListener("click", () => {
      if (!browseDir) return;
      const parts = browseDir.replace(/[\\/]+$/, "").split(/[\\/]/);
      parts.pop();
      const parent = parts.join("\\");
      loadBrowse(parent.length <= 2 ? "" : parent);
    });
    el("browseCancel").addEventListener("click", () => el("browseDialog").close());

    el("metaBtn").addEventListener("click", openMetaDialog);
    el("metaCancel").addEventListener("click", () => el("metaDialog").close());
    el("metaForm").addEventListener("submit", submitMeta);

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
