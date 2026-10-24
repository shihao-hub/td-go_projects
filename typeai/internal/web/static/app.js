"use strict";

const $ = (id) => document.getElementById(id);

const state = {
  data: null,
  activeBranch: "",
  streaming: false,
  staged: [],
  temp: null,
  expanded: new Set(),
  abort: null,
  tempNodes: null,
  allSessions: [],
  sessionViewMode: localStorage.getItem("typeai_view_mode") || "tree",
  collapsedGroups: new Set(JSON.parse(localStorage.getItem("typeai_collapsed_groups") || "[]")),
  editingSessionID: null,
  modalTags: [],
};

const els = {
  sidebar: $("sidebar"),
  sessionList: $("session-list"),
  tabs: $("tabs"),
  topMeta: $("top-meta"),
  transcript: $("transcript"),
  input: $("input"),
  send: $("send"),
  cancel: $("cancel"),
  pickImage: $("pick-image"),
  fileInput: $("file-input"),
  attachments: $("attachments"),
  errorBar: $("error-bar"),
  channel: $("channel"),
  hint: $("hint"),
  viewModeTree: $("view-mode-tree"),
  viewModeList: $("view-mode-list"),
  contextMenu: $("session-context-menu"),
  sessionModal: $("session-modal"),
  sessionModalForm: $("session-modal-form"),
  sessionModalTitle: $("session-modal-title"),
  sessTitle: $("sess-title"),
  sessPinned: $("sess-pinned"),
  tagPillsBox: $("tag-pills-box"),
  tagPillsList: $("tag-pills-list"),
  tagPillInput: $("tag-pill-input"),
  tagPoolChips: $("tag-pool-chips"),
  sessDueDate: $("sess-due-date"),
  sessClearDue: $("sess-clear-due"),
  sessionModalCancel: $("session-modal-cancel"),
  settings: $("settings"),
  settingsForm: $("settings-form"),
  cfgBaseURL: $("cfg-base-url"),
  cfgModel: $("cfg-model"),
  cfgAPIKey: $("cfg-api-key"),
  cfgNote: $("cfg-note"),
};

/* ---------------- transport ---------------- */

async function api(path, options = {}) {
  const method = options.method || "GET";
  const init = { method };
  if (options.body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(options.body);
  }
  const res = await fetch(path, init);
  const text = await res.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = null;
  }
  if (!res.ok) {
    const message = data && data.error && data.error.message ? data.error.message : text || res.statusText;
    throw new Error(message);
  }
  return data;
}

async function readSSE(res, onEvent) {
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    let index;
    while ((index = buffer.indexOf("\n\n")) >= 0) {
      const chunk = buffer.slice(0, index);
      buffer = buffer.slice(index + 2);
      let event = "message";
      const dataLines = [];
      for (const line of chunk.split("\n")) {
        if (line.startsWith("event:")) event = line.slice(6).trim();
        else if (line.startsWith("data:")) dataLines.push(line.slice(5).trim());
      }
      if (!dataLines.length) continue;
      try {
        onEvent(event, JSON.parse(dataLines.join("\n")));
      } catch {
        /* 忽略无法解析的事件 */
      }
    }
  }
}

/* ---------------- state ---------------- */

async function loadState() {
  const data = await api("/api/state");
  state.data = data;
  const branches = data.session.branches || [];
  if (!state.activeBranch || !branches.some((b) => b.id === state.activeBranch)) {
    state.activeBranch = data.session.active_branch_id || (branches[0] && branches[0].id) || "";
  }
  state.streaming = data.streaming;
  renderAll();
}

function activeBranch() {
  if (!state.data) return null;
  return (state.data.session.branches || []).find((b) => b.id === state.activeBranch) || null;
}

function applySession(session) {
  if (!state.data || !session) return;
  state.data.session = session;
  if (!(session.branches || []).some((b) => b.id === state.activeBranch)) {
    state.activeBranch = session.active_branch_id;
  }
  renderTabs();
  renderTranscript();
  renderMeta();
}

/* ---------------- rendering ---------------- */

function renderAll() {
  renderMeta();
  renderTabs();
  renderTranscript();
  renderComposer();
  renderChannel();
}

function renderChannel() {
  if (!state.data) return;
  els.channel.textContent = `${state.data.channel} · ${state.data.version}`;
}

function renderMeta() {
  if (!state.data) return;
  const session = state.data.session;
  const titlePart = session.title ? `【${session.title}】 ` : "";
  els.topMeta.textContent = `${titlePart}${session.model} · ${session.id} · [${state.activeBranch}]`;
}

function renderTabs() {
  const branches = (state.data && state.data.session.branches) || [];
  els.tabs.innerHTML = "";
  for (const branch of branches) {
    const tab = document.createElement("button");
    tab.className = "tab";
    if (branch.id === state.activeBranch) tab.classList.add("active");
    if (state.temp && state.temp.branch === branch.id) tab.classList.add("running");
    tab.textContent = branch.id;
    tab.title = "单击切换 · 双击重命名 · 右键删除";
    tab.addEventListener("click", () => switchBranch(branch.id));
    tab.addEventListener("dblclick", (event) => {
      event.preventDefault();
      renameBranch(branch.id);
    });
    tab.addEventListener("contextmenu", (event) => {
      event.preventDefault();
      deleteBranch(branch.id);
    });
    els.tabs.append(tab);
  }
  const fork = document.createElement("button");
  fork.className = "tab";
  fork.textContent = "＋";
  fork.title = "在最后一条 AI 回答之后创建分支";
  fork.addEventListener("click", () => forkLast());
  els.tabs.append(fork);
}

function renderTranscript() {
  const branch = activeBranch();
  const messages = branch ? branch.messages || [] : [];
  const follow = els.transcript.scrollHeight - els.transcript.scrollTop - els.transcript.clientHeight < 160;
  els.transcript.innerHTML = "";
  state.tempNodes = null;

  if (!messages.length && !state.temp) {
    const note = document.createElement("div");
    note.className = "empty-note";
    note.textContent = "还没有消息。输入内容开始对话，或从左侧恢复历史会话。";
    els.transcript.append(note);
    return;
  }

  messages.forEach((message, index) => {
    els.transcript.append(renderMessage(message, index));
  });

  if (state.temp) {
    els.transcript.append(renderTemp());
  }
  if (follow || state.temp) scrollToBottom();
}

function renderMessage(message, index) {
  const wrap = document.createElement("article");
  wrap.className = "msg";

  const head = document.createElement("div");
  head.className = "msg-head";
  const role = document.createElement("span");
  role.className = message.role === "user" ? "role-user" : "role-ai";
  role.textContent = message.role === "user" ? "You" : "AI";
  head.append(role);

  if (message.role === "assistant") {
    const fork = document.createElement("button");
    fork.className = "fork-btn";
    fork.textContent = "分支";
    fork.title = "从此回答创建分支";
    fork.addEventListener("click", () => forkAt(index));
    head.append(fork);
  }
  wrap.append(head);

  const body = document.createElement("div");
  body.className = "msg-body";
  if (message.role === "assistant") {
    const content = message.content || "";
    const key = `${state.activeBranch}:${index}`;
    const fold = content.length >= 2000 || content.split("\n").length >= 40;

    body.classList.add("rendered");
    body.innerHTML = renderMarkdown(content);

    if (fold) {
      const isExpanded = state.expanded.has(key);
      if (!isExpanded) {
        body.classList.add("clamped");
      }
      const note = document.createElement("div");
      note.className = "clamp-note";
      note.append(`… ${content.length} 字符 · ${content.split("\n").length} 行 `);
      const button = document.createElement("button");
      button.textContent = isExpanded ? "收起" : "展开";
      button.addEventListener("click", () => {
        if (state.expanded.has(key)) {
          state.expanded.delete(key);
          body.classList.add("clamped");
          button.textContent = "展开";
        } else {
          state.expanded.add(key);
          body.classList.remove("clamped");
          button.textContent = "收起";
        }
      });
      note.append(button);
      wrap.append(body, note);
    } else {
      wrap.append(body);
    }
  } else {
    body.textContent = message.content || "";
    wrap.append(body);
  }

  appendImages(wrap, message.images);
  return wrap;
}

function appendImages(container, images) {
  if (!images || !images.length) return;
  const row = document.createElement("div");
  row.className = "msg-images";
  for (const image of images) {
    const img = document.createElement("img");
    img.src = `/api/image?path=${encodeURIComponent(image.path)}`;
    img.alt = image.file_name || "image";
    img.title = image.file_name || image.path;
    img.addEventListener("click", () => window.open(img.src, "_blank"));
    row.append(img);
  }
  container.append(row);
}

function renderTemp() {
  const temp = state.temp;
  const wrap = document.createElement("article");
  wrap.className = "msg";

  if (temp.user) {
    const head = document.createElement("div");
    head.className = "msg-head";
    const role = document.createElement("span");
    role.className = "role-user";
    role.textContent = "You";
    head.append(role);
    const body = document.createElement("div");
    body.className = "msg-body";
    body.textContent = temp.user.content || "";
    wrap.append(head, body);
    appendImages(wrap, temp.user.images);
  }

  const head = document.createElement("div");
  head.className = "msg-head";
  const role = document.createElement("span");
  role.className = "role-ai";
  role.textContent = "AI";
  head.append(role);

  const thinking = document.createElement("details");
  thinking.className = "thinking";
  thinking.hidden = true;
  const summary = document.createElement("summary");
  const reasoningBody = document.createElement("div");
  thinking.append(summary, reasoningBody);

  const body = document.createElement("div");
  body.className = "msg-body";
  const answerText = document.createElement("span");
  const waiting = document.createElement("span");
  waiting.className = "waiting";
  waiting.textContent = "正在等待回答…";
  body.append(answerText, waiting);

  const error = document.createElement("div");
  error.className = "error-bar";
  error.hidden = true;

  wrap.append(head, thinking, body, error);
  state.tempNodes = { summary, reasoningBody, answerText, waiting, error };
  updateTemp();
  return wrap;
}

function updateTemp() {
  const nodes = state.tempNodes;
  const temp = state.temp;
  if (!nodes || !temp) return;

  if (temp.reasoning) {
    nodes.summary.textContent = `Thinking · ${temp.reasoning.length} 字符`;
    nodes.reasoningBody.textContent = temp.reasoning;
    nodes.summary.parentElement.hidden = false;
  }
  if (temp.answer) {
    nodes.answerText.textContent = temp.answer;
    nodes.waiting.hidden = true;
  }
  if (temp.error) {
    nodes.error.textContent = temp.error;
    nodes.error.hidden = false;
    nodes.waiting.hidden = true;
  }
  scrollToBottom();
}

function renderComposer() {
  els.send.disabled = state.streaming;
  els.send.hidden = state.streaming;
  els.cancel.hidden = !state.streaming;
  els.hint.textContent = state.streaming ? "回答生成中…" : "";
  renderAttachments();
}

function renderAttachments() {
  els.attachments.innerHTML = "";
  els.attachments.hidden = state.staged.length === 0;
  state.staged.forEach((image, index) => {
    const chip = document.createElement("div");
    chip.className = "attachment";
    const thumb = document.createElement("img");
    thumb.src = `/api/image?path=${encodeURIComponent(image.path)}`;
    const label = document.createElement("span");
    label.textContent = image.file_name;
    const remove = document.createElement("button");
    remove.textContent = "×";
    remove.title = "移除";
    remove.addEventListener("click", () => {
      state.staged.splice(index, 1);
      renderAttachments();
    });
    chip.append(thumb, label, remove);
    els.attachments.append(chip);
  });
}

/* ---------------- sessions & tree ---------------- */

async function loadSessions() {
  try {
    const data = await api("/api/sessions");
    state.allSessions = (data && data.items) || [];
    renderSessions();
  } catch (error) {
    showError(error.message);
  }
}

function formatDueDate(due) {
  if (!due) return "";
  const m = due.match(/^(\d{4})-(\d{2}-\d{2})$/);
  if (m) {
    const year = m[1];
    const monthDay = m[2];
    const curYear = String(new Date().getFullYear());
    return year === curYear ? `${monthDay} 截止` : `${due} 截止`;
  }
  return `${due} 截止`;
}

function renderSessionCard(item, currentSessionID) {
  const card = document.createElement("div");
  card.className = "session-item";
  if (item.id === currentSessionID) {
    card.classList.add("active");
  }
  if (item.pinned) {
    card.classList.add("pinned");
  }

  const header = document.createElement("div");
  header.className = "session-header";

  const title = document.createElement("div");
  title.className = "session-title";
  const displayTitle = item.title || item.first_user || item.id;
  title.textContent = displayTitle;
  title.title = displayTitle;

  const actions = document.createElement("div");
  actions.className = "session-actions";

  const renameBtn = document.createElement("button");
  renameBtn.type = "button";
  renameBtn.className = "session-action-btn";
  renameBtn.textContent = "✎";
  renameBtn.title = "重命名会话（双击卡片亦可重命名）";
  renameBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    renameSession(item.id, item.title || item.first_user || "");
  });

  const pinBtn = document.createElement("button");
  pinBtn.type = "button";
  pinBtn.className = "session-action-btn pin-btn" + (item.pinned ? " active" : "");
  pinBtn.textContent = "⚲";
  pinBtn.title = item.pinned ? "取消置顶" : "置顶会话";
  pinBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    togglePin(item.id);
  });

  const editBtn = document.createElement("button");
  editBtn.type = "button";
  editBtn.className = "session-action-btn";
  editBtn.textContent = "⚙";
  editBtn.title = "编辑会话属性（标签、截止时间等）";
  editBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    openSessionModal(item.id);
  });

  actions.append(renameBtn, pinBtn, editBtn);
  header.append(title, actions);
  card.append(header);

  // Badges row
  const badges = document.createElement("div");
  badges.className = "session-badges";

  const tags = item.tags || [];
  for (const tag of tags) {
    const tagBadge = document.createElement("span");
    tagBadge.className = "badge badge-tag";
    if (tag === "待办") tagBadge.classList.add("tag-todo");
    else if (tag === "归档") tagBadge.classList.add("tag-archive");
    tagBadge.textContent = tag;
    badges.append(tagBadge);
  }

  if (item.due_date) {
    const dueBadge = document.createElement("span");
    dueBadge.className = "badge badge-due";
    dueBadge.textContent = formatDueDate(item.due_date);
    badges.append(dueBadge);
  }

  if (badges.children.length > 0) {
    card.append(badges);
  }

  const meta = document.createElement("div");
  meta.className = "session-meta";
  const when = new Date(item.updated_at).toLocaleString();
  meta.textContent = `${when} · ${item.message_count} 条 · ${item.model}`;
  card.append(meta);

  card.addEventListener("dblclick", (e) => {
    e.preventDefault();
    e.stopPropagation();
    renameSession(item.id, item.title || item.first_user || "");
  });

  card.addEventListener("click", () => {
    if (item.id === currentSessionID) {
      els.input.focus();
      return;
    }
    resumeSession(item.id);
  });

  card.addEventListener("contextmenu", (e) => {
    e.preventDefault();
    showContextMenu(e.clientX, e.clientY, item);
  });

  return card;
}

function renderSessions() {
  els.sessionList.innerHTML = "";
  const items = state.allSessions || [];
  if (!items.length) {
    const note = document.createElement("div");
    note.className = "empty-note";
    note.textContent = "暂无其他历史会话";
    els.sessionList.append(note);
    return;
  }

  const currentSessionID = state.data && state.data.session ? state.data.session.id : "";

  // 辅助函数：组内排序，置顶排在最前，同优先级按更新时间倒序
  const sortGroupItems = (list) => {
    return [...list].sort((a, b) => {
      if (!!b.pinned !== !!a.pinned) {
        return b.pinned ? 1 : -1;
      }
      return new Date(b.updated_at) - new Date(a.updated_at);
    });
  };

  if (state.sessionViewMode === "list") {
    for (const item of sortGroupItems(items)) {
      els.sessionList.append(renderSessionCard(item, currentSessionID));
    }
    return;
  }

  // Tree View mode - 方案 B：置顶作为组内属性沉淀在各分组头部，消除重复的顶部置顶大组
  const todoItems = sortGroupItems(items.filter((item) => (item.tags || []).includes("待办")));
  const archiveItems = sortGroupItems(items.filter((item) => (item.tags || []).includes("归档")));

  const otherTagMap = new Map();
  for (const item of items) {
    for (const tag of item.tags || []) {
      if (tag === "待办" || tag === "归档") continue;
      if (!otherTagMap.has(tag)) otherTagMap.set(tag, []);
      otherTagMap.get(tag).push(item);
    }
  }

  const uncategorized = sortGroupItems(items.filter((item) => !item.tags || item.tags.length === 0));

  const renderGroup = (groupId, title, groupItems) => {
    if (!groupItems.length) return;
    const group = document.createElement("div");
    group.className = "tree-group";
    if (state.collapsedGroups.has(groupId)) {
      group.classList.add("collapsed");
    }

    const header = document.createElement("div");
    header.className = "tree-group-header";

    const arrow = document.createElement("span");
    arrow.className = "tree-arrow";
    arrow.textContent = "▾";

    const titleEl = document.createElement("span");
    titleEl.className = "tree-title";
    titleEl.textContent = title;

    const countEl = document.createElement("span");
    countEl.className = "tree-count";
    countEl.textContent = groupItems.length;

    header.append(arrow, titleEl, countEl);
    header.addEventListener("click", () => {
      if (state.collapsedGroups.has(groupId)) {
        state.collapsedGroups.delete(groupId);
        group.classList.remove("collapsed");
      } else {
        state.collapsedGroups.add(groupId);
        group.classList.add("collapsed");
      }
      localStorage.setItem("typeai_collapsed_groups", JSON.stringify(Array.from(state.collapsedGroups)));
    });

    const itemsContainer = document.createElement("div");
    itemsContainer.className = "tree-group-items";
    for (const item of groupItems) {
      itemsContainer.append(renderSessionCard(item, currentSessionID));
    }

    group.append(header, itemsContainer);
    els.sessionList.append(group);
  };

  renderGroup("tag_todo", "待办", todoItems);
  renderGroup("tag_archive", "归档", archiveItems);

  for (const [tag, tagItems] of Array.from(otherTagMap.entries()).sort((a, b) => a[0].localeCompare(b[0]))) {
    renderGroup("tag_" + tag, tag, sortGroupItems(tagItems));
  }

  renderGroup("uncategorized", "未分类", uncategorized);
}

/* ---------------- context menu & meta ---------------- */

function hideContextMenu() {
  els.contextMenu.hidden = true;
  els.contextMenu.innerHTML = "";
}

function showContextMenu(x, y, item) {
  hideContextMenu();
  const menu = els.contextMenu;

  const hasTodo = (item.tags || []).includes("待办");
  const hasArchive = (item.tags || []).includes("归档");

  const actions = [
    {
      icon: "✎",
      label: "重命名会话…",
      onClick: () => renameSession(item.id, item.title || item.first_user || ""),
    },
    {
      icon: "⚲",
      label: item.pinned ? "取消置顶" : "置顶会话",
      onClick: () => togglePin(item.id),
    },
    {
      icon: "◇",
      label: hasTodo ? "移除「待办」" : "设为「待办」",
      onClick: () => toggleTag(item.id, "待办"),
    },
    {
      icon: "⌸",
      label: hasArchive ? "移除「归档」" : "设为「归档」",
      onClick: () => toggleTag(item.id, "归档"),
    },
    {
      icon: "✎",
      label: "编辑标签与截止时间…",
      onClick: () => openSessionModal(item.id),
    },
    { divider: true },
    {
      icon: "✕",
      label: "删除会话…",
      danger: true,
      onClick: () => deleteSession(item.id),
    },
  ];

  for (const act of actions) {
    if (act.divider) {
      const div = document.createElement("div");
      div.className = "context-menu-divider";
      menu.append(div);
      continue;
    }
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "context-menu-item" + (act.danger ? " danger" : "");
    btn.innerHTML = `<span>${act.icon}</span><span>${escapeHtml(act.label)}</span>`;
    btn.addEventListener("click", () => {
      hideContextMenu();
      act.onClick();
    });
    menu.append(btn);
  }

  menu.style.left = `${Math.min(x, window.innerWidth - 170)}px`;
  menu.style.top = `${Math.min(y, window.innerHeight - 220)}px`;
  menu.hidden = false;
}

async function togglePin(sessionID) {
  const item = state.allSessions.find((s) => s.id === sessionID);
  if (!item) return;
  const nextPinned = !item.pinned;
  try {
    await api("/api/sessions/meta", {
      method: "POST",
      body: { id: sessionID, pinned: nextPinned },
    });
    if (state.data && state.data.session && state.data.session.id === sessionID) {
      state.data.session.pinned = nextPinned;
    }
    await loadSessions();
  } catch (error) {
    showError(error.message);
  }
}

async function toggleTag(sessionID, tag) {
  const item = state.allSessions.find((s) => s.id === sessionID);
  if (!item) return;
  const currentTags = item.tags || [];
  let nextTags;
  if (currentTags.includes(tag)) {
    nextTags = currentTags.filter((t) => t !== tag);
  } else {
    nextTags = currentTags.concat(tag);
  }
  try {
    await api("/api/sessions/meta", {
      method: "POST",
      body: { id: sessionID, tags: nextTags },
    });
    if (state.data && state.data.session && state.data.session.id === sessionID) {
      state.data.session.tags = nextTags;
    }
    await loadSessions();
  } catch (error) {
    showError(error.message);
  }
}

async function renameSession(id, currentTitle) {
  const next = window.prompt("输入会话新标题（清空则恢复默认提问预览）：", currentTitle || "");
  if (next === null) return;
  try {
    const data = await api("/api/sessions/rename", {
      method: "POST",
      body: { id, title: next.trim() },
    });
    if (state.data && state.data.session && state.data.session.id === id) {
      applySession(data.session);
    }
    await loadSessions();
    if (state.data && state.data.session && state.data.session.id === id) {
      await loadState();
    }
  } catch (error) {
    showError(error.message);
  }
}

/* ---------------- tag pills & pool ---------------- */

function getAllExistingTags() {
  const set = new Set(["待办", "归档"]);
  for (const s of state.allSessions || []) {
    for (const t of s.tags || []) {
      if (t && t.trim()) set.add(t.trim());
    }
  }
  return Array.from(set);
}

function renderTagPills() {
  if (!els.tagPillsList) return;
  els.tagPillsList.innerHTML = "";
  for (const tag of state.modalTags) {
    const pill = document.createElement("span");
    pill.className = "tag-pill";
    if (tag === "待办") pill.classList.add("tag-todo");
    else if (tag === "归档") pill.classList.add("tag-archive");

    const label = document.createElement("span");
    label.textContent = tag;

    const removeBtn = document.createElement("button");
    removeBtn.type = "button";
    removeBtn.className = "tag-pill-remove";
    removeBtn.textContent = "×";
    removeBtn.title = `移除「${tag}」`;
    removeBtn.addEventListener("click", (e) => {
      e.stopPropagation();
      removeModalTag(tag);
    });

    pill.append(label, removeBtn);
    els.tagPillsList.append(pill);
  }
}

function renderTagPool() {
  if (!els.tagPoolChips) return;
  els.tagPoolChips.innerHTML = "";
  const allTags = getAllExistingTags();
  for (const tag of allTags) {
    const isSelected = state.modalTags.includes(tag);
    const chip = document.createElement("button");
    chip.type = "button";
    chip.className = "pool-chip" + (isSelected ? " active" : "");
    if (tag === "待办") chip.classList.add("chip-todo");
    else if (tag === "归档") chip.classList.add("chip-archive");
    chip.textContent = (isSelected ? "✓ " : "+ ") + tag;
    chip.title = isSelected ? `点击取消「${tag}」` : `点击添加「${tag}」`;
    chip.addEventListener("click", (e) => {
      e.preventDefault();
      if (isSelected) {
        removeModalTag(tag);
      } else {
        addModalTag(tag);
      }
    });
    els.tagPoolChips.append(chip);
  }
}

function addModalTag(rawTag) {
  if (!rawTag) return;
  const parts = rawTag.split(/[,，]/).map((t) => t.trim()).filter(Boolean);
  let changed = false;
  for (const part of parts) {
    if (!state.modalTags.includes(part)) {
      state.modalTags.push(part);
      changed = true;
    }
  }
  if (changed) {
    renderTagPills();
    renderTagPool();
  }
}

function removeModalTag(tag) {
  const next = state.modalTags.filter((t) => t !== tag);
  if (next.length !== state.modalTags.length) {
    state.modalTags = next;
    renderTagPills();
    renderTagPool();
  }
}
  }
}

function openSessionModal(sessionID) {
  const item = state.allSessions.find((s) => s.id === sessionID);
  if (!item) return;
  state.editingSessionID = sessionID;
  els.sessionModalTitle.textContent = `编辑会话 [${sessionID}]`;
  els.sessTitle.value = item.title || "";
  els.sessPinned.checked = !!item.pinned;
  state.modalTags = (item.tags || []).slice();
  renderTagPills();
  renderTagPool();
  if (els.tagPillInput) {
    els.tagPillInput.value = "";
  }
  els.sessDueDate.value = item.due_date || "";
  els.sessionModal.showModal();
}

async function saveSessionModal(event) {
  event.preventDefault();
  const sessionID = state.editingSessionID;
  if (!sessionID) return;

  if (els.tagPillInput && els.tagPillInput.value.trim()) {
    addModalTag(els.tagPillInput.value.trim());
    els.tagPillInput.value = "";
  }

  const title = els.sessTitle.value.trim();
  const pinned = els.sessPinned.checked;
  const rawTags = state.modalTags.slice();
  const dueDate = els.sessDueDate.value.trim();

  try {
    await api("/api/sessions/meta", {
      method: "POST",
      body: {
        id: sessionID,
        title,
        pinned,
        tags: rawTags,
        due_date: dueDate,
      },
    });
    els.sessionModal.close();
    state.editingSessionID = null;
    if (state.data && state.data.session && state.data.session.id === sessionID) {
      state.data.session.title = title;
      state.data.session.pinned = pinned;
      state.data.session.tags = rawTags;
      state.data.session.due_date = dueDate;
      await loadState();
    }
    await loadSessions();
  } catch (error) {
    showError(error.message);
  }
}

async function deleteSession(sessionID) {
  if (!window.confirm(`确定删除会话 ${sessionID}？此操作将永久删除对应本地文件。`)) {
    return;
  }
  try {
    await api("/api/sessions/delete", {
      method: "POST",
      body: { id: sessionID },
    });
    await loadState();
    await loadSessions();
  } catch (error) {
    showError(error.message);
  }
}

/* ---------------- actions ---------------- */

async function send() {
  if (state.streaming) return;
  const input = els.input.value.trim();
  const images = state.staged.map((image) => image.path);
  if (!input && !images.length) return;
  if (!input && images.length) {
    showError("请输入消息文本（图片需要随文本一起发送）");
    return;
  }

  const branch = state.activeBranch;
  state.temp = {
    branch,
    user: { role: "user", content: input, images: state.staged.slice() },
    reasoning: "",
    answer: "",
    error: "",
  };
  state.streaming = true;
  state.staged = [];
  els.input.value = "";
  autoGrow();
  hideError();
  renderTabs();
  renderTranscript();
  renderComposer();

  const controller = new AbortController();
  state.abort = controller;

  try {
    const res = await fetch("/api/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ branch_id: branch, input, images }),
      signal: controller.signal,
    });
    if (!res.ok) {
      const text = await res.text();
      let message = text;
      try {
        const parsed = JSON.parse(text);
        if (parsed && parsed.error) message = parsed.error.message;
      } catch {
        /* 保留原始文本 */
      }
      throw new Error(message || res.statusText);
    }
    await readSSE(res, (event, data) => {
      if (!state.temp) return;
      if (event === "delta") {
        if (data.kind === "reasoning") state.temp.reasoning += data.text;
        else state.temp.answer += data.text;
        updateTemp();
      } else if (event === "error") {
        state.temp.error = data.message || "请求失败";
        updateTemp();
      } else if (event === "done") {
        if (state.data) state.data.session = data.session;
      }
    });
  } catch (error) {
    if (error.name === "AbortError") {
      if (state.temp) state.temp.error = "已取消";
      updateTemp();
    } else {
      showError(error.message);
    }
  } finally {
    state.streaming = false;
    state.abort = null;
    state.temp = null;
    try {
      await loadState();
      await loadSessions();
    } catch (error) {
      showError(error.message);
    }
    els.input.focus();
  }
}

async function cancel() {
  try {
    await api("/api/chat/cancel", { method: "POST", body: {} });
  } catch (error) {
    showError(error.message);
  }
  if (state.abort) state.abort.abort();
}

async function switchBranch(id) {
  if (!id || id === state.activeBranch) return;
  try {
    const data = await api("/api/branch/switch", { method: "POST", body: { branch_id: id } });
    state.activeBranch = id;
    applySession(data.session);
    renderAll();
  } catch (error) {
    showError(error.message);
  }
}

async function forkAt(index) {
  try {
    const data = await api("/api/branch/fork", {
      method: "POST",
      body: { branch_id: state.activeBranch, message_index: index },
    });
    state.activeBranch = data.branch.id;
    applySession(data.session);
    renderAll();
    els.input.focus();
  } catch (error) {
    showError(error.message);
  }
}

function forkLast() {
  const branch = activeBranch();
  const messages = (branch && branch.messages) || [];
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    if (messages[index].role === "assistant" && messages[index].content) {
      forkAt(index);
      return;
    }
  }
  showError("当前分支没有已完成的 AI 消息，无法创建分支");
}

async function renameBranch(id) {
  const next = window.prompt("新的分支名（字母、数字、下划线、减号、点号）", id);
  if (!next || next === id) return;
  try {
    const data = await api("/api/branch/rename", {
      method: "POST",
      body: { branch_id: id, new_name: next },
    });
    if (state.activeBranch === id) state.activeBranch = next;
    applySession(data.session);
    renderAll();
  } catch (error) {
    showError(error.message);
  }
}

async function deleteBranch(id) {
  const branch = state.data && state.data.session.branches.find((item) => item.id === id);
  if (!branch) return;
  if (branch.id === state.data.session.root_branch_id) {
    showError("无法删除根分支");
    return;
  }
  if (!window.confirm(`删除分支 ${id}？该分支的独立消息会一并删除。`)) return;
  try {
    const data = await api("/api/branch/delete", { method: "POST", body: { branch_id: id } });
    if (state.activeBranch === id) state.activeBranch = data.session.active_branch_id;
    applySession(data.session);
    renderAll();
  } catch (error) {
    showError(error.message);
  }
}

async function newSession() {
  try {
    await api("/api/sessions/new", { method: "POST", body: {} });
    state.activeBranch = "";
    state.expanded.clear();
    await loadState();
    await loadSessions();
    els.input.focus();
  } catch (error) {
    showError(error.message);
  }
}

async function resumeSession(id) {
  try {
    await api("/api/sessions/resume", { method: "POST", body: { id } });
    state.activeBranch = "";
    state.expanded.clear();
    await loadState();
    await loadSessions();
  } catch (error) {
    showError(error.message);
  }
}

/* ---------------- images ---------------- */

function readFileBase64(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result || "");
      resolve(result.slice(result.indexOf(",") + 1));
    };
    reader.onerror = () => reject(new Error("读取图片失败"));
    reader.readAsDataURL(file);
  });
}

async function uploadFiles(files) {
  const allowed = ["image/png", "image/jpeg", "image/webp", "image/gif"];
  for (const file of files) {
    if (!allowed.includes(file.type)) {
      showError("仅支持 PNG、JPEG、WebP 或 GIF 图片");
      continue;
    }
    if (file.size > 10 * 1024 * 1024) {
      showError("单张图片不能超过 10 MiB");
      continue;
    }
    try {
      const dataBase64 = await readFileBase64(file);
      const image = await api("/api/images", {
        method: "POST",
        body: { file_name: file.name, media_type: file.type, data_base64: dataBase64 },
      });
      state.staged.push(image);
    } catch (error) {
      showError(error.message);
    }
  }
  renderAttachments();
}

/* ---------------- settings ---------------- */

function openSettings() {
  if (!state.data) return;
  const cfg = state.data.config;
  els.cfgBaseURL.value = cfg.base_url || "";
  els.cfgModel.value = cfg.model || "";
  els.cfgAPIKey.value = "";
  els.cfgNote.textContent = cfg.env_overrides && cfg.env_overrides.length
    ? `环境变量优先：${cfg.env_overrides.join("、")}`
    : `配置文件：${cfg.config_path}`;
  els.settings.showModal();
}

async function saveSettings(event) {
  event.preventDefault();
  try {
    const updated = await api("/api/config", {
      method: "PUT",
      body: {
        base_url: els.cfgBaseURL.value.trim(),
        model: els.cfgModel.value.trim(),
        api_key: els.cfgAPIKey.value.trim(),
      },
    });
    if (state.data) state.data.config = updated;
    els.settings.close();
    hideError();
    await loadState();
  } catch (error) {
    els.cfgNote.textContent = error.message;
  }
}

/* ---------------- misc ui ---------------- */

function showError(message) {
  if (!message) return;
  els.errorBar.textContent = message;
  els.errorBar.hidden = false;
}

function hideError() {
  els.errorBar.hidden = true;
  els.errorBar.textContent = "";
}

function scrollToBottom() {
  const node = els.transcript;
  const nearBottom = node.scrollHeight - node.scrollTop - node.clientHeight < 160;
  if (nearBottom) node.scrollTop = node.scrollHeight;
}

function autoGrow() {
  els.input.style.height = "auto";
  els.input.style.height = `${Math.min(els.input.scrollHeight, 220)}px`;
}

/* ---------------- markdown ---------------- */

function escapeHtml(value) {
  return String(value)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function inlineMarkdown(text) {
  const codes = [];
  let source = String(text).replace(/`([^`\n]+)`/g, (_, code) => {
    codes.push(`<code>${escapeHtml(code)}</code>`);
    return `\u0000IC${codes.length - 1}\u0000`;
  });
  let out = escapeHtml(source);
  out = out.replace(/~~([^~\n]+)~~/g, "<del>$1</del>");
  out = out.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>");
  out = out.replace(/__([^\n_]+)__/g, "<strong>$1</strong>");
  out = out.replace(/(^|[^*])\*([^*\n]+)\*/g, "$1<em>$2</em>");
  out = out.replace(/(^|[^_])_([^\n_]+)_/g, "$1<em>$2</em>");
  out = out.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+|mailto:[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noreferrer">$1</a>');
  out = out.replace(/\u0000IC(\d+)\u0000/g, (_, index) => codes[Number(index)]);
  return out.replace(/\n/g, "<br>");
}

function isTableSeparator(line) {
  const trimmed = line.trim();
  if (!trimmed.includes("-") || !trimmed.includes("|")) return false;
  const cells = trimmed.replace(/^\|/, "").replace(/\|$/, "").split("|");
  if (!cells.length) return false;
  return cells.every((c) => /^\s*:?-{2,}:?\s*$/.test(c));
}

function parseTable(lines, startIdx) {
  const headerLine = lines[startIdx];
  const sepLine = lines[startIdx + 1];

  const parseCells = (str) => {
    let s = str.trim();
    if (s.startsWith("|")) s = s.slice(1);
    if (s.endsWith("|")) s = s.slice(0, -1);
    return s.split("|").map((c) => c.trim());
  };

  const headerCells = parseCells(headerLine);
  const sepCells = parseCells(sepLine);

  const aligns = sepCells.map((c) => {
    const left = c.startsWith(":");
    const right = c.endsWith(":");
    if (left && right) return "center";
    if (right) return "right";
    return "left";
  });

  let thead = "<thead><tr>";
  for (let i = 0; i < headerCells.length; i++) {
    const align = aligns[i] || "left";
    thead += `<th style="text-align:${align}">${inlineMarkdown(headerCells[i])}</th>`;
  }
  thead += "</tr></thead>";

  const rows = [];
  let curr = startIdx + 2;
  while (curr < lines.length) {
    const line = lines[curr];
    if (!line.trim() || !line.includes("|") || line.trim().startsWith("#") || line.trim().startsWith("```")) {
      break;
    }
    const cells = parseCells(line);
    let rowHtml = "<tr>";
    for (let i = 0; i < headerCells.length; i++) {
      const val = cells[i] !== undefined ? cells[i] : "";
      const align = aligns[i] || "left";
      rowHtml += `<td style="text-align:${align}">${inlineMarkdown(val)}</td>`;
    }
    rowHtml += "</tr>";
    rows.push(rowHtml);
    curr++;
  }

  const html = `<div class="table-wrap"><table>${thead}<tbody>${rows.join("")}</tbody></table></div>`;
  return { html, nextIdx: curr };
}

function renderMarkdown(source) {
  const blocks = [];
  let text = String(source).replace(/```([^\n]*)\n([\s\S]*?)```/g, (_, lang, code) => {
    const cleanCode = code.replace(/\n$/, "");
    const l = (lang || "").trim();
    blocks.push(`
      <div class="code-block">
        <div class="code-head">
          <span class="code-lang">${escapeHtml(l || "text")}</span>
          <button class="copy-code-btn" type="button" data-code="${encodeURIComponent(cleanCode)}">复制</button>
        </div>
        <pre><code>${escapeHtml(cleanCode)}</code></pre>
      </div>
    `.trim());
    return `\n\n\u0000BLOCK${blocks.length - 1}\u0000\n\n`;
  });

  const lines = text.split("\n");
  const out = [];
  let list = null;
  let paragraph = [];

  const flushParagraph = () => {
    if (paragraph.length) {
      out.push(`<p>${inlineMarkdown(paragraph.join("\n"))}</p>`);
      paragraph = [];
    }
  };
  const closeList = () => {
    if (list) {
      out.push(`</${list}>`);
      list = null;
    }
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const blockMatch = line.trim().match(/^\u0000BLOCK(\d+)\u0000$/);
    if (blockMatch) {
      flushParagraph();
      closeList();
      out.push(blocks[Number(blockMatch[1])]);
      continue;
    }
    if (!line.trim()) {
      flushParagraph();
      closeList();
      continue;
    }

    if (line.includes("|") && i + 1 < lines.length && isTableSeparator(lines[i + 1])) {
      flushParagraph();
      closeList();
      const { html, nextIdx } = parseTable(lines, i);
      out.push(html);
      i = nextIdx - 1;
      continue;
    }

    let match;
    if ((match = line.match(/^(#{1,6})\s+(.*)$/))) {
      flushParagraph();
      closeList();
      const level = match[1].length;
      out.push(`<h${level}>${inlineMarkdown(match[2])}</h${level}>`);
      continue;
    }
    if (/^\s*([-*_])\1{2,}\s*$/.test(line)) {
      flushParagraph();
      closeList();
      out.push("<hr>");
      continue;
    }
    if ((match = line.match(/^>\s?(.*)$/))) {
      flushParagraph();
      closeList();
      out.push(`<blockquote>${inlineMarkdown(match[1])}</blockquote>`);
      continue;
    }
    if ((match = line.match(/^\s*([-*+])\s+\[([ xX])\]\s+(.*)$/))) {
      flushParagraph();
      if (list !== "ul") {
        closeList();
        out.push('<ul class="task-list">');
        list = "ul";
      }
      const checked = match[2].toLowerCase() === "x";
      out.push(`<li class="task-item"><input type="checkbox" ${checked ? "checked" : ""} disabled> <span>${inlineMarkdown(match[3])}</span></li>`);
      continue;
    }
    if ((match = line.match(/^\s*([-*+])\s+(.*)$/))) {
      flushParagraph();
      if (list !== "ul") {
        closeList();
        out.push("<ul>");
        list = "ul";
      }
      out.push(`<li>${inlineMarkdown(match[2])}</li>`);
      continue;
    }
    if ((match = line.match(/^\s*(\d+)\.\s+(.*)$/))) {
      flushParagraph();
      if (list !== "ol") {
        closeList();
        out.push("<ol>");
        list = "ol";
      }
      out.push(`<li>${inlineMarkdown(match[2])}</li>`);
      continue;
    }
    if (list) closeList();
    paragraph.push(line);
  }
  flushParagraph();
  closeList();
  return out.join("\n");
}

/* ---------------- events ---------------- */

els.send.addEventListener("click", send);
els.cancel.addEventListener("click", cancel);
els.pickImage.addEventListener("click", () => els.fileInput.click());
els.fileInput.addEventListener("change", () => {
  const files = Array.from(els.fileInput.files || []);
  els.fileInput.value = "";
  if (files.length) uploadFiles(files);
});
$("new-session").addEventListener("click", newSession);
$("open-settings").addEventListener("click", openSettings);
$("settings-cancel").addEventListener("click", () => els.settings.close());
els.settingsForm.addEventListener("submit", saveSettings);

// View mode switch
els.viewModeTree.addEventListener("click", () => {
  state.sessionViewMode = "tree";
  localStorage.setItem("typeai_view_mode", "tree");
  els.viewModeTree.classList.add("active");
  els.viewModeList.classList.remove("active");
  renderSessions();
});
els.viewModeList.addEventListener("click", () => {
  state.sessionViewMode = "list";
  localStorage.setItem("typeai_view_mode", "list");
  els.viewModeList.classList.add("active");
  els.viewModeTree.classList.remove("active");
  renderSessions();
});

// Session modal events
els.sessionModalForm.addEventListener("submit", saveSessionModal);
els.sessionModalCancel.addEventListener("click", () => els.sessionModal.close());
els.sessClearDue.addEventListener("click", () => {
  els.sessDueDate.value = "";
});
if (els.tagPillsBox && els.tagPillInput) {
  els.tagPillsBox.addEventListener("click", () => {
    els.tagPillInput.focus();
  });

  els.tagPillInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter" || e.key === "," || e.key === "，") {
      e.preventDefault();
      const val = els.tagPillInput.value.trim();
      if (val) {
        addModalTag(val);
        els.tagPillInput.value = "";
      }
    } else if (e.key === "Backspace" && els.tagPillInput.value === "") {
      if (state.modalTags.length > 0) {
        const last = state.modalTags[state.modalTags.length - 1];
        removeModalTag(last);
      }
    }
  });

  els.tagPillInput.addEventListener("blur", () => {
    const val = els.tagPillInput.value.trim();
    if (val) {
      addModalTag(val);
      els.tagPillInput.value = "";
    }
  });
}

// Close context menu on external click
document.addEventListener("click", (e) => {
  if (!els.contextMenu.hidden && !els.contextMenu.contains(e.target)) {
    hideContextMenu();
  }
});

// Copy code buttons delegation
document.addEventListener("click", (e) => {
  const btn = e.target.closest(".copy-code-btn");
  if (!btn) return;
  const code = decodeURIComponent(btn.dataset.code || "");
  if (!code) return;
  navigator.clipboard.writeText(code).then(() => {
    const originalText = btn.textContent;
    btn.textContent = "已复制!";
    setTimeout(() => {
      btn.textContent = originalText;
    }, 1500);
  }).catch(() => {
    btn.textContent = "复制失败";
  });
});

els.input.addEventListener("input", autoGrow);
els.input.addEventListener("keydown", (event) => {
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
    event.preventDefault();
    send();
  }
});

document.addEventListener("paste", (event) => {
  const items = Array.from((event.clipboardData && event.clipboardData.items) || []);
  const files = items
    .filter((item) => item.kind === "file" && item.type.startsWith("image/"))
    .map((item) => item.getAsFile())
    .filter(Boolean);
  if (files.length) {
    event.preventDefault();
    uploadFiles(files);
  }
});

document.addEventListener("dragover", (event) => {
  if (event.dataTransfer && Array.from(event.dataTransfer.types || []).includes("Files")) {
    event.preventDefault();
  }
});

document.addEventListener("drop", (event) => {
  if (!event.dataTransfer || !event.dataTransfer.files.length) return;
  event.preventDefault();
  uploadFiles(Array.from(event.dataTransfer.files));
});

/* ---------------- boot ---------------- */

(async function boot() {
  if (state.sessionViewMode === "list") {
    els.viewModeList.classList.add("active");
    els.viewModeTree.classList.remove("active");
  } else {
    els.viewModeTree.classList.add("active");
    els.viewModeList.classList.remove("active");
  }

  try {
    await loadState();
    await loadSessions();
  } catch (error) {
    showError(error.message);
  }
  els.input.focus();
})();
