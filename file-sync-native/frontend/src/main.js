import './style.css';

import { EventsOn } from '../wailsjs/runtime/runtime';
import {
  ListTasks, CreateTask, UpdateTask, DeleteTask,
  GetSettings, UpdateSettings, PickFolder,
  ScanTask, StartSync, CancelSync, ConfirmDeletes, Progress,
} from '../wailsjs/go/main/App';

const $ = (s, el = document) => el.querySelector(s);

let tasks = [];
let editingId = null;
let diffTask = null;
let settings = { backup_root: "" };
let targetTouched = false;
let confirmTaskId = null;
const running = new Set();

const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

const statusText = st => ({
  idle: "空闲", pending: "准备中", scanning: "扫描中", copying: "复制中",
  awaiting_delete: "待确认删除", deleting: "删除中",
  completed: "已完成", error: "错误", cancelled: "已取消",
}[st] || st);

const ACTIVE = ["pending", "scanning", "copying", "awaiting_delete", "deleting"];

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
  if (root) return root + "\\" + fitName(name);
  return s + "-sync";
}

function humanSize(n) {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(u.length - 1, Math.floor(Math.log2(n) / 10));
  return (n / 2 ** (10 * i)).toFixed(i === 0 ? 0 : 1) + " " + u[i];
}

function humanEta(sec) {
  if (!Number.isFinite(sec) || sec <= 0) return "";
  if (sec < 60) return `约 ${sec} 秒`;
  if (sec < 3600) return `约 ${Math.floor(sec / 60)} 分 ${sec % 60} 秒`;
  return `约 ${Math.floor(sec / 3600)} 时 ${Math.floor((sec % 3600) / 60)} 分`;
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
  tasks = await ListTasks();
  render();
  // 页面刷新/重启后恢复进行中的任务状态
  for (const t of tasks) {
    const p = await Progress(t.id).catch(() => null);
    if (p && ACTIVE.includes(p.status)) {
      running.add(t.id);
      render();
      applyProgress(p);
    }
  }
}

async function loadSettings() {
  settings = await GetSettings();
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
        <span class="badge" data-badge>${isRunning ? "同步中" : "空闲"}</span>
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

function progressLine(p) {
  switch (p.status) {
    case "scanning": {
      const copy = p.total_bytes > 0
        ? ` · 已复制 ${humanSize(p.done_bytes)}/${humanSize(p.total_bytes)}` : "";
      return `扫描中… 已发现 ${p.scanned_files} 个文件${copy}<br><span class="mono">${esc(p.current_path || "")}</span>`;
    }
    case "copying": {
      const eta = p.eta_seconds > 0 ? `，剩余 ${humanEta(p.eta_seconds)}` : "";
      const scan = p.scanned_files > 0 ? `（源已发现 ${p.scanned_files}）` : "";
      return `复制中 ${p.done_files}/${p.total_files} 个文件 · ${humanSize(p.done_bytes)}/${humanSize(p.total_bytes)} · ${humanSize(p.speed_bps)}/s${eta}${scan}<br><span class="mono">${esc(p.current_path || "")}</span>`;
    }
    case "deleting":
      return `删除中 ${p.done_files}/${p.total_files}<br><span class="mono">${esc(p.current_path || "")}</span>`;
    case "awaiting_delete":
      return "复制完成，等待删除确认…";
    case "pending":
      return "准备中…";
    default:
      return p.status;
  }
}

function applyProgress(p) {
  const task = tasks.find(t => t.id === p.task_id);
  const card = $(`.card[data-id="${CSS.escape(p.task_id)}"]`);
  if (!card) return;
  const active = ACTIVE.includes(p.status);

  if (active) running.add(p.task_id);
  else running.delete(p.task_id);

  const badge = $("[data-badge]", card);
  badge.textContent = statusText(p.status);
  badge.className = "badge " + p.status;

  card.classList.toggle("running", active);
  card.querySelectorAll("button[data-act]:not([data-act='cancel'])")
    .forEach(b => b.disabled = active);
  $("[data-act='cancel']", card).hidden = !active;

  const prog = $("[data-progress]", card);
  if (active) {
    prog.hidden = false;
    $("[data-bar-fill]", prog).style.width = Math.min(100, p.percentage) + "%";
    $("[data-progress-text]", prog).innerHTML = progressLine(p);
  } else {
    prog.hidden = true;
  }

  if (p.status === "awaiting_delete" && p.pending_deletes?.length) {
    openDeleteConfirm(p.task_id, p.pending_deletes);
  }
  if (!active) loadTasks().catch(() => {});
}

EventsOn("sync:progress", applyProgress);

EventsOn("sync:finished", fin => {
  running.delete(fin.task_id);
  const name = tasks.find(t => t.id === fin.task_id)?.name || fin.task_id;
  if (fin.status === "completed") {
    const declined = fin.declined > 0 ? `（保留 ${fin.declined} 个未删除文件）` : "";
    toast(`任务「${name}」同步完成：复制 ${fin.copied}、跳过 ${fin.skipped}、删除 ${fin.deleted}${declined}`, "success");
  } else if (fin.status === "error") {
    toast(`任务「${name}」失败：${fin.error || (fin.errors && fin.errors[0]) || "未知错误"}`, "error");
    if (fin.errors?.length > 1) console.warn("同步错误明细:", fin.errors);
  } else if (fin.status === "cancelled") {
    toast(`任务「${name}」已取消`, "info");
  }
  if (confirmTaskId === fin.task_id) closeModal("#modal-delete");
  loadTasks().catch(() => {});
});

async function doSync(id, force, task) {
  await StartSync(id, force);
  running.add(id);
  render();
  applyProgress({ task_id: id, status: "pending" });
  void task;
}

async function showDiff(task) {
  const diff = await ScanTask(task.id);
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

// ---------- 删除二次确认 ----------

function openDeleteConfirm(taskId, list) {
  confirmTaskId = taskId;
  const name = tasks.find(t => t.id === taskId)?.name || taskId;
  $("#delete-task-name").textContent = "· " + name;
  $("#delete-count").textContent = list.length;
  const note = list.length >= 500 ? '<li class="diff-empty">仅显示前 500 项…</li>' : "";
  $("#delete-list").innerHTML = list.map(p => `<li class="deleted">${esc(p)}</li>`).join("") + note;
  openModal("#modal-delete");
}

$("#btn-delete-confirm").addEventListener("click", async () => {
  if (!confirmTaskId) return;
  try {
    await ConfirmDeletes(confirmTaskId, true);
    closeModal("#modal-delete");
    toast("已确认删除，正在执行…", "info");
  } catch (err) {
    toast(err.message || String(err), "error");
  }
});

$("#btn-delete-keep").addEventListener("click", async () => {
  if (!confirmTaskId) return;
  try {
    await ConfirmDeletes(confirmTaskId, false);
    closeModal("#modal-delete");
    toast("已保留这些文件，继续收尾…", "info");
  } catch (err) {
    toast(err.message || String(err), "error");
  }
});

// ---------- 模态框 ----------

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
    if (!targetTouched) f.elements.target_path.value = defaultTarget(e.target.value);
    if (!f.elements.name.value.trim()) f.elements.name.value = pathToName(e.target.value);
  }
  if (e.target.name === "target_path") targetTouched = true;
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
    if (editingId) await UpdateTask(editingId, body);
    else await CreateTask(body);
    closeModal("#modal-task");
    toast("任务已保存", "success");
    await loadTasks();
  } catch (err) {
    toast(err.message || String(err), "error");
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
        btn.disabled = true;
        btn.textContent = "扫描中…";
        try { await showDiff(task); } finally { btn.disabled = false; btn.textContent = "扫描差异"; }
        break;
      case "sync":
        await doSync(id, false, task);
        break;
      case "force":
        if (confirm(`强制同步将全量校验内容并覆盖「${task.name}」的目标目录，目标中多余的文件会被删除。确定继续？`))
          await doSync(id, true, task);
        break;
      case "edit":
        openTaskModal(task);
        break;
      case "delete":
        if (confirm(`删除任务「${task.name}」？（仅删除任务配置，不会删除文件）`)) {
          await DeleteTask(id);
          await loadTasks();
          toast("任务已删除", "success");
        }
        break;
      case "cancel":
        await CancelSync(id);
        toast("已请求取消", "info");
        break;
    }
  } catch (err) {
    toast(err.message || String(err), "error");
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

// 目录选择：系统原生对话框
document.querySelectorAll(".browse").forEach(btn => {
  btn.addEventListener("click", async () => {
    const input = $("#task-form").elements[btn.dataset.for];
    const chosen = await PickFolder(input.value.trim());
    if (!chosen) return;
    input.value = chosen;
    const f = $("#task-form");
    if (input.name === "source_path") {
      if (!f.elements.name.value.trim()) f.elements.name.value = pathToName(chosen);
      if (!targetTouched) f.elements.target_path.value = defaultTarget(chosen);
    }
  });
});

$("#btn-settings").addEventListener("click", () => {
  $("#settings-backup-root").value = settings.backup_root || "";
  openModal("#modal-settings");
});

$("#settings-form").addEventListener("submit", async e => {
  e.preventDefault();
  try {
    settings = await UpdateSettings($("#settings-backup-root").value.trim());
    closeModal("#modal-settings");
    toast("设置已保存", "success");
  } catch (err) {
    toast(err.message || String(err), "error");
  }
});

$("#btn-browse-settings").addEventListener("click", async () => {
  const chosen = await PickFolder($("#settings-backup-root").value.trim());
  if (chosen) $("#settings-backup-root").value = chosen;
});

Promise.all([loadTasks(), loadSettings()]).catch(err => toast("初始化失败：" + (err.message || err), "error"));
