"use strict";

// ============================================================
// liteconf 控制台
// 单文件原生 JS：只做渲染、交互与 /api/* 调用，无任何业务实现
// 分节：工具 → API 包装 → 横幅 → 路由 → 列表 → 编辑器（text/table）→ 新建
// ============================================================

// ------------------------------------------------------------
// 工具与常量
// ------------------------------------------------------------

// 合法 app/env 名称：与服务端校验规则一致，仅用于前端预校验（不替代服务端）
const NAME_RE = /^[a-zA-Z0-9_-]+$/;

// 普通对象（非 null、非数组）
function isPlainObject(v) {
  return v !== null && typeof v === "object" && !Array.isArray(v);
}

// 原始值：null / number / boolean / string
function isPrimitive(v) {
  return v === null || typeof v !== "object";
}

// ------------------------------------------------------------
// DOM 引用
// ------------------------------------------------------------

const $ = (id) => document.getElementById(id);

const errorBanner = $("error-banner");
const listView = $("list-view");
const editorView = $("editor-view");
const editorTitle = $("editor-title");
const editorVersion = $("editor-version");
const notFoundBox = $("not-found");
const editorBody = $("editor-body");
const btnModeText = $("btn-mode-text");
const btnModeTable = $("btn-mode-table");
const textPane = $("text-pane");
const tablePane = $("table-pane");
const jsonText = $("json-text");
const editorStatus = $("editor-status");
const tableStatus = $("table-status");
const tableBody = $("table-body");
const newDialog = $("new-dialog");
const newError = $("new-error");
const newApp = $("new-app");
const newEnv = $("new-env");
const newJson = $("new-json");
const btnNewSubmit = $("btn-new-submit");

// ------------------------------------------------------------
// 编辑器状态：text（JSON 文本）是唯一权威缓冲
// ------------------------------------------------------------

const state = {
  app: null,
  env: null,
  version: null,
  mode: "text", // "text" | "table"
  text: "",     // 唯一权威 JSON 文本缓冲；表格视图由它派生
};

// 键值表行元数据（与 table-body 内 input 顺序一一对应）
let tableRows = [];

// ------------------------------------------------------------
// API 包装：统一包络 {code,message,data} → 成功返回 data，失败 reject 错误对象
// ------------------------------------------------------------

async function api(method, path, body) {
  let resp;
  try {
    const opts = { method, headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = body;
    }
    resp = await fetch("/api" + path, opts);
  } catch (e) {
    throw { status: 0, code: "network", message: "网络请求失败：" + (e && e.message ? e.message : e) };
  }
  let payload;
  try {
    payload = await resp.json();
  } catch (e) {
    throw { status: resp.status, code: "bad_response", message: "服务端返回非 JSON 响应（HTTP " + resp.status + "）" };
  }
  if (payload.code !== "ok") {
    throw { status: resp.status, code: payload.code, message: payload.message || payload.code };
  }
  return payload.data;
}

// ------------------------------------------------------------
// 全局错误横幅
// ------------------------------------------------------------

function showError(msg) {
  errorBanner.textContent = msg;
  errorBanner.classList.remove("hidden");
}

function clearError() {
  errorBanner.textContent = "";
  errorBanner.classList.add("hidden");
}

// ------------------------------------------------------------
// hash 路由：#/ 为列表，#/{app}/{env} 为编辑器
// ------------------------------------------------------------

// 解析 hash；不合法（段数不对或名称非法）一律按列表处理，不向服务端发请求
function parseHash() {
  const h = location.hash.replace(/^#/, "");
  if (!h || h === "/") return null;
  const parts = h.replace(/^\//, "").split("/");
  if (parts.length !== 2 || !parts.every((p) => NAME_RE.test(p))) return null;
  return { app: parts[0], env: parts[1] };
}

function onRouteChange() {
  const target = parseHash();
  if (target) {
    showEditor(target.app, target.env);
  } else {
    showList();
  }
}

window.addEventListener("hashchange", onRouteChange);

// ------------------------------------------------------------
// 列表视图：GET /api/discovery 按 app 分组展示
// ------------------------------------------------------------

async function showList() {
  clearError();
  editorView.classList.add("hidden");
  listView.classList.remove("hidden");
  listView.innerHTML = '<p class="empty-state">加载中…</p>';

  let data;
  try {
    data = await api("GET", "/discovery");
  } catch (err) {
    // 请求失败：错误横幅提示，不渲染误导性空列表
    listView.innerHTML = "";
    showError("获取配置列表失败：" + err.message);
    return;
  }

  const apps = data.apps || [];
  if (apps.length === 0) {
    listView.innerHTML = '<p class="empty-state">仓库中还没有配置。点击右上角「新建配置」创建第一条。</p>';
    return;
  }

  const frag = document.createDocumentFragment();
  for (const appEntry of apps) {
    const card = document.createElement("div");
    card.className = "app-card";
    const title = document.createElement("h3");
    title.textContent = appEntry.app;
    card.appendChild(title);

    const table = document.createElement("table");
    const thead = document.createElement("thead");
    thead.innerHTML = "<tr><th>env</th><th>version</th></tr>";
    table.appendChild(thead);
    const tbody = document.createElement("tbody");
    for (const envEntry of appEntry.envs || []) {
      const tr = document.createElement("tr");
      tr.className = "env-row";
      tr.title = "点击查看/编辑";
      const tdEnv = document.createElement("td");
      tdEnv.textContent = envEntry.env;
      const tdVer = document.createElement("td");
      tdVer.textContent = envEntry.version;
      tr.appendChild(tdEnv);
      tr.appendChild(tdVer);
      tr.addEventListener("click", () => {
        location.hash = "#/" + appEntry.app + "/" + envEntry.env;
      });
      tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    card.appendChild(table);
    frag.appendChild(card);
  }
  listView.innerHTML = "";
  listView.appendChild(frag);
}

// ------------------------------------------------------------
// 编辑器：载入查看 GET /api/{app}/{env}
// ------------------------------------------------------------

function refreshVersionLabel(extra) {
  const v = state.version === null ? "未知" : state.version;
  editorVersion.textContent = "版本：" + v + (extra ? " · " + extra : "");
}

async function showEditor(app, env) {
  clearError();
  listView.classList.add("hidden");
  editorView.classList.remove("hidden");

  // 重置编辑器
  editorTitle.textContent = app + " / " + env;
  editorVersion.textContent = "加载中…";
  editorStatus.textContent = "";
  tableStatus.textContent = "";
  editorBody.classList.remove("hidden");
  notFoundBox.classList.add("hidden");
  state.app = app;
  state.env = env;
  state.version = null;
  state.mode = "text";

  let data;
  try {
    data = await api("GET", "/" + app + "/" + env);
  } catch (err) {
    if (err.code === "not_found") {
      // 配置不存在（如查看期间被删除）
      editorBody.classList.add("hidden");
      editorVersion.textContent = "";
      notFoundBox.innerHTML = '<p>配置不存在（' + app + "/" + env + '）。<a href="#/">返回列表</a></p>';
      notFoundBox.classList.remove("hidden");
    } else {
      editorVersion.textContent = "";
      showError("读取配置失败：" + err.message);
    }
    return;
  }

  state.version = data.version;
  state.text = JSON.stringify(data.content, null, 2);
  setMode("text");
  refreshVersionLabel();
}

// ------------------------------------------------------------
// JSON 文本编辑模式（保存流程 text/table 共用）
// ------------------------------------------------------------

// textarea ↔ 权威缓冲实时同步
function bindTextBuffer() {
  jsonText.addEventListener("input", () => {
    state.text = jsonText.value;
  });
}

// 格式化：parse → stringify(obj, null, 2) 回填；失败提示且不改动原文
function formatText() {
  clearError();
  editorStatus.textContent = "";
  let obj;
  try {
    obj = JSON.parse(jsonText.value);
  } catch (e) {
    editorStatus.textContent = "JSON 解析失败：" + e.message + "（原文未改动）";
    return;
  }
  state.text = JSON.stringify(obj, null, 2);
  jsonText.value = state.text;
  editorStatus.textContent = "已格式化";
}

// 保存：本地校验 → PUT → 展示新版本；本地校验失败不发请求；服务端错误不动本地缓冲
async function saveConfig() {
  clearError();
  editorStatus.textContent = "";
  tableStatus.textContent = "";

  // 键值表模式：保存前先 rebuild 回写权威缓冲（失败有红行，阻止保存）
  if (state.mode === "table") {
    if (!commitTable()) {
      tableStatus.textContent = "存在非法复杂值（红行），请修正后再保存";
      return;
    }
  }

  // 前端本地校验：失败立即拦截，不发请求
  let obj;
  try {
    obj = JSON.parse(state.text);
  } catch (e) {
    editorStatus.textContent = "JSON 解析失败：" + e.message + "（未发起保存）";
    return;
  }
  if (!isPlainObject(obj)) {
    editorStatus.textContent = "顶层必须是 JSON 对象（未发起保存）";
    return;
  }

  let data;
  try {
    data = await api("PUT", "/" + state.app + "/" + state.env, JSON.stringify(obj));
  } catch (err) {
    // 服务端校验失败（invalid_json 等）：横幅提示，原配置未被覆盖，本地缓冲不动
    showError("保存失败（服务端未覆盖配置）：" + err.message);
    return;
  }
  state.version = data.version;
  refreshVersionLabel();
  editorStatus.textContent = "保存成功";
  tableStatus.textContent = "保存成功，新版本：" + data.version;
}

// ------------------------------------------------------------
// 键值表编辑模式
// flatten：递归下钻普通对象生成点路径行；键名含 "." 不下钻；
// 数组与其他非原始值 → 该键整行按「复杂值」处理（单元格为 JSON 文本片段）
// ------------------------------------------------------------

// 返回 [{segments, value, complex}]；segments 为逐级键名数组（点路径 = segments.join(".")）
function flatten(obj, prefix) {
  const rows = [];
  for (const k of Object.keys(obj)) {
    const v = obj[k];
    const segs = prefix.concat(k);
    if (isPlainObject(v) && !k.includes(".")) {
      // 普通对象且键名无点：下钻
      rows.push(...flatten(v, segs));
    } else if (isPrimitive(v) && !k.includes(".")) {
      // 原始叶子值：直接编辑
      rows.push({ segments: segs, value: v, complex: false });
    } else {
      // 复杂值（数组/嵌套对象/键名含点）：整体 JSON 片段编辑
      rows.push({ segments: segs, value: v, complex: true });
    }
  }
  return rows;
}

function renderTable(obj) {
  tableRows = flatten(obj, []);
  tableBody.innerHTML = "";
  tableRows.forEach((row, i) => {
    const tr = document.createElement("tr");
    const tdPath = document.createElement("td");
    tdPath.textContent = row.segments.join(".");
    tdPath.className = "path-cell";
    const tdVal = document.createElement("td");
    const input = document.createElement("input");
    input.type = "text";
    input.dataset.index = String(i);
    if (row.complex) {
      input.value = JSON.stringify(row.value);
      input.classList.add("complex-value");
      input.title = "复杂值：请以 JSON 片段形式整体编辑";
    } else if (row.value === null) {
      input.value = "null";
    } else {
      input.value = String(row.value);
    }
    // 编辑后清除该行错误标红
    input.addEventListener("input", () => {
      input.closest("tr").classList.remove("row-error");
    });
    tdVal.appendChild(input);
    tr.appendChild(tdPath);
    tr.appendChild(tdVal);
    tableBody.appendChild(tr);
  });
}

// rebuild：表格内容按 segments 重组为 JSON 对象。
// 叶子单元格先尝试 JSON.parse（识别数字/布尔/null 字面量），失败按字符串；
// 复杂值单元格整体 JSON.parse，失败标红该行并阻止保存。
function rebuildFromTable() {
  const root = {};
  let allValid = true;
  const inputs = tableBody.querySelectorAll("input");
  inputs.forEach((input) => {
    const row = tableRows[Number(input.dataset.index)];
    const raw = input.value;
    let value;
    if (row.complex) {
      try {
        value = JSON.parse(raw);
      } catch (e) {
        allValid = false;
        input.closest("tr").classList.add("row-error");
        return;
      }
    } else {
      const t = raw.trim();
      if (t === "") {
        value = "";
      } else {
        try {
          value = JSON.parse(t);
        } catch (e) {
          value = raw; // 非字面量，按字符串
        }
      }
    }
    // 按逐级键名写入嵌套对象（点路径键保持原样，不拆分）
    let node = root;
    for (let j = 0; j < row.segments.length - 1; j++) {
      const seg = row.segments[j];
      if (!isPlainObject(node[seg])) node[seg] = {};
      node = node[seg];
    }
    node[row.segments[row.segments.length - 1]] = value;
  });
  return { ok: allValid, obj: root };
}

// 键值表 → 权威缓冲回写；存在非法红行时返回 false（阻止保存与切换）
function commitTable() {
  const res = rebuildFromTable();
  if (!res.ok) return false;
  state.text = JSON.stringify(res.obj, null, 2);
  jsonText.value = state.text;
  return true;
}

// ------------------------------------------------------------
// 双模式互转：一律以权威缓冲 text 或其派生结果为准，不静默丢弃未保存修改
// ------------------------------------------------------------

function setMode(mode) {
  if (mode !== state.mode) {
    if (mode === "table") {
      // text→table：当前 text 可解析才切换，失败报错并停留原模式
      let obj;
      try {
        obj = JSON.parse(state.text);
      } catch (e) {
        showError("当前 JSON 非法，无法切换到键值表：" + e.message);
        return;
      }
      renderTable(obj);
    } else {
      // table→text：rebuild 回写 text；存在非法红行则不切换
      if (!commitTable()) {
        showError("键值表存在非法复杂值（红行），无法切回文本模式");
        return;
      }
      tableStatus.textContent = "";
    }
    state.mode = mode;
  }
  applyModeDom();
}

function applyModeDom() {
  const isText = state.mode === "text";
  textPane.classList.toggle("hidden", !isText);
  tablePane.classList.toggle("hidden", isText);
  btnModeText.classList.toggle("active", isText);
  btnModeTable.classList.toggle("active", !isText);
  if (isText) {
    jsonText.value = state.text;
  }
}

// ------------------------------------------------------------
// 新建配置对话框
// ------------------------------------------------------------

// 覆盖确认状态：true 表示已向用户明示覆盖警告，等待其显式确认后执行
let overwriteArmed = false;

function showNewError(msg) {
  newError.textContent = msg;
  newError.classList.remove("hidden");
}

function hideNewError() {
  newError.textContent = "";
  newError.classList.add("hidden");
}

function openNewDialog() {
  hideNewError();
  newApp.value = "";
  newEnv.value = "";
  newJson.value = "{}";
  overwriteArmed = false;
  btnNewSubmit.textContent = "创建";
  newDialog.showModal();
}

// 输入变更后重置覆盖确认状态（目标变了需重新探测）
function resetOverwriteArm() {
  if (overwriteArmed) {
    overwriteArmed = false;
    btnNewSubmit.textContent = "创建";
    hideNewError();
  }
}

async function submitNewConfig() {
  hideNewError();

  // 前端预校验：不合法直接提示，不提交
  const app = newApp.value.trim();
  const env = newEnv.value.trim();
  if (!NAME_RE.test(app) || !NAME_RE.test(env)) {
    showNewError("app/env 只能含字母、数字、下划线、连字符（[a-zA-Z0-9_-]+）");
    return;
  }
  let obj;
  try {
    obj = JSON.parse(newJson.value);
  } catch (e) {
    showNewError("初始 JSON 非法：" + e.message);
    return;
  }
  if (!isPlainObject(obj)) {
    showNewError("初始 JSON 必须是顶层对象");
    return;
  }

  // 存在性探测：已存在 → 明示覆盖警告，等待用户再次点击（显式确认）才执行
  if (!overwriteArmed) {
    let exists = false;
    try {
      await api("GET", "/" + app + "/" + env);
      exists = true;
    } catch (err) {
      if (err.code !== "not_found") {
        showNewError("探测配置存在性失败：" + err.message);
        return;
      }
    }
    if (exists) {
      overwriteArmed = true;
      btnNewSubmit.textContent = "确认覆盖";
      showNewError("该配置已存在，提交将覆盖现有内容。确认无误请再次点击「确认覆盖」。");
      return;
    }
  }

  let data;
  try {
    data = await api("PUT", "/" + app + "/" + env, JSON.stringify(obj));
  } catch (err) {
    if (err.code === "invalid_name") {
      // 服务端兜底校验触发：展示命名规则提示
      showNewError("命名不合法：app/env 需满足 [a-zA-Z0-9_-]+");
    } else {
      showNewError("创建失败：" + err.message);
    }
    return;
  }

  // 成功：关闭对话框，跳转编辑器展示首版版本号，列表下次进入时刷新
  newDialog.close();
  const target = "#/" + app + "/" + env;
  if (location.hash === target) {
    onRouteChange();
  } else {
    location.hash = target;
  }
}

// ------------------------------------------------------------
// 事件绑定与初始化
// ------------------------------------------------------------

bindTextBuffer();

$("btn-new").addEventListener("click", openNewDialog);
$("btn-new-cancel").addEventListener("click", () => newDialog.close());
btnNewSubmit.addEventListener("click", submitNewConfig);
newApp.addEventListener("input", resetOverwriteArm);
newEnv.addEventListener("input", resetOverwriteArm);

btnModeText.addEventListener("click", () => setMode("text"));
btnModeTable.addEventListener("click", () => setMode("table"));
$("btn-format").addEventListener("click", formatText);
$("btn-save").addEventListener("click", saveConfig);
$("btn-save-table").addEventListener("click", saveConfig);

onRouteChange();
