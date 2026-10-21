const state = {
  apps: [],
  meta: null,
  busy: false,
};

const els = {
  channelBadge: document.getElementById("channelBadge"),
  configPath: document.getElementById("configPath"),
  scriptsDir: document.getElementById("scriptsDir"),
  taskState: document.getElementById("taskState"),
  installTaskBtn: document.getElementById("installTaskBtn"),
  appGrid: document.getElementById("appGrid"),
  logBody: document.getElementById("logBody"),
  refreshBtn: document.getElementById("refreshBtn"),
  clearLogBtn: document.getElementById("clearLogBtn"),
  applyAllBtn: document.getElementById("applyAllBtn"),
  killCheck: document.getElementById("killCheck"),
  explorerCheck: document.getElementById("explorerCheck"),
};

function log(message) {
  const text = String(message ?? "").trim();
  if (!text) {
    return;
  }
  const stamp = new Date().toLocaleTimeString("zh-CN", { hour12: false });
  const current = els.logBody.textContent === "等待操作…" ? "" : els.logBody.textContent;
  els.logBody.textContent = `${current}${current ? "\n" : ""}[${stamp}] ${text}`;
  els.logBody.scrollTop = els.logBody.scrollHeight;
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  const payload = await response.json().catch(() => null);
  if (!response.ok || !payload || payload.ok !== true) {
    const detail = payload && payload.error ? payload.error : { message: `HTTP ${response.status}` };
    const error = new Error(detail.message || "请求失败");
    error.code = detail.code;
    error.output = detail.output;
    throw error;
  }
  return payload.data;
}

function setBusy(busy) {
  state.busy = busy;
  document.body.classList.toggle("busy", busy);
  for (const button of document.querySelectorAll("button")) {
    if (button.id === "clearLogBtn") {
      continue;
    }
    button.disabled = busy;
  }
}

function optionLabel(style) {
  return style;
}

function renderApps() {
  if (!state.apps.length) {
    els.appGrid.innerHTML = '<div class="empty">脚本目录中没有可调度的应用。</div>';
    return;
  }

  els.appGrid.innerHTML = "";
  for (const app of state.apps) {
    const card = document.createElement("article");
    card.className = `app-card${app.enabled ? "" : " disabled"}`;

    const head = document.createElement("div");
    head.className = "card-head";

    const titleBox = document.createElement("div");
    const name = document.createElement("div");
    name.className = "app-name";
    name.textContent = app.name;
    const script = document.createElement("div");
    script.className = "app-script";
    script.title = app.script;
    script.textContent = app.script || "（无补丁脚本）";
    titleBox.append(name, script);

    const toggle = document.createElement("label");
    toggle.className = "switch";
    const toggleInput = document.createElement("input");
    toggleInput.type = "checkbox";
    toggleInput.checked = Boolean(app.enabled);
    toggleInput.addEventListener("change", () => updateEnabled(app, toggleInput));
    const toggleText = document.createElement("span");
    toggleText.textContent = app.enabled ? "已启用" : "已停用";
    toggle.append(toggleInput, toggleText);

    head.append(titleBox, toggle);

    const styleRow = document.createElement("div");
    styleRow.className = "style-row";
    const styleLabel = document.createElement("label");
    styleLabel.textContent = "款式";
    const select = document.createElement("select");
    select.disabled = state.busy || (app.styles.length === 0 && !app.custom_icon);
    if (app.custom_icon) {
      const customOption = document.createElement("option");
      customOption.value = "__custom__";
      customOption.textContent = `自定义：${app.custom_icon.split(/[\\/]/).pop()}`;
      select.append(customOption);
    }
    for (const style of app.styles) {
      const option = document.createElement("option");
      option.value = style;
      option.textContent = optionLabel(style);
      select.append(option);
    }
    if (!app.styles.length && !app.custom_icon) {
      const option = document.createElement("option");
      option.textContent = "（无预设款式）";
      select.append(option);
    }
    select.value = app.custom_icon ? "__custom__" : app.style || app.styles[0] || "";
    select.addEventListener("change", () => updateStyle(app, select));
    styleRow.append(styleLabel, select);

    const actions = document.createElement("div");
    actions.className = "card-actions";
    const applyBtn = document.createElement("button");
    applyBtn.className = "btn btn-primary btn-small";
    applyBtn.type = "button";
    applyBtn.textContent = "应用";
    applyBtn.disabled = state.busy || !app.enabled;
    applyBtn.addEventListener("click", () => applyOne(app));

    const revertBtn = document.createElement("button");
    revertBtn.className = "btn btn-quiet btn-small btn-danger";
    revertBtn.type = "button";
    revertBtn.textContent = "恢复官方";
    revertBtn.disabled = state.busy;
    revertBtn.addEventListener("click", () => revertOne(app));

    actions.append(applyBtn, revertBtn);
    card.append(head, styleRow, actions);
    els.appGrid.append(card);
  }
}

async function updateEnabled(app, input) {
  setBusy(true);
  try {
    await api("/api/config", {
      method: "POST",
      body: JSON.stringify({ app: app.name, enabled: input.checked }),
    });
    app.enabled = input.checked;
    log(`${app.name}: ${input.checked ? "已启用" : "已停用"}`);
    await loadApps();
  } catch (error) {
    input.checked = !input.checked;
    log(`更新启用状态失败：${error.message}`);
  } finally {
    setBusy(false);
    renderApps();
  }
}

async function updateStyle(app, select) {
  if (select.value === "__custom__") {
    return;
  }
  setBusy(true);
  try {
    await api("/api/config", {
      method: "POST",
      body: JSON.stringify({ app: app.name, style: select.value }),
    });
    app.style = select.value;
    app.custom_icon = "";
    log(`${app.name}: 款式切换为 ${select.value}`);
  } catch (error) {
    log(`保存款式失败：${error.message}`);
  } finally {
    setBusy(false);
    await loadApps();
  }
}

async function applyOne(app) {
  setBusy(true);
  log(`正在应用 ${app.name}…`);
  try {
    const data = await api("/api/apply", {
      method: "POST",
      body: JSON.stringify({
        app: app.name,
        kill: els.killCheck.checked,
        restart_explorer: els.explorerCheck.checked,
      }),
    });
    if (data.output) {
      log(data.output);
    }
    log(`${app.name} 应用完成`);
  } catch (error) {
    if (error.output) {
      log(error.output);
    }
    log(`${app.name} 应用失败：${error.message}`);
  } finally {
    setBusy(false);
    await refreshStatus();
  }
}

async function revertOne(app) {
  setBusy(true);
  log(`正在恢复 ${app.name} 的官方图标…`);
  try {
    const data = await api("/api/revert", {
      method: "POST",
      body: JSON.stringify({ app: app.name, restart_explorer: els.explorerCheck.checked }),
    });
    if (data.output) {
      log(data.output);
    }
    log(`${app.name} 已恢复官方图标`);
  } catch (error) {
    if (error.output) {
      log(error.output);
    }
    log(`${app.name} 恢复失败：${error.message}`);
  } finally {
    setBusy(false);
  }
}

async function applyAll() {
  setBusy(true);
  log("正在批量应用全部已启用应用…");
  try {
    const data = await api("/api/apply", {
      method: "POST",
      body: JSON.stringify({
        kill: els.killCheck.checked,
        restart_explorer: els.explorerCheck.checked,
      }),
    });
    if (data.output) {
      log(data.output);
    }
    const applied = data.applied || [];
    log(`批量完成：${applied.length ? applied.join("、") : "没有已启用应用"}`);
  } catch (error) {
    if (error.output) {
      log(error.output);
    }
    log(`批量应用失败：${error.message}`);
  } finally {
    setBusy(false);
    await refreshStatus();
  }
}

async function installTask() {
  setBusy(true);
  log("正在注册登录守护计划任务…");
  try {
    const data = await api("/api/task", { method: "POST", body: JSON.stringify({ install: true }) });
    if (data.output) {
      log(data.output);
    }
    log(`计划任务已注册：${data.task}`);
  } catch (error) {
    if (error.output) {
      log(error.output);
    }
    log(`注册计划任务失败：${error.message}`);
  } finally {
    setBusy(false);
    await refreshStatus();
  }
}

async function loadMeta() {
  const meta = await api("/api/meta");
  state.meta = meta;
  els.channelBadge.textContent = `${meta.channel} · ${meta.version}`;
  els.configPath.textContent = meta.configPath || "（未解析）";
  els.configPath.title = meta.configPath || "";
  els.scriptsDir.textContent = meta.scriptsDir || "（未解析）";
  els.scriptsDir.title = meta.scriptsDir || "";
  els.taskState.textContent = meta.taskInstalled ? "已注册" : "未注册";
  els.taskState.className = `task-state ${meta.taskInstalled ? "installed" : "missing"}`;
  els.installTaskBtn.textContent = meta.taskInstalled ? "重新注册" : "注册计划任务";
  els.installTaskBtn.title = meta.cliPath ? `将调用 ${meta.cliPath}` : "";
}

async function loadApps() {
  state.apps = await api("/api/apps");
  renderApps();
}

async function refreshStatus() {
  await Promise.all([loadMeta(), loadApps()]);
}

async function boot() {
  try {
    await refreshStatus();
    log(`已连接 ${state.meta.channel} 通道（${state.meta.version}）`);
  } catch (error) {
    log(`初始化失败：${error.message}`);
    els.appGrid.innerHTML = `<div class="empty">无法连接本地服务：${error.message}</div>`;
  }
}

els.refreshBtn.addEventListener("click", async () => {
  setBusy(true);
  try {
    await refreshStatus();
    log("已刷新");
  } catch (error) {
    log(`刷新失败：${error.message}`);
  } finally {
    setBusy(false);
  }
});

els.applyAllBtn.addEventListener("click", applyAll);
els.installTaskBtn.addEventListener("click", installTask);
els.clearLogBtn.addEventListener("click", () => {
  els.logBody.textContent = "等待操作…";
});

boot();
