// main.ts：termhub 工作台——目录选择、会话 tab、启动项、状态面板与 AI 诊断。
import { createTerminalTab, type TabHandle } from "./term";
import { pickDirectory } from "./picker";

interface SessionInfo {
  id: string;
  status: "running" | "exited";
  started_at: string;
  exited_at?: string;
  exit_code?: number;
  work_dir?: string;
  cwd?: string;
  source?: string;
  memory_bytes?: number;
  commands?: string[];
  last_output?: string;
}

interface Entry {
  id: string;
  name: string;
  work_dir: string;
  command: string;
}

interface Group {
  id: string;
  name: string;
  entry_ids: string[];
}

interface Config {
  entries: Entry[];
  groups: Group[];
}

interface AgentReply {
  answer: string;
  conversation_id: string;
}

const tabs = new Map<string, TabHandle>();
let liveSessions: SessionInfo[] = [];
let exitedSessions: SessionInfo[] = [];
let exitedSignature = "";
let config: Config = { entries: [], groups: [] };
let editingEntryId: string | null = null;
let editingGroupId: string | null = null;
let agentSessionId: string | null = null;
let agentConversationId = "";
let currentCommands: string[] = [];

const sessionList = document.getElementById("session-list")!;
const panes = document.getElementById("panes")!;
const emptyHint = document.getElementById("empty-hint")!;
const newButton = document.getElementById("new-terminal") as HTMLButtonElement;

const startupModal = document.getElementById("startup-modal") as HTMLDivElement;
const sessionsModal = document.getElementById("sessions-modal") as HTMLDivElement;
const commandsModal = document.getElementById("commands-modal") as HTMLDivElement;
const agentModal = document.getElementById("agent-modal") as HTMLDivElement;

function byId<T extends HTMLElement>(id: string): T {
  return document.getElementById(id) as T;
}

async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const resp = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const json = await resp.json();
  if (!json.ok) throw new Error(json.error || `请求失败: ${resp.status}`);
  return json.data as T;
}

function dirBase(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : path;
}

function formatBytes(bytes: number | undefined): string {
  if (!bytes) return "0 MB";
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatUptime(startedAt: string): string {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = seconds % 60;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m ${rest}s`;
  return `${rest}s`;
}

async function newTerminal(): Promise<void> {
  const workDir = await pickDirectory();
  if (!workDir) return;

  newButton.disabled = true;
  try {
    const data = await api<{ id: string }>("POST", "/api/v1/sessions", { work_dir: workDir });
    openTab(data.id, workDir);
    void refreshAllSessions();
  } catch (err) {
    alert(`拉起终端失败: ${err instanceof Error ? err.message : String(err)}`);
  } finally {
    newButton.disabled = false;
  }
}

/** 打开并激活一个 tab；已有会话复用。 */
function openTab(id: string, workDir: string, activate = true): void {
  if (tabs.has(id)) {
    if (activate) activateTab(id);
    return;
  }

  const tabEl = document.createElement("div");
  tabEl.className = "tab";
  tabEl.dataset.id = id;
  tabEl.title = workDir;
  tabEl.innerHTML = `<span class="tab-status"></span><span class="tab-body"><span class="tab-label"></span><span class="tab-sub"></span></span><span class="tab-close" title="关闭">×</span>`;
  (tabEl.querySelector(".tab-label") as HTMLElement).textContent = dirBase(workDir) || "PowerShell";
  (tabEl.querySelector(".tab-sub") as HTMLElement).textContent = workDir || id;
  tabEl.addEventListener("click", () => activateTab(id));

  const paneEl = document.createElement("div");
  paneEl.className = "pane";
  paneEl.dataset.id = id;

  sessionList.appendChild(tabEl);
  panes.appendChild(paneEl);

  const handle = createTerminalTab(id, paneEl, (exitedId) => {
    removeTab(exitedId);
    void refreshAllSessions();
  });
  tabs.set(id, handle);

  (tabEl.querySelector(".tab-close") as HTMLElement).addEventListener("click", (ev) => {
    ev.stopPropagation();
    void closeTab(id);
  });

  if (activate) activateTab(id);
  else emptyHint.hidden = true;
}

function activateTab(id: string): void {
  for (const el of sessionList.children) {
    (el as HTMLElement).classList.toggle("active", (el as HTMLElement).dataset.id === id);
  }
  for (const el of panes.children) {
    (el as HTMLElement).classList.toggle("hidden", (el as HTMLElement).dataset.id !== id);
  }
  emptyHint.hidden = tabs.has(id);
  const handle = tabs.get(id);
  if (handle) handle.term.focus();
}

async function closeTab(id: string): Promise<void> {
  if (!confirm("关闭该终端将终止其 shell 及全部子进程，确定？")) return;
  try {
    await api("DELETE", `/api/v1/sessions/${id}`);
  } catch (err) {
    alert(`关闭失败: ${err instanceof Error ? err.message : String(err)}`);
    return;
  }
  const handle = tabs.get(id);
  if (handle) handle.dispose();
  removeTab(id);
  void refreshAllSessions();
}

function removeTab(id: string): void {
  const tabEl = sessionList.querySelector(`.tab:not(.tab-exited)[data-id="${id}"]`);
  const wasActive = tabEl?.classList.contains("active") ?? false;
  tabs.delete(id);
  tabEl?.remove();
  panes.querySelector(`[data-id="${id}"]`)?.remove();

  const next = tabs.keys().next();
  if (next.done) {
    emptyHint.hidden = false;
  } else if (wasActive) {
    activateTab(next.value);
  }
}

/** 与后端运行中会话对齐：补齐缺失 tab，移除已结束会话。 */
async function refreshLiveTabs(): Promise<void> {
  try {
    const live = await api<SessionInfo[]>("GET", "/api/v1/sessions");
    liveSessions = live;
    const ids = new Set(live.map((s) => s.id));
    for (const session of live) {
      openTab(session.id, session.cwd || session.work_dir || "", false);
    }
    for (const id of [...tabs.keys()]) {
      if (!ids.has(id)) removeTab(id);
    }
    if (!sessionList.querySelector(".tab.active") && live.length > 0) {
      activateTab(live[0].id);
    }
    if (live.length === 0 && tabs.size === 0) emptyHint.hidden = false;
  } catch {
    // 后端尚未就绪时保持空工作台。
  }
}

async function refreshAllSessions(): Promise<void> {
  try {
    const [live, history] = await Promise.all([
      api<SessionInfo[]>("GET", "/api/v1/sessions"),
      api<SessionInfo[]>("GET", "/api/v1/sessions/history"),
    ]);
    liveSessions = live;
    exitedSessions = history;
    renderExitedSidebar();
    if (!sessionsModal.hidden) renderSessionCards();
  } catch {
    // 面板轮询失败时静默，等待下一次刷新。
  }
}

/** 已退出会话也进入左侧列表，重启后用户仍能看到并删除。 */
function renderExitedSidebar(): void {
  const signature = exitedSessions
    .map((session) => `${session.id}:${(session.commands ?? []).length}:${session.exited_at ?? ""}`)
    .join("|");
  if (signature === exitedSignature) return;
  exitedSignature = signature;

  sessionList.querySelectorAll(".tab-exited, .session-section").forEach((el) => el.remove());
  if (exitedSessions.length === 0) return;

  const header = document.createElement("div");
  header.className = "session-section";
  header.textContent = "已退出";
  sessionList.append(header);

  for (const session of exitedSessions) {
    const tabEl = document.createElement("div");
    tabEl.className = "tab tab-exited";
    tabEl.dataset.id = session.id;
    tabEl.title = `${session.cwd || session.work_dir || ""}
已退出${session.exit_code !== undefined ? ` · code ${session.exit_code}` : ""}${session.exited_at ? ` · ${new Date(session.exited_at).toLocaleString()}` : ""}`;
    tabEl.innerHTML = `<span class="tab-status tab-status-exited"></span><span class="tab-body"><span class="tab-label"></span><span class="tab-sub"></span></span><span class="tab-close" title="删除记录">×</span>`;
    (tabEl.querySelector(".tab-label") as HTMLElement).textContent =
      dirBase(session.cwd || session.work_dir || "") || session.id;
    (tabEl.querySelector(".tab-sub") as HTMLElement).textContent =
      `已退出 · ${(session.commands ?? []).length} 条命令`;
    tabEl.addEventListener("click", () => void openSessionsFor(session.id));
    (tabEl.querySelector(".tab-close") as HTMLElement).addEventListener("click", (ev) => {
      ev.stopPropagation();
      void deleteExitedSession(session.id);
    });
    sessionList.append(tabEl);
  }
}

async function openSessionsFor(id: string): Promise<void> {
  sessionsModal.hidden = false;
  await refreshAllSessions();
  const card = byId("session-cards").querySelector<HTMLElement>(`[data-session-id="${id}"]`);
  if (!card) return;
  card.scrollIntoView({ block: "nearest" });
  card.classList.add("session-card-target");
  setTimeout(() => card.classList.remove("session-card-target"), 1500);
}

async function deleteExitedSession(id: string): Promise<void> {
  if (!confirm("删除这条已退出会话记录？")) return;
  try {
    await api("DELETE", `/api/v1/sessions/history/${id}`);
    await refreshAllSessions();
  } catch (err) {
    alert(`删除失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

/* ---------------- 会话状态面板 ---------------- */

function openSessions(): void {
  sessionsModal.hidden = false;
  void refreshAllSessions();
}

function renderSessionCards(): void {
  const container = byId<HTMLDivElement>("session-cards");
  container.replaceChildren();
  const all = [...liveSessions, ...exitedSessions];
  if (all.length === 0) {
    const empty = document.createElement("div");
    empty.className = "dir-empty";
    empty.textContent = "还没有会话";
    container.append(empty);
    return;
  }

  for (const session of all) {
    const running = session.status === "running";
    const card = document.createElement("div");
    card.className = "session-card";
    card.dataset.sessionId = session.id;

    const head = document.createElement("div");
    head.className = "session-card-head";
    const dot = document.createElement("span");
    dot.className = `status-dot ${running ? "status-running" : "status-exited"}`;
    const name = document.createElement("span");
    name.className = "session-card-name";
    name.textContent = dirBase(session.cwd || session.work_dir || "") || session.id;
    head.append(dot, name);

    const meta = document.createElement("div");
    meta.className = "session-meta";
    const lines = [
      running ? `运行中 · ${formatUptime(session.started_at)}` : `已退出 · code ${session.exit_code ?? 0}`,
      `目录: ${session.cwd || session.work_dir || "-"}`,
      running ? `内存: ${formatBytes(session.memory_bytes)}` : `退出: ${session.exited_at ? new Date(session.exited_at).toLocaleString() : "-"}`,
    ];
    if (session.source) lines.push(`来源: ${session.source}`);
    meta.textContent = lines.join("\n");

    const cmdBox = document.createElement("div");
    cmdBox.className = "session-commands";
    const commands = session.commands ?? [];
    cmdBox.textContent = commands.length > 0
      ? commands.slice(-3).map((cmd) => `$ ${cmd}`).join("\n")
      : "（暂无命令记录）";

    const actions = document.createElement("div");
    actions.className = "session-card-actions";
    if (running) {
      actions.append(
        miniButton("聚焦", () => {
          if (!tabs.has(session.id)) openTab(session.id, session.cwd || session.work_dir || "");
          else activateTab(session.id);
          sessionsModal.hidden = true;
        }),
      );
    }
    actions.append(
      miniButton("命令", () => openCommands(session)),
      miniButton("诊断", () => openAgent(session)),
    );
    if (running) {
      actions.append(miniButton("关闭", () => void terminateFromPanel(session.id), "danger"));
    } else {
      actions.append(
        miniButton("重开", () => void reopenSession(session)),
        miniButton("删除", () => void deleteExitedSession(session.id), "danger"),
      );
    }

    if (running) {
      card.style.cursor = "pointer";
      card.addEventListener("click", (ev) => {
        if ((ev.target as HTMLElement).closest("button")) return;
        if (!tabs.has(session.id)) openTab(session.id, session.cwd || session.work_dir || "");
        else activateTab(session.id);
        sessionsModal.hidden = true;
      });
    }

    card.append(head, meta, cmdBox, actions);
    container.append(card);
  }
}

function miniButton(text: string, onClick: () => void, extra = ""): HTMLButtonElement {
  const button = document.createElement("button");
  button.className = `mini-btn ${extra}`.trim();
  button.textContent = text;
  button.addEventListener("click", onClick);
  return button;
}

async function terminateFromPanel(id: string): Promise<void> {
  if (!confirm("关闭该终端将终止其 shell 及全部子进程，确定？")) return;
  try {
    await api("DELETE", `/api/v1/sessions/${id}`);
  } catch (err) {
    alert(`关闭失败: ${err instanceof Error ? err.message : String(err)}`);
    return;
  }
  const handle = tabs.get(id);
  if (handle) handle.dispose();
  removeTab(id);
  await refreshAllSessions();
  await refreshLiveTabs();
}

async function reopenSession(session: SessionInfo): Promise<void> {
  const workDir = session.cwd || session.work_dir || "";
  try {
    const data = await api<{ id: string }>("POST", "/api/v1/sessions", {
      work_dir: workDir,
      source: "reopen",
    });
    openTab(data.id, workDir);
    sessionsModal.hidden = true;
    await refreshAllSessions();
  } catch (err) {
    alert(`重开失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

/* ---------------- 命令清单 ---------------- */

function openCommands(session: SessionInfo): void {
  byId("commands-title").textContent = `命令清单 · ${dirBase(session.cwd || session.work_dir || "") || session.id}`;
  currentCommands = session.commands ?? [];
  byId<HTMLInputElement>("commands-filter").value = "";
  renderCommandList("");
  commandsModal.hidden = false;
}

function renderCommandList(filter: string): void {
  const list = byId<HTMLOListElement>("command-list");
  list.replaceChildren();
  const keyword = filter.trim().toLowerCase();
  const commands = keyword
    ? currentCommands.filter((cmd) => cmd.toLowerCase().includes(keyword))
    : currentCommands;
  if (commands.length === 0) {
    const empty = document.createElement("li");
    empty.textContent = keyword ? "没有匹配的命令" : "（暂无命令记录）";
    list.append(empty);
    return;
  }
  for (const cmd of commands) {
    const item = document.createElement("li");
    item.textContent = cmd;
    list.append(item);
  }
}

async function copyCommands(): Promise<void> {
  const text = currentCommands.join("\n");
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const area = document.createElement("textarea");
    area.value = text;
    document.body.append(area);
    area.select();
    document.execCommand("copy");
    area.remove();
  }
}

/* ---------------- AI 诊断 ---------------- */

function openAgent(session: SessionInfo): void {
  agentSessionId = session.id;
  agentConversationId = "";
  byId("agent-answer").textContent = "";
  byId<HTMLTextAreaElement>("agent-question").value = "";
  byId<HTMLInputElement>("agent-followup").value = "";
  const context = byId("agent-context");
  const commands = (session.commands ?? []).slice(-5);
  context.textContent = [
    `目录: ${session.cwd || session.work_dir || "-"}`,
    `会话: ${session.id} (${session.status})`,
    commands.length ? `最近命令:\n${commands.map((cmd) => `$ ${cmd}`).join("\n")}` : "最近命令:（无）",
  ].join("\n");
  agentModal.hidden = false;
  byId<HTMLTextAreaElement>("agent-question").focus();
}

async function askAgent(question: string): Promise<void> {
  if (!agentSessionId) return;
  const answerEl = byId<HTMLPreElement>("agent-answer");
  const askButton = byId<HTMLButtonElement>("agent-ask");
  const sendButton = byId<HTMLButtonElement>("agent-send");
  askButton.disabled = true;
  sendButton.disabled = true;
  answerEl.textContent = "agent 分析中…";
  try {
    const reply = await api<AgentReply>("POST", `/api/v1/sessions/${agentSessionId}/agent`, {
      question,
      conversation_id: agentConversationId,
    });
    agentConversationId = reply.conversation_id || agentConversationId;
    answerEl.textContent = reply.answer;
  } catch (err) {
    answerEl.textContent = `调用失败: ${err instanceof Error ? err.message : String(err)}`;
  } finally {
    askButton.disabled = false;
    sendButton.disabled = false;
  }
}

/* ---------------- 启动项与启动组 ---------------- */

async function openStartups(): Promise<void> {
  startupModal.hidden = false;
  try {
    config = await api<Config>("GET", "/api/v1/config");
    renderStartups();
  } catch (err) {
    alert(`读取配置失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function renderStartups(): void {
  renderEntryList();
  renderGroupList();
  renderGroupChoices();
}

function renderEntryList(): void {
  const list = byId<HTMLDivElement>("entry-list");
  list.replaceChildren();
  if (config.entries.length === 0) {
    list.append(messageItem("还没有启动项"));
    return;
  }
  for (const entry of config.entries) {
    const item = document.createElement("div");
    item.className = "config-item";
    const head = document.createElement("div");
    head.className = "config-item-head";
    const name = document.createElement("span");
    name.className = "config-item-name";
    name.textContent = entry.name;
    const actions = document.createElement("div");
    actions.className = "config-item-actions";
    actions.append(
      miniButton("启动", () => void startEntry(entry.id)),
      miniButton("编辑", () => editEntry(entry)),
      miniButton("删除", () => void deleteEntry(entry.id), "danger"),
    );
    head.append(name, actions);
    const meta = document.createElement("div");
    meta.className = "config-item-meta";
    meta.textContent = `${entry.work_dir || "(默认目录)"}${entry.command ? ` · ${entry.command}` : ""}`;
    item.append(head, meta);
    list.append(item);
  }
}

function renderGroupList(): void {
  const list = byId<HTMLDivElement>("group-list");
  list.replaceChildren();
  if (config.groups.length === 0) {
    list.append(messageItem("还没有启动组"));
    return;
  }
  for (const group of config.groups) {
    const item = document.createElement("div");
    item.className = "config-item";
    const head = document.createElement("div");
    head.className = "config-item-head";
    const name = document.createElement("span");
    name.className = "config-item-name";
    name.textContent = group.name;
    const actions = document.createElement("div");
    actions.className = "config-item-actions";
    actions.append(
      miniButton("启动", () => void startGroup(group.id)),
      miniButton("编辑", () => editGroup(group)),
      miniButton("删除", () => void deleteGroup(group.id), "danger"),
    );
    head.append(name, actions);
    const names = group.entry_ids
      .map((id) => config.entries.find((entry) => entry.id === id)?.name ?? "已删除")
      .join("、");
    const meta = document.createElement("div");
    meta.className = "config-item-meta";
    meta.textContent = names || "（空组）";
    item.append(head, meta);
    list.append(item);
  }
}

function renderGroupChoices(): void {
  const box = byId<HTMLDivElement>("group-entry-choices");
  box.replaceChildren();
  if (config.entries.length === 0) {
    box.append(messageItem("先创建启动项"));
    return;
  }
  const selected = new Set(
    config.groups.find((group) => group.id === editingGroupId)?.entry_ids ?? [],
  );
  for (const entry of config.entries) {
    const label = document.createElement("label");
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.value = entry.id;
    checkbox.checked = selected.has(entry.id);
    const text = document.createElement("span");
    text.textContent = entry.name;
    label.append(checkbox, text);
    box.append(label);
  }
}

function messageItem(text: string): HTMLDivElement {
  const div = document.createElement("div");
  div.className = "dir-empty";
  div.textContent = text;
  return div;
}

async function saveConfig(next: Config): Promise<void> {
  config = await api<Config>("PUT", "/api/v1/config", next);
  renderStartups();
}

function editEntry(entry: Entry): void {
  editingEntryId = entry.id;
  byId<HTMLInputElement>("entry-name").value = entry.name;
  byId<HTMLInputElement>("entry-dir").value = entry.work_dir;
  byId<HTMLInputElement>("entry-command").value = entry.command;
  byId<HTMLButtonElement>("entry-cancel").hidden = false;
  byId<HTMLFormElement>("entry-form").querySelector("button[type=submit]")!.textContent = "更新启动项";
}

function resetEntryForm(): void {
  editingEntryId = null;
  byId<HTMLFormElement>("entry-form").reset();
  byId<HTMLButtonElement>("entry-cancel").hidden = true;
  byId<HTMLFormElement>("entry-form").querySelector("button[type=submit]")!.textContent = "保存启动项";
}

function editGroup(group: Group): void {
  editingGroupId = group.id;
  byId<HTMLInputElement>("group-name").value = group.name;
  byId<HTMLButtonElement>("group-cancel").hidden = false;
  byId<HTMLFormElement>("group-form").querySelector("button[type=submit]")!.textContent = "更新启动组";
  renderGroupChoices();
}

function resetGroupForm(): void {
  editingGroupId = null;
  byId<HTMLFormElement>("group-form").reset();
  byId<HTMLButtonElement>("group-cancel").hidden = true;
  byId<HTMLFormElement>("group-form").querySelector("button[type=submit]")!.textContent = "保存启动组";
  renderGroupChoices();
}

async function startEntry(id: string): Promise<void> {
  try {
    await api("POST", `/api/v1/config/entries/${id}/start`);
    startupModal.hidden = true;
    await refreshLiveTabs();
    await refreshAllSessions();
  } catch (err) {
    alert(`启动失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

async function startGroup(id: string): Promise<void> {
  try {
    const result = await api<{ ids: string[]; errors: string[] }>(
      "POST",
      `/api/v1/config/groups/${id}/start`,
    );
    startupModal.hidden = true;
    await refreshLiveTabs();
    await refreshAllSessions();
    if (result.errors.length > 0) alert(`部分启动失败:\n${result.errors.join("\n")}`);
  } catch (err) {
    alert(`启动失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

async function deleteEntry(id: string): Promise<void> {
  if (!confirm("删除该启动项？")) return;
  const next: Config = {
    entries: config.entries.filter((entry) => entry.id !== id),
    groups: config.groups.map((group) => ({
      ...group,
      entry_ids: group.entry_ids.filter((entryID) => entryID !== id),
    })),
  };
  try {
    await saveConfig(next);
    resetEntryForm();
  } catch (err) {
    alert(`删除失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

async function deleteGroup(id: string): Promise<void> {
  if (!confirm("删除该启动组？")) return;
  try {
    await saveConfig({ entries: config.entries, groups: config.groups.filter((group) => group.id !== id) });
    resetGroupForm();
  } catch (err) {
    alert(`删除失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

/* ---------------- 事件绑定 ---------------- */

newButton.addEventListener("click", () => void newTerminal());
byId("open-startups").addEventListener("click", () => void openStartups());
byId("open-sessions").addEventListener("click", openSessions);

byId("startup-close").addEventListener("click", () => {
  startupModal.hidden = true;
});
byId("sessions-close").addEventListener("click", () => {
  sessionsModal.hidden = true;
});
byId("sessions-refresh").addEventListener("click", () => void refreshAllSessions());
byId("commands-close").addEventListener("click", () => {
  commandsModal.hidden = true;
});
byId<HTMLInputElement>("commands-filter").addEventListener("input", (ev) => {
  renderCommandList((ev.target as HTMLInputElement).value);
});
byId("commands-copy").addEventListener("click", () => void copyCommands());
byId("agent-close").addEventListener("click", () => {
  agentModal.hidden = true;
});

byId("entry-pick").addEventListener("click", async () => {
  const dir = await pickDirectory();
  if (dir) byId<HTMLInputElement>("entry-dir").value = dir;
});

byId<HTMLFormElement>("entry-form").addEventListener("submit", (ev) => {
  ev.preventDefault();
  void (async () => {
    const name = byId<HTMLInputElement>("entry-name").value.trim();
    const workDir = byId<HTMLInputElement>("entry-dir").value.trim();
    const command = byId<HTMLInputElement>("entry-command").value.trim();
    if (!name || !workDir) {
      alert("名称和初始目录不能为空");
      return;
    }
    const next: Config = { entries: [...config.entries], groups: config.groups };
    if (editingEntryId) {
      next.entries = next.entries.map((entry) =>
        entry.id === editingEntryId ? { ...entry, name, work_dir: workDir, command } : entry,
      );
    } else {
      next.entries.push({ id: "", name, work_dir: workDir, command });
    }
    try {
      await saveConfig(next);
      resetEntryForm();
    } catch (err) {
      alert(`保存失败: ${err instanceof Error ? err.message : String(err)}`);
    }
  })();
});

byId("entry-cancel").addEventListener("click", resetEntryForm);

byId<HTMLFormElement>("group-form").addEventListener("submit", (ev) => {
  ev.preventDefault();
  void (async () => {
    const name = byId<HTMLInputElement>("group-name").value.trim();
    if (!name) {
      alert("组名称不能为空");
      return;
    }
    const entryIDs = [...byId<HTMLDivElement>("group-entry-choices").querySelectorAll<HTMLInputElement>("input[type=checkbox]")]
      .filter((input) => input.checked)
      .map((input) => input.value);
    const next: Config = { entries: config.entries, groups: [...config.groups] };
    if (editingGroupId) {
      next.groups = next.groups.map((group) =>
        group.id === editingGroupId ? { ...group, name, entry_ids: entryIDs } : group,
      );
    } else {
      next.groups.push({ id: "", name, entry_ids: entryIDs });
    }
    try {
      await saveConfig(next);
      resetGroupForm();
    } catch (err) {
      alert(`保存失败: ${err instanceof Error ? err.message : String(err)}`);
    }
  })();
});

byId("group-cancel").addEventListener("click", resetGroupForm);

byId("agent-ask").addEventListener("click", () => {
  void askAgent(byId<HTMLTextAreaElement>("agent-question").value.trim());
});
byId("agent-send").addEventListener("click", () => {
  const input = byId<HTMLInputElement>("agent-followup");
  const question = input.value.trim();
  if (!question) return;
  input.value = "";
  void askAgent(question);
});
byId<HTMLInputElement>("agent-followup").addEventListener("keydown", (ev) => {
  if (ev.key === "Enter") {
    ev.preventDefault();
    byId<HTMLButtonElement>("agent-send").click();
  }
});

void refreshLiveTabs();
void refreshAllSessions();
setInterval(() => {
  if (!sessionsModal.hidden) void refreshAllSessions();
}, 2000);
