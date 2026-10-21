// clictl WebUI：工具注册、启动、停止与启动记录查看。
// 与 CLI 共用同一套 /api 契约（{ok, data} / {ok, error}）。
(function () {
  "use strict";

  const state = {
    tools: [],
    running: new Map(), // name -> pids
    current: null,
    filter: "all",
    query: "",
    meta: null,
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

  function renderList() {
    const items = filteredTools();
    countEl.textContent = `${items.length} 个`;
    if (items.length === 0) {
      toolListEl.innerHTML = `<div class="empty-hint">${state.tools.length === 0 ? "尚未注册任何工具" : "没有匹配的工具"}</div>`;
      return;
    }
    toolListEl.innerHTML = items
      .map((tool) => {
        const running = state.running.has(tool.name);
        const active = state.current && state.current.tool.name === tool.name ? "active" : "";
        const invalid = tool.status === "invalid" ? "invalid" : "";
        return `
          <div class="tool-card ${active} ${invalid}" data-name="${escapeHtml(tool.name)}">
            <div class="card-top">
              ${running ? '<span class="pulse" title="后台运行中"></span>' : ""}
              <span class="card-name">${escapeHtml(tool.name)}</span>
              ${tool.status === "invalid" ? '<span class="tag-invalid">失效</span>' : ""}
            </div>
            <div class="card-path" title="${escapeHtml(tool.path)}">${escapeHtml(tool.path)}</div>
            <div class="card-foot">
              <span>${tool.launch_count || 0} 次启动</span>
              <span>${tool.last_launch ? formatTime(tool.last_launch) : "从未启动"}</span>
            </div>
          </div>`;
      })
      .join("");

    toolListEl.querySelectorAll(".tool-card").forEach((card) => {
      card.addEventListener("click", () => {
        selectTool(card.dataset.name).catch(reportError);
      });
    });
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
    el("detailName").textContent = tool.name;
    el("detailDesc").textContent = tool.description || "未填写描述";
    el("detailStatus").textContent = tool.status === "active" ? (running ? "运行中" : "就绪") : "文件失效";
    const dot = el("detailStatusDot");
    dot.className = `status-dot ${tool.status === "active" ? "ok" : "bad"}`;

    el("statLaunches").textContent = String(tool.launch_count || 0);
    el("statDuration").textContent = formatDuration(info.total_duration_ms);
    el("statSize").textContent = formatBytes(tool.size_bytes);
    el("statPids").textContent = running && info.running.pids.length ? info.running.pids.join(", ") : "无";
    el("detailPath").textContent = tool.path;

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

  async function startTool(args, button) {
    if (!state.current) return;
    const name = state.current.tool.name;
    await withBusy(button, async () => {
      try {
        const res = await post(`/api/tools/${encodeURIComponent(name)}/start`, { args: args || [] });
        if (res.already_running) {
          toast(`${name} 已有实例在运行（PID ${res.pid}）`);
        } else {
          toast(`已启动 ${name}（PID ${res.pid}）`);
        }
        await loadTools();
      } catch (err) {
        reportError(err);
      }
    });
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

    el("startBtn").addEventListener("click", (event) => startTool([], event.currentTarget));
    el("startArgsBtn").addEventListener("click", (event) =>
      startTool(parseArgs(el("startArgs").value), event.currentTarget)
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
    await loadMeta();
    await loadTools();
    setInterval(() => {
      loadTools();
      loadMeta();
    }, 5000);
  });
})();
