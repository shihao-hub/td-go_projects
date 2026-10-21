// agyquota 内嵌 WebUI：默认只展示上次查询缓存，用户点击「查询」才发起真实请求。
// 纯原生 DOM，无外部依赖；所有接口数据经 textContent 写入，避免 HTML 注入。

const state = {
  source: localStorage.getItem("agyquota.source") || "agy",
  meta: null,
  loading: false,
  autoTimer: null,
  fetchedAt: null,
};

const $ = (id) => document.getElementById(id);
const AUTO_REFRESH_KEY = "agyquota.autoRefresh";
const SOURCE_KEY = "agyquota.source";
const AUTO_REFRESH_MS = 60_000;
const STALE_MS = 30 * 60_000;

const content = $("content");
const statusDot = $("statusDot");
const statusText = $("statusText");
const staleHint = $("fetchedAt");
const lastTime = $("lastTime");
const lastAgo = $("lastAgo");
const refreshBtn = $("refreshBtn");
const rawBtn = $("rawBtn");
const rawDialog = $("rawDialog");
const rawBody = $("rawBody");
const rawTitle = $("rawTitle");

// ---------- 基础工具 ----------

function setStatus(kind, text) {
  statusDot.className = "dot" + (kind ? " " + kind : "");
  statusText.textContent = text;
}

function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
}

function make(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function pad(n) {
  return String(n).padStart(2, "0");
}

function formatClock(value) {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function formatShort(value) {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function ageMs(value) {
  if (!value) return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return null;
  return Math.max(0, Date.now() - d.getTime());
}

function ago(value) {
  const ms = ageMs(value);
  if (ms === null) return "";
  const sec = Math.round(ms / 1000);
  if (sec < 60) return `${sec} 秒前`;
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟前`;
  if (sec < 86_400) return `${Math.floor(sec / 3600)} 小时前`;
  return `${Math.floor(sec / 86_400)} 天前`;
}

function percentClass(pct) {
  if (pct < 20) return "bad";
  if (pct < 50) return "warn";
  return "";
}

function sourceLabel(source) {
  return source === "zed" ? "Zed 插件" : "Antigravity CLI";
}

// ---------- 上次查询时间（页首醒目位置） ----------

function updateLastQuery(value) {
  state.fetchedAt = value || null;
  if (!value) {
    lastTime.textContent = "尚未查询";
    lastTime.classList.add("is-empty");
    lastAgo.textContent = "";
    staleHint.textContent = "";
    return;
  }
  lastTime.textContent = formatShort(value);
  lastTime.classList.remove("is-empty");
  const ms = ageMs(value) ?? 0;
  lastAgo.textContent = ago(value);
  if (ms > STALE_MS) {
    lastAgo.textContent += " · 已过期";
    lastAgo.classList.add("is-stale");
    staleHint.textContent = "缓存已超过 30 分钟，建议重新查询";
  } else {
    lastAgo.classList.remove("is-stale");
    staleHint.textContent = "";
  }
}

// ---------- 状态渲染 ----------

function renderSkeleton() {
  clear(content);
  const box = make("div", "skeleton");
  box.setAttribute("aria-hidden", "true");
  box.append(make("div", "skeleton-line w40"), make("div", "skeleton-line"), make("div", "skeleton-line w80"));
  content.append(box);
}

function renderError(error) {
  clear(content);
  const card = make("div", "error-card");
  card.append(make("span", "error-code", error.code || "internal"));
  card.append(make("h2", "", "配额查询失败"));
  card.append(make("p", "", error.message || "未知错误"));
  if (Array.isArray(error.suggestions) && error.suggestions.length) {
    const list = make("ul");
    for (const item of error.suggestions) list.append(make("li", "", item));
    card.append(list);
  }
  const actions = make("div", "error-actions");
  const retry = make("button", "btn primary", "重试");
  retry.type = "button";
  retry.addEventListener("click", query);
  actions.append(retry);
  card.append(actions);
  content.append(card);
  setStatus("error", "查询失败");
}

function renderNeverQueried() {
  clear(content);
  updateLastQuery(null);
  renderMeta(null);
  const box = make("div", "empty");
  box.append(make("h2", "", "尚未查询"));
  box.append(make("p", "", `还没有 ${sourceLabel(state.source)} 的配额记录。`));
  box.append(make("p", "hint", "桌面面板启动时不会自动联网；点击下方按钮或右上角「查询」开始。"));
  const actions = make("div", "empty-actions");
  const start = make("button", "btn primary", "查询配额");
  start.type = "button";
  start.addEventListener("click", query);
  actions.append(start);
  box.append(actions);
  content.append(box);
  setStatus("", "等待查询");
}

function renderEmpty(snapshot, cached) {
  clear(content);
  const box = make("div", "empty");
  box.append(make("h2", "", "未解析到配额分组"));
  box.append(make("p", "", "接口响应中没有可展示的模型桶。"));
  box.append(make("p", "hint", "响应结构可能已变化，可点击「原始响应」查看底层数据。"));
  content.append(box);
  renderMeta(snapshot);
  updateLastQuery(snapshot.fetchedAt);
  setStatus(cached ? "ok" : "ok", cached ? "展示上次缓存" : "查询完成，但未解析到模型桶");
}

function renderBucket(bucket) {
  const card = make("article", "bucket");
  const head = make("div", "bucket-head");
  head.append(make("h2", "", bucket.name || "未命名分组"));
  const windows = Array.isArray(bucket.windows) ? bucket.windows : [];
  const count = windows.length;
  head.append(make("span", "bucket-note", `${count} window${count === 1 ? "" : "s"}`));
  card.append(head);

  const list = make("div", "windows");
  for (const win of windows) {
    const row = make("div", "window-row");
    const rawPct = Number(win.percent);
    const pct = Number.isFinite(rawPct) ? Math.max(0, Math.min(100, rawPct)) : 0;
    const tone = percentClass(pct);

    row.append(make("span", "window-label", win.label || win.window || "quota"));

    const meter = make("div", "meter");
    const fill = make("div", "meter-fill" + (tone ? " " + tone : ""));
    fill.style.width = `${pct}%`;
    meter.append(fill, make("div", "meter-ticks"));
    row.append(meter);

    const pctText = Number.isFinite(rawPct) ? pct.toFixed(pct % 1 === 0 ? 0 : 1) : "—";
    row.append(make("span", "window-pct" + (tone ? " " + tone : ""), `${pctText}%`));

    const reset = win.resetsIn || (win.resetAt ? formatClock(win.resetAt) : "重置时间未知");
    row.append(make("span", "window-reset", reset));
    list.append(row);
  }
  card.append(list);
  return card;
}

function flattenWindows(snapshot) {
  // 后端契约是 bucket.models[].windows[]，同时兼容 bucket.windows 的扁平形态。
  const out = [];
  for (const bucket of snapshot.buckets || []) {
    if (Array.isArray(bucket.windows)) {
      out.push({ name: bucket.name, windows: bucket.windows });
      continue;
    }
    const windows = [];
    for (const model of bucket.models || []) {
      for (const win of model.windows || []) windows.push(win);
    }
    out.push({ name: bucket.name, windows });
  }
  return out;
}

function renderSnapshot(snapshot, cached) {
  const buckets = flattenWindows(snapshot);
  if (!buckets.length) {
    renderEmpty(snapshot, cached);
    return;
  }

  clear(content);
  for (const bucket of buckets) content.append(renderBucket(bucket));
  renderMeta(snapshot);
  updateLastQuery(snapshot.fetchedAt);
  const source = snapshot.source || state.source;
  if (cached) {
    setStatus("ok", `展示上次缓存 · ${source}`);
  } else {
    setStatus("ok", `查询完成 · ${source}`);
  }
}

function renderMeta(snapshot) {
  const metaRow = $("metaRow");
  metaRow.hidden = false;
  $("metaSource").textContent = snapshot?.source || sourceLabel(state.source);
  $("metaDataDir").textContent = state.meta?.dataDir || "—";

  const accountItem = $("accountItem");
  if (snapshot?.account) {
    accountItem.hidden = false;
    $("metaAccount").textContent = snapshot.account;
  } else {
    accountItem.hidden = true;
  }

  const tokenItem = $("tokenItem");
  if (snapshot?.tokenExpiresAt) {
    tokenItem.hidden = false;
    const suffix = snapshot.tokenExpiresIn ? `（${snapshot.tokenExpiresIn}）` : "";
    $("metaToken").textContent = `${snapshot.tokenExpiresAt}${suffix}`;
  } else {
    tokenItem.hidden = true;
  }
}

// ---------- 网络 ----------

async function fetchEnvelope(path, signal) {
  const resp = await fetch(path, { signal, cache: "no-store" });
  if (!resp.ok) {
    return { ok: false, error: { code: `http_${resp.status}`, message: `接口返回 HTTP ${resp.status}` } };
  }
  return resp.json();
}

async function fetchCached(source) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5_000);
  try {
    const env = await fetchEnvelope(`/api/cache?source=${encodeURIComponent(source)}`, controller.signal);
    return env.ok && env.data ? env.data : null;
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}

function setLoading(loading) {
  state.loading = loading;
  refreshBtn.disabled = loading;
  rawBtn.disabled = loading;
  if (loading) setStatus("loading", `正在通过 ${sourceLabel(state.source)} 查询…`);
}

async function query() {
  if (state.loading) return;
  setLoading(true);
  renderSkeleton();
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 120_000);
  try {
    const env = await fetchEnvelope(`/api/quota?source=${encodeURIComponent(state.source)}`, controller.signal);
    if (!env.ok) {
      renderError(env.error || { code: "internal", message: "响应包络缺少 error 字段" });
      return;
    }
    renderSnapshot(env.data, false);
  } catch (err) {
    const aborted = err && err.name === "AbortError";
    renderError({
      code: aborted ? "timeout" : "network_error",
      message: aborted ? "查询超时（120 秒），请检查网络或稍后重试" : `无法连接本地 WebUI 服务：${err}`,
    });
  } finally {
    clearTimeout(timer);
    setLoading(false);
  }
}

async function loadCached() {
  const snap = await fetchCached(state.source);
  if (snap) {
    renderSnapshot(snap, true);
  } else {
    renderNeverQueried();
  }
}

async function showRaw() {
  if (state.loading) return;
  rawBody.textContent = "正在读取…";
  rawTitle.textContent = `接口原始响应 · ${sourceLabel(state.source)}`;
  if (!rawDialog.open) rawDialog.showModal();
  try {
    const env = await fetchEnvelope(`/api/quota?source=${encodeURIComponent(state.source)}&raw=1`);
    if (!env.ok) {
      rawBody.textContent = `[${env.error?.code || "internal"}] ${env.error?.message || "读取失败"}`;
      return;
    }
    const raw = env.data?.raw ?? env.data;
    rawBody.textContent = typeof raw === "string" ? raw : JSON.stringify(raw, null, 2);
  } catch (err) {
    rawBody.textContent = `读取失败：${err}`;
  }
}

async function loadMeta() {
  try {
    const resp = await fetch("/api/meta", { cache: "no-store" });
    if (!resp.ok) return;
    state.meta = await resp.json();
    const channel = $("channel");
    channel.textContent = state.meta.channel || state.meta.buildMode || "dev";
    channel.classList.add(state.meta.buildMode === "release" ? "release" : "dev");
    $("version").textContent = state.meta.version || "";
    $("metaDataDir").textContent = state.meta.dataDir || "—";
  } catch {
    // 元信息仅用于页脚展示，失败不阻断缓存读取。
  }
}

// ---------- 交互 ----------

function selectSource(next, { load = true } = {}) {
  state.source = next;
  localStorage.setItem(SOURCE_KEY, next);
  document.querySelectorAll("#sourceTabs [data-source]").forEach((tab) => {
    const active = tab.dataset.source === next;
    tab.classList.toggle("active", active);
    tab.setAttribute("aria-selected", String(active));
  });
  if (load) loadCached();
}

function bindSources() {
  document.querySelectorAll("#sourceTabs [data-source]").forEach((tab) => {
    tab.addEventListener("click", () => {
      if (tab.dataset.source === state.source) return;
      selectSource(tab.dataset.source);
    });
  });
}

function applyAutoRefresh(enabled) {
  if (state.autoTimer) {
    clearInterval(state.autoTimer);
    state.autoTimer = null;
  }
  $("autoRefresh").checked = enabled;
  if (enabled) state.autoTimer = setInterval(query, AUTO_REFRESH_MS);
}

function bindControls() {
  refreshBtn.addEventListener("click", query);
  rawBtn.addEventListener("click", showRaw);
  $("rawClose").addEventListener("click", () => rawDialog.close());

  const stored = localStorage.getItem(AUTO_REFRESH_KEY) === "1";
  applyAutoRefresh(stored);
  $("autoRefresh").addEventListener("change", (event) => {
    const enabled = event.target.checked;
    localStorage.setItem(AUTO_REFRESH_KEY, enabled ? "1" : "0");
    applyAutoRefresh(enabled);
  });

  document.addEventListener("keydown", (event) => {
    if (rawDialog.open) {
      if (event.key === "Escape") rawDialog.close();
      return;
    }
    if (event.target instanceof HTMLInputElement) return;
    if (event.key === "r" || event.key === "R") {
      event.preventDefault();
      query();
    } else if (event.key === "1" || event.key === "2") {
      selectSource(event.key === "1" ? "agy" : "zed");
    }
  });
}

setInterval(() => {
  if (state.fetchedAt) updateLastQuery(state.fetchedAt);
}, 15_000);

bindSources();
selectSource(state.source, { load: false });
bindControls();
loadMeta().then(loadCached);
