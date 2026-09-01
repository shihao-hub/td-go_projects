const $ = (s, el = document) => el.querySelector(s);

let tasks = [];
let editingId = null;
let diffTask = null;
let settings = { backup_root: "" };
let targetTouched = false;
const running = new Set();
const esMap = new Map();
let browseInput = null;
let browseState = { path: "", parent: "" };

const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

const statusText = st => ({ idle: "空闲", pending: "准备中", scanning: "扫描中", syncing: "同步中", completed: "已完成", error: "错误" }[st] || st);

function fnv1a(str, seed) {
  let h = seed >>> 0;
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h;
}

const MAX_DIR_NAME = 64;

function pathToName(s) {
  return (s || "").trim().replace(/[\\/]+$/, "")
    .replace(/[\\/:]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function fitName(name) {
  if (name.length <= MAX_DIR_NAME) return name;
  return name.slice(0, MAX_DIR_NAME - 9) + "-" + (fnv1a(name, 0x811c9dc5) >>> 0).toString(16).padStart(8, "0");
}

function defaultTarget(source) {
  const s = (source || "").trim().replace(/[\\/]+$/, "");
  if (!s) return "";
  const name = pathToName(s);
  if (!name || /^[A-Za-z]:$/.test(s)) return "";
  const root = (settings.backup_root || "").trim().replace(/[\\/]+$/, "");
  if (root) {
    return root + "\\" + fitName(name);
  }
  return s + "-sync";
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...opts,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText || "请求失败");
  return data;
}

function toast(msg, type = "info") {
  const el = document.createElement("div");
  el.className = "toast " + type;
  el.textContent = msg;
  $("#toasts").appendChild(el);
  requestAnimationFrame(() => el.classList.add("show"));
  setTimeout(() => { el.classList.remove("show"); setTimeout(() => el.remove(), 300); }, 4500);
}

async function loadTasks() {
  tasks = await api("/api/tasks");
  render();
  tasks.forEach(t => { if (!running.has(t.id) && !esMap.has(t.id)) track(t); });
}

async function loadSettings() {
  settings = await api("/api/settings");
}

function render() {
  $("#empty-state").hidden = tasks.length > 0;
  $("#task-grid").innerHTML = tasks.map(t => {
    const isRunning = running.has(t.id);
    const last = t.last_sync && !t.last_sync.startsWith("0001-")
      ? new Date(t.last_sync).toLocaleString() : "从未";
    return `
    <div class="card${isRunning ? " running" : ""}" data-id="${esc(t.id)}">
      <div class="card-head">
        <div class="card-name" title="${esc(t.name)}">${esc(t.name)}</div>
        <span class="badge${isRunning ? " syncing" : ""}" data-badge>${isRunning ? "同步中" : "空闲"}</span>
      </div>
      <div class="card-paths">
        <div class="path">${esc(t.source_path)}</div>
        <div class="arrow">&#8595;</div>
        <div class="path">${esc(t.target_path)}</div>
      </div>
      <div class="card-meta">上次同步：${last}</div>
      <div class="progress" data-progress hidden>
        <div class="progress-text" data-progress-text></div>
        <div class="bar"><div class="bar-fill" data-bar-fill></div></div>
      </div>
      <div class="card-actions">
        <button class="btn" data-act="scan" ${isRunning ? "disabled" : ""}>扫描差异</button>
        <button class="btn primary" data-act="sync" ${isRunning ? "disabled" : ""}>同步</button>
        <button class="btn danger-ghost" data-act="force" ${isRunning ? "disabled" : ""}>强制同步</button>
        <button class="btn" data-act="edit" ${isRunning ? "disabled" : ""}>编辑</button>
        <button class="btn danger" data-act="delete" ${isRunning ? "disabled" : ""}>删除</button>
        <button class="btn" data-act="cancel" hidden>取消</button>
      </div>
    </div>`;
  }).join("");
}

function track(task) {
  const id = task.id;
  if (esMap.has(id)) esMap.get(id).close();
  const es = new EventSource(`/api/tasks/${id}/progress`);
  esMap.set(id, es);

  const finish = (fn) => {
    es.close();
    esMap.delete(id);
    running.delete(id);
    if (fn) fn();
    loadTasks().catch(() => {});
  };

  es.onmessage = ev => {
    let p;
    try { p = JSON.parse(ev.data); } catch { return; }
    if (p.status === "idle") {
      es.close();
      esMap.delete(id);
      if (running.delete(id)) loadTasks().catch(() => {});
      return;
    }
    const card = $(`.card[data-id="${CSS.escape(id)}"]`);
    if (!card) return;

    const badge = $("[data-badge]", card);
    badge.textContent = statusText(p.status);
    badge.className = "badge " + p.status;
    card.classList.toggle("running", ["pending", "scanning", "syncing"].includes(p.status));

    const actBtns = card.querySelectorAll("button[data-act]:not([data-act='cancel'])");
    actBtns.forEach(b => b.disabled = ["pending", "scanning", "syncing"].includes(p.status));
    $("[data-act='cancel']", card).hidden = !["pending", "scanning", "syncing"].includes(p.status);

    const prog = $("[data-progress]", card);
    if (["scanning", "syncing", "pending"].includes(p.status)) {
      prog.hidden = false;
      $("[data-bar-fill]", prog).style.width = p.percentage + "%";
      $("[data-progress-text]", prog).textContent = p.status === "scanning"
        ? `扫描中… 已扫描 ${p.current_file} 个文件（${p.current_path}）`
        : p.total_files > 0
          ? `${p.current_file} / ${p.total_files}（${esc(p.current_path)}）`
          : "准备中…";
    }
    if (p.status === "completed") finish(() => toast(`任务「${task.name}」同步完成`, "success"));
    if (p.status === "error") finish(() => toast(`同步失败：${p.error_message || "未知错误"}`, "error"));
  };
}

async function doSync(id, force, task) {
  await api(`/api/tasks/${id}/sync`, { method: "POST", body: { force } });
  running.add(id);
  render();
  track(task);
}

async function showDiff(task) {
  const diff = await api(`/api/tasks/${task.id}/scan`, { method: "POST" });
  diffTask = task;
  $("#diff-task-name").textContent = "· " + task.name;
  const total = diff.added.length + diff.modified.length + diff.deleted.length;
  $("#diff-stats").innerHTML = total === 0
    ? "没有差异，目标目录已是最新。"
    : `共 <b>${total}</b> 个变更：${diff.added.length} 新增、${diff.modified.length} 修改、${diff.deleted.length} 删除`;
  const fill = (ulId, items, cls) => {
    const ul = $(ulId);
    ul.innerHTML = items.length === 0
      ? `<li class="diff-empty">无</li>`
      : items.map(p => `<li class="${cls}" title="${esc(p)}">${esc(p)}</li>`).join("");
  };
  fill("#diff-added", diff.added, "added");
  fill("#diff-modified", diff.modified, "modified");
  fill("#diff-deleted", diff.deleted, "deleted");
  $("#count-added").textContent = `(${diff.added.length})`;
  $("#count-modified").textContent = `(${diff.modified.length})`;
  $("#count-deleted").textContent = `(${diff.deleted.length})`;
  $("#btn-diff-sync").hidden = total === 0;
  openModal("#modal-diff");
}

function openModal(sel) { $(sel).hidden = false; }
function closeModal(sel) { $(sel).hidden = true; }

function openTaskModal(task) {
  editingId = task?.id ?? null;
  $("#task-modal-title").textContent = editingId ? "编辑任务" : "新建任务";
  const f = $("#task-form");
  f.elements.name.value = task?.name ?? "";
  f.elements.source_path.value = task?.source_path ?? "";
  f.elements.target_path.value = task?.target_path ?? defaultTarget(task?.source_path);
  targetTouched = Boolean(task?.target_path);
  f.elements.ignore_rules.value = (task?.ignore_rules ?? []).join("\n");
  openModal("#modal-task");
  f.elements.name.focus();
}

$("#btn-add").addEventListener("click", () => openTaskModal());

$("#task-form").addEventListener("input", e => {
  const f = $("#task-form");
  if (e.target.name === "source_path") {
    if (!targetTouched) {
      f.elements.target_path.value = defaultTarget(e.target.value);
    }
    if (!f.elements.name.value.trim()) {
      f.elements.name.value = pathToName(e.target.value);
    }
  }
  if (e.target.name === "target_path") {
    targetTouched = true;
  }
});

$("#task-form").addEventListener("submit", async e => {
  e.preventDefault();
  const f = e.target;
  const body = {
    name: f.elements.name.value.trim(),
    source_path: f.elements.source_path.value.trim(),
    target_path: f.elements.target_path.value.trim(),
    ignore_rules: f.elements.ignore_rules.value.split("\n").map(s => s.trim()).filter(Boolean),
  };
  try {
    if (editingId) await api(`/api/tasks/${editingId}`, { method: "PUT", body });
    else await api("/api/tasks", { method: "POST", body });
    closeModal("#modal-task");
    toast("任务已保存", "success");
    await loadTasks();
  } catch (err) {
    toast(err.message, "error");
  }
});

$("#btn-diff-sync").addEventListener("click", async () => {
  closeModal("#modal-diff");
  if (diffTask) await doSync(diffTask.id, false, diffTask);
});

$("#task-grid").addEventListener("click", async e => {
  const btn = e.target.closest("button[data-act]");
  if (!btn || btn.disabled) return;
  const card = btn.closest(".card");
  const id = card.dataset.id;
  const task = tasks.find(t => t.id === id);
  if (!task) return;
  try {
    switch (btn.dataset.act) {
      case "scan":
        await showDiff(task);
        break;
      case "sync":
        await doSync(id, false, task);
        break;
      case "force":
        if (confirm(`强制同步将以源目录为准完全覆盖「${task.name}」的目标目录，目标中多余的文件会被删除。确定继续？`))
          await doSync(id, true, task);
        break;
      case "edit":
        openTaskModal(task);
        break;
      case "delete":
        if (confirm(`删除任务「${task.name}」？（仅删除任务配置，不会删除文件）`)) {
          await api(`/api/tasks/${id}`, { method: "DELETE" });
          await loadTasks();
          toast("任务已删除", "success");
        }
        break;
      case "cancel":
        await api(`/api/tasks/${id}/cancel`, { method: "POST" });
        toast("已请求取消", "info");
        break;
    }
  } catch (err) {
    toast(err.message, "error");
  }
});

document.querySelectorAll(".modal").forEach(m => {
  m.addEventListener("click", e => {
    if (e.target === m || e.target.closest("[data-close]")) m.hidden = true;
  });
});

document.addEventListener("keydown", e => {
  if (e.key === "Escape") document.querySelectorAll(".modal").forEach(m => m.hidden = true);
});

document.querySelectorAll(".browse").forEach(btn => {
  btn.addEventListener("click", async () => {
    browseInput = $("#task-form").elements[btn.dataset.for];
    await loadBrowse(browseInput.value.trim());
    openModal("#modal-browse");
  });
});

async function loadBrowse(p) {
  try {
    const data = await api(`/api/browse${p ? `?path=${encodeURIComponent(p)}` : ""}`);
    browseState = { path: data.path || "", parent: data.parent || "" };
    $("#browse-path").textContent = browseState.path || "请选择磁盘";
    $("#browse-list").innerHTML = (data.dirs || []).map(d =>
      `<li data-path="${esc(d.path)}" title="${esc(d.path)}">${esc(d.name)}</li>`).join("")
      || `<li class="diff-empty" style="cursor:default">没有子目录</li>`;
  } catch (err) {
    toast(err.message, "error");
  }
}

$("#browse-list").addEventListener("click", e => {
  const li = e.target.closest("li[data-path]");
  if (li) loadBrowse(li.dataset.path);
});

$("#browse-up").addEventListener("click", () => {
  if (browseState.parent) loadBrowse(browseState.parent);
});

$("#browse-select").addEventListener("click", () => {
  if (browseInput && browseState.path) {
    browseInput.value = browseState.path;
    const f = $("#task-form");
    if (browseInput.name === "source_path" && f.elements.name && !f.elements.name.value.trim()) {
      f.elements.name.value = pathToName(browseState.path);
    }
    if (browseInput.name === "source_path" && !targetTouched) {
      f.elements.target_path.value = defaultTarget(browseState.path);
    }
  }
  closeModal("#modal-browse");
});

$("#btn-settings").addEventListener("click", () => {
  $("#settings-backup-root").value = settings.backup_root || "";
  openModal("#modal-settings");
});

$("#settings-form").addEventListener("submit", async e => {
  e.preventDefault();
  try {
    settings = await api("/api/settings", { method: "PUT", body: { backup_root: $("#settings-backup-root").value.trim() } });
    closeModal("#modal-settings");
    toast("设置已保存", "success");
  } catch (err) {
    toast(err.message, "error");
  }
});

$("#btn-browse-settings").addEventListener("click", async () => {
  browseInput = $("#settings-backup-root");
  await loadBrowse(browseInput.value.trim());
  openModal("#modal-browse");
});

Promise.all([loadTasks(), loadSettings()]).catch(err => toast("初始化失败：" + err.message, "error"));
