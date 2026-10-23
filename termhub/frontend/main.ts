// main.ts：termhub 工作台——左侧终端记忆列表（持久化）、未启动/运行中双态、
// 带「自动重开」勾选；启动项、会话状态与 AI 诊断面板。
import { createTerminalTab, type TabHandle } from "./term";
import { pickDirectory } from "./picker";

interface SessionInfo {
  id: string;
  status: "running" | "exited";
  started_at: string;
  exit_code?: number;
  work_dir?: string;
  cwd?: string;
  source?: string;
  memory_bytes?: number;
  commands?: string[];
}

interface TerminalInfo {
  id: string;
  work_dir: string;
  source?: string;
  auto_start: boolean;
  created_at: string;
  last_session_id?: string;
  last_commands: string[];
  session: SessionInfo | null;
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

// tabs 以终端记忆（slot）为身份：一条记忆最多一个运行中的 shell tab。
const tabs = new Map<string, TabHandle>();
let terminals: TerminalInfo[] = [];
let terminalSignature = "";
let activeSlotId: string | null = null;
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

function findSlot(id: string): TerminalInfo | undefined {
  return terminals.find((slot) => slot.id === id);
}

/* ---------------- 终端记忆列表轮换 ---------------- */

/** 与后端终端记忆对齐：刷新本地状态、渲染列表、增删 tab。 */
async function refreshTerminals(): Promise<void> {
  try {
    terminals = await api<TerminalInfo[]>("GET", "/api/v1/terminals");
    renderTerminalList();
    syncTabs();
    if (!sessionsModal.hidden) renderSessionCards();
  } catch {
    // 后端尚未就绪时保持现状，等待下一次刷新。
  }
}

function renderTerminalList(): void {
  const signature = terminals
    .map((slot) => {
      const session = slot.session?.id ?? "";
      const cwd = slot.session?.cwd ?? "";
      return `${slot.id}:${session}:${slot.auto_start ? 1 : 0}:${cwd}`;
    })
    .join("|");
  if (signature === terminalSignature) return;
  terminalSignature = signature;

  sessionList.replaceChildren();
  for (const slot of terminals) {
    sessionList.append(buildSlotElement(slot));
  }
}

function buildSlotElement(slot: TerminalInfo): HTMLElement {
  const running = !!slot.session;
  const label = dirBase(slot.session?.cwd || slot.work_dir) || "PowerShell";
  const tabEl = document.createElement("div");
  tabEl.className = "tab" + (running ? "" : " tab-idle") + (slot.id === activeSlotId ? " active" : "");
  tabEl.dataset.slot = slot.id;
  tabEl.title = running
    ? `${slot.session?.cwd || slot.work_dir}\n运行中 · ${formatUptime(slot.session!.started_at)}`
    : `${slot.work_dir}\n未启动 · 点击拉起 PowerShell`;
  tabEl.innerHTML =
    `<span class="tab-status"></span>` +
    `<span class="tab-body"><span class="tab-label"></span><span class="tab-sub"></span></span>` +
    `<label class="tab-autostart" title="下次启动 termhub 时自动拉起该终端"><input type="checkbox">重开</label>` +
    `<span class="tab-close" title="删除终端">×</span>`;
  (tabEl.querySelector(".tab-label") as HTMLElement).textContent = label;
  (tabEl.querySelector(".tab-sub") as HTMLElement).textContent = running
    ? slot.session?.cwd || slot.work_dir
    : `未启动 · ${slot.work_dir}`;

  (tabEl.querySelector<HTMLInputElement>(".tab-autostart input") as HTMLInputElement).checked =
    slot.auto_start;
  (tabEl.querySelector(".tab-autostart input") as HTMLInputElement).addEventListener("change", (ev) => {
    ev.stopPropagation();
    void toggleAutoStart(slot, (ev.target as HTMLInputElement).checked);
  });
  (tabEl.querySelector(".tab-autostart") as HTMLElement).addEventListener("click", (ev) => {
    // label 内文字区域点击不激活终端。
    ev.stopPropagation();
  });

  tabEl.addEventListener("click", () => {
    if (running) {
      if (!tabs.has(slot.id)) syncTabs(); // WS 就绪前有短暂空洞，触发一次对齐。
      activateTab(slot.id);
    } else {
      void startTerminal(slot);
    }
  });
  (tabEl.querySelector(".tab-close") as HTMLElement).addEventListener("click", (ev) => {
    ev.stopPropagation();
    void deleteTerminal(slot);
  });
  return tabEl;
}

/** tab 增量对齐：运行中的记忆建 tab，结束/被删的记忆撤 tab。 */
function syncTabs(): void {
  const byId = new Map(terminals.map((slot) => [slot.id, slot] as const));
  for (const slotId of [...tabs.keys()]) {
    const slot = byId.get(slotId);
    if (!slot || !slot.session) {
      tabs.get(slotId)?.dispose();
      removeTab(slotId);
    }
  }
  for (const slot of terminals) {
    if (slot.session && !tabs.has(slot.id)) openTab(slot, false);
  }
  if (activeSlotId == null || !tabs.has(activeSlotId)) {
    const first = terminals.find((slot) => slot.session && tabs.has(slot.id));
    if (first) activateTab(first.id);
  }
  emptyHint.hidden = tabs.size > 0;
  if (!emptyHint.hidden) {
    emptyHint.textContent =
      terminals.length > 0 ? "点击左侧未启动的终端即可拉起 PowerShell" : "点击「新建终端」选择目录，或从「启动项」一键拉起";
  }
}

function openTab(slot: TerminalInfo, activate = true): void {
  const session = slot.session;
  if (!session) return;
  if (tabs.has(slot.id)) {
    if (activate) activateTab(slot.id);
    return;
  }

  // 列表 DOM 一律由 renderTerminalList 统一生成；这里只建立连接与画布。
  const paneEl = document.createElement("div");
  paneEl.className = "pane";
  paneEl.dataset.slot = slot.id;
  panes.append(paneEl);

  const handle = createTerminalTab(slot.id, session.id, paneEl, (exitedSlotId) => {
    // shell 自行退出转回未启动；同一 slot 已被新连接接管（订阅替换）时，
    // 迟到的旧连接关闭不再撤 tab，只刷新一次对齐。
    if (tabs.get(exitedSlotId) === handle) {
      removeTab(exitedSlotId);
    }
    void refreshTerminals();
  });
  tabs.set(slot.id, handle);

  if (activate) activateTab(slot.id);
  else emptyHint.hidden = true;
}

function activateTab(slotId: string): void {
  activeSlotId = slotId;
  for (const el of sessionList.children) {
    const tab = el as HTMLElement;
    if (tab.classList.contains("tab")) tab.classList.toggle("active", tab.dataset.slot === slotId);
  }
  for (const el of panes.children) {
    const pane = el as HTMLElement;
    pane.classList.toggle("hidden", pane.dataset.slot !== slotId);
  }
  emptyHint.hidden = tabs.has(slotId);
  tabs.get(slotId)?.term.focus();
}

function removeTab(slotId: string): void {
  const wasActive = activeSlotId === slotId;
  tabs.delete(slotId);
  panes.querySelector(`[data-slot="${slotId}"]`)?.remove();
  if (wasActive || (activeSlotId != null && !tabs.has(activeSlotId))) {
    const fallback = terminals.find((slot) => slot.session && tabs.has(slot.id));
    activeSlotId = null;
    if (fallback) activateTab(fallback.id);
    else emptyHint.hidden = true;
  }
  // 列表 DOM 由 renderTerminalList 管理，此处只撤 pane 与连接。
  renderTerminalList();
}

/* ---------------- 终端动作 ---------------- */

async function newTerminal(): Promise<void> {
  const workDir = await pickDirectory();
  if (!workDir) return;

  newButton.disabled = true;
  try {
    const data = await api<{ slot_id: string; session_id: string }>("POST", "/api/v1/terminals", {
      work_dir: workDir,
      source: "manual",
    });
    const slot: TerminalInfo = {
      id: data.slot_id,
      work_dir: workDir,
      source: "manual",
      auto_start: false,
      created_at: new Date().toISOString(),
      last_commands: [],
      session: {
        id: data.session_id,
        status: "running",
        started_at: new Date().toISOString(),
        exit_code: 0,
        work_dir: workDir,
        cwd: workDir,
        commands: [],
      },
    };
    terminals.push(slot);
    renderTerminalList();
    openTab(slot, true);
    void refreshTerminals();
  } catch (err) {
    alert(`拉起终端失败: ${err instanceof Error ? err.message : String(err)}`);
  } finally {
    newButton.disabled = false;
  }
}

/** 启动一条未启动的终端记忆：纯净 PowerShell，同目录，不执行任何命令。 */
async function startTerminal(slot: TerminalInfo): Promise<void> {
  try {
    const data = await api<{ slot_id: string; session_id: string }>(
      "POST",
      `/api/v1/terminals/${slot.id}/start`,
      {},
    );
    slot.session = {
      id: data.session_id,
      status: "running",
      started_at: new Date().toISOString(),
      exit_code: 0,
      work_dir: slot.work_dir,
      cwd: slot.work_dir,
      commands: [],
    };
    renderTerminalList();
    openTab(slot, true);
    void refreshTerminals();
  } catch (err) {
    alert(`启动失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

/** 删除终端记忆：运行中的先终止 shell；落盘记忆与最近一次运行随删除清理。 */
async function deleteTerminal(slot: TerminalInfo): Promise<void> {
  const running = !!slot.session;
  const message = running
    ? "关闭该终端将终止其 shell 及全部子进程，确定？"
    : "删除这条终端记录？";
  if (!confirm(message)) return;
  try {
    await api("DELETE", `/api/v1/terminals/${slot.id}`);
    if (running) {
      tabs.get(slot.id)?.dispose();
      removeTab(slot.id);
    }
    await refreshTerminals();
  } catch (err) {
    alert(`删除失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

async function toggleAutoStart(slot: TerminalInfo, checked: boolean): Promise<void> {
  slot.auto_start = checked;
  renderTerminalList();
  try {
    await api("PUT", `/api/v1/terminals/${slot.id}`, { auto_start: checked });
  } catch (err) {
    slot.auto_start = !checked;
    renderTerminalList();
    alert(`更新失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

/* ---------------- 会话状态面板 ---------------- */

function openSessions(): void {
  sessionsModal.hidden = false;
  void refreshTerminals();
}

function renderSessionCards(): void {
  const container = byId<HTMLDivElement>("session-cards");
  container.replaceChildren();
  if (terminals.length === 0) {
    const empty = document.createElement("div");
    empty.className = "dir-empty";
    empty.textContent = "还没有终端";
    container.append(empty);
    return;
  }

  for (const slot of terminals) {
    container.append(buildSessionCard(slot));
  }
}

function buildSessionCard(slot: TerminalInfo): HTMLElement {
  const running = !!slot.session;
  const card = document.createElement("div");
  card.className = "session-card";
  card.dataset.slot = slot.id;

  const head = document.createElement("div");
  head.className = "session-card-head";
  const dot = document.createElement("span");
  dot.className = `status-dot ${running ? "status-running" : "status-exited"}`;
  const name = document.createElement("span");
  name.className = "session-card-name";
  name.textContent = dirBase(slot.session?.cwd || slot.work_dir) || slot.id;
  head.append(dot, name);

  const meta = document.createElement("div");
  meta.className = "session-meta";
  const lines: string[] = [];
  if (running) {
    lines.push(
      `运行中 · ${formatUptime(slot.session!.started_at)}`,
      `目录: ${slot.session?.cwd || slot.work_dir}`,
      `内存: ${formatBytes(slot.session?.memory_bytes)}`,
    );
  } else {
    lines.push(`未启动`, `目录: ${slot.work_dir}`);
  }
  if (slot.source) lines.push(`来源: ${slot.source}`);
  meta.textContent = lines.join("\n");

  const actions = document.createElement("div");
  actions.className = "session-card-actions";
  if (running) {
    actions.append(
      miniButton("聚焦", () => {
        if (!tabs.has(slot.id)) openTab(slot, false);
        activateTab(slot.id);
        sessionsModal.hidden = true;
      }),
    );
  } else {
    actions.append(miniButton("启动", () => void startTerminal(slot)));
  }
  actions.append(
    miniButton("命令", () => openCommands(slot)),
    miniButton("诊断", () => openAgent(slot)),
    miniButton("删除", () => void deleteTerminal(slot), "danger"),
  );

  if (!running) {
    const autoWrap = document.createElement("label");
    autoWrap.className = "card-autostart";
    autoWrap.textContent = "下次启动自动重开";
    const autoBox = document.createElement("input");
    autoBox.type = "checkbox";
    autoBox.checked = slot.auto_start;
    autoBox.addEventListener("change", () => void toggleAutoStart(slot, autoBox.checked));
    autoWrap.prepend(autoBox);
    card.append(autoWrap);
  }

  card.append(head, meta, actions);
  return card;
}

function miniButton(text: string, onClick: () => void, extra = ""): HTMLButtonElement {
  const button = document.createElement("button");
  button.className = `mini-btn ${extra}`.trim();
  button.textContent = text;
  button.addEventListener("click", (ev) => {
    ev.stopPropagation();
    onClick();
  });
  return button;
}

/* ---------------- 命令清单与诊断数据源 ---------------- */

function slotCommands(slot: TerminalInfo): string[] {
  const live = slot.session?.commands ?? [];
  return live.length > 0 ? live : slot.last_commands;
}

function slotContextID(slot: TerminalInfo): string | null {
  return slot.session?.id ?? slot.last_session_id ?? null;
}

function openCommands(slot: TerminalInfo): void {
  byId("commands-title").textContent = `命令清单 · ${dirBase(slot.session?.cwd || slot.work_dir) || slot.id}`;
  currentCommands = slotCommands(slot);
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

function openAgent(slot: TerminalInfo): void {
  const contextID = slotContextID(slot);
  if (!contextID) {
    alert("该终端还没有可分析的运行记录");
    return;
  }
  agentSessionId = contextID;
  agentConversationId = "";
  byId("agent-answer").textContent = "";
  byId<HTMLTextAreaElement>("agent-question").value = "";
  byId<HTMLInputElement>("agent-followup").value = "";
  const commands = slotCommands(slot).slice(-5);
  byId("agent-context").textContent = [
    `目录: ${slot.session?.cwd || slot.work_dir || "-"}`,
    `终端: ${slot.id} (${runningLabel(slot)})`,
    commands.length ? `最近命令:\n${commands.map((cmd) => `$ ${cmd}`).join("\n")}` : "最近命令:（无）",
  ].join("\n");
  agentModal.hidden = false;
  byId<HTMLTextAreaElement>("agent-question").focus();
}

function runningLabel(slot: TerminalInfo): string {
  return slot.session ? "运行中" : "未启动";
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

/* ---------------- 启动项/组拉起（统一进终端记忆） ---------------- */

async function startEntry(id: string): Promise<void> {
  try {
    await api<{ slot_id: string; session_id: string }>(`POST`, `/api/v1/config/entries/${id}/start`);
    startupModal.hidden = true;
    await refreshTerminals();
    void activateLatestIfNeeded();
  } catch (err) {
    alert(`启动失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

async function startGroup(id: string): Promise<void> {
  try {
    const result = await api<{ errors: string[] }>("POST", `/api/v1/config/groups/${id}/start`);
    startupModal.hidden = true;
    await refreshTerminals();
    void activateLatestIfNeeded();
    if (result.errors.length > 0) alert(`部分启动失败:\n${result.errors.join("\n")}`);
  } catch (err) {
    alert(`启动失败: ${err instanceof Error ? err.message : String(err)}`);
  }
}

/** 组拉起可能没有选中窗口，把最后一个新建终端置为激活。 */
function activateLatestIfNeeded(): void {
  let latest = "";
  for (const slot of terminals) {
    if (slot.session && slot.created_at > latest) latest = slot.created_at;
  }
  if (!latest) return;
  const target = terminals.find((slot) => slot.created_at === latest);
  if (target && tabs.has(target.id)) activateTab(target.id);
}

/* ---------------- 启动项 CRUD ---------------- */

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
byId("sessions-refresh").addEventListener("click", () => void refreshTerminals());
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

void refreshTerminals();
setInterval(() => {
  if (!sessionsModal.hidden) void refreshTerminals();
}, 2000);
