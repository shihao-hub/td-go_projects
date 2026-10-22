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
  els.topMeta.textContent = `${session.model} · ${session.id} · [${state.activeBranch}]`;
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
    if (fold && !state.expanded.has(key)) {
      body.textContent = content.split("\n").slice(0, 6).join("\n");
      const note = document.createElement("div");
      note.className = "clamp-note";
      note.append(`… ${content.length} 字符 · ${content.split("\n").length} 行 `);
      const button = document.createElement("button");
      button.textContent = "展开";
      button.addEventListener("click", () => {
        state.expanded.add(key);
        renderTranscript();
      });
      note.append(button);
      wrap.append(body, note);
    } else {
      body.classList.add("rendered");
      body.innerHTML = renderMarkdown(content);
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

async function loadSessions() {
  try {
    const data = await api("/api/sessions");
    els.sessionList.innerHTML = "";
    const items = (data && data.items) || [];
    if (!items.length) {
      const note = document.createElement("div");
      note.className = "empty-note";
      note.textContent = "暂无其他历史会话";
      els.sessionList.append(note);
      return;
    }
    for (const item of items) {
      const button = document.createElement("button");
      button.className = "session-item";
      const title = document.createElement("div");
      title.className = "session-title";
      title.textContent = item.first_user || item.id;
      const meta = document.createElement("div");
      meta.className = "session-meta";
      const when = new Date(item.updated_at).toLocaleString();
      meta.textContent = `${when} · ${item.message_count} 条 · ${item.model}`;
      button.append(title, meta);
      button.title = `恢复会话 ${item.id}`;
      button.addEventListener("click", () => resumeSession(item.id));
      els.sessionList.append(button);
    }
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
  out = out.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>");
  out = out.replace(/(^|[^*])\*([^*\n]+)\*/g, "$1<em>$2</em>");
  out = out.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noreferrer">$1</a>');
  out = out.replace(/\u0000IC(\d+)\u0000/g, (_, index) => codes[Number(index)]);
  return out.replace(/\n/g, "<br>");
}

function renderMarkdown(source) {
  const blocks = [];
  let text = String(source).replace(/```([^\n]*)\n([\s\S]*?)```/g, (_, _lang, code) => {
    blocks.push(`<pre><code>${escapeHtml(code.replace(/\n$/, ""))}</code></pre>`);
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

  for (const line of lines) {
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
  try {
    await loadState();
    await loadSessions();
  } catch (error) {
    showError(error.message);
  }
  els.input.focus();
})();
