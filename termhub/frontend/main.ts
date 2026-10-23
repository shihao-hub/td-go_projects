// main.ts：termhub 工作台入口——目录选择 + 垂直会话列表 + 终端 tab 管理。
import { createTerminalTab, type TabHandle } from "./term";
import { pickDirectory } from "./picker";

const tabs = new Map<string, TabHandle>();

const sessionList = document.getElementById("session-list")!;
const panes = document.getElementById("panes")!;
const emptyHint = document.getElementById("empty-hint")!;
const newButton = document.getElementById("new-terminal") as HTMLButtonElement;

/** 取路径最后一段作为会话显示名。 */
function dirBase(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : path;
}

/** 选择目录并拉起一个新终端会话。 */
async function newTerminal(): Promise<void> {
  const workDir = await pickDirectory();
  if (!workDir) return;

  newButton.disabled = true;
  try {
    const resp = await fetch("/api/v1/sessions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ work_dir: workDir }),
    });
    const json = await resp.json();
    if (!json.ok) {
      alert(`拉起终端失败: ${json.error}`);
      return;
    }
    openTab(json.data.id as string, workDir);
  } catch (err) {
    alert(`拉起终端失败: ${err instanceof Error ? err.message : String(err)}`);
  } finally {
    newButton.disabled = false;
  }
}

/** 打开并激活一个 tab；已有会话复用。 */
function openTab(id: string, workDir: string): void {
  if (tabs.has(id)) {
    activateTab(id);
    return;
  }

  // 左侧会话条目。
  const tabEl = document.createElement("div");
  tabEl.className = "tab";
  tabEl.dataset.id = id;
  tabEl.title = workDir;
  tabEl.innerHTML = `<span class="tab-status"></span><span class="tab-body"><span class="tab-label"></span><span class="tab-sub"></span></span><span class="tab-close" title="关闭">×</span>`;
  (tabEl.querySelector(".tab-label") as HTMLElement).textContent = dirBase(workDir) || "PowerShell";
  (tabEl.querySelector(".tab-sub") as HTMLElement).textContent = workDir;
  tabEl.addEventListener("click", () => activateTab(id));

  // 终端容器。
  const paneEl = document.createElement("div");
  paneEl.className = "pane";
  paneEl.dataset.id = id;

  sessionList.appendChild(tabEl);
  panes.appendChild(paneEl);

  const handle = createTerminalTab(id, paneEl, (exitedId) => {
    removeTab(exitedId);
  });
  tabs.set(id, handle);

  (tabEl.querySelector(".tab-close") as HTMLElement).addEventListener(
    "click",
    (ev) => {
      ev.stopPropagation();
      closeTab(id);
    },
  );

  activateTab(id);
}

/** 激活指定 tab。 */
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

/** 关闭 tab：断开 WS（会话进程回收语义在 T05 实装）。 */
function closeTab(id: string): void {
  const handle = tabs.get(id);
  if (handle) {
    handle.dispose();
    removeTab(id);
  }
}

/** 从 UI 移除 tab 与 pane；仅当移除的是当前 tab 时才切换激活项。 */
function removeTab(id: string): void {
  const tabEl = sessionList.querySelector(`[data-id="${id}"]`);
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

newButton.addEventListener("click", () => {
  void newTerminal();
});

/** 窗口重建后自动重连后端仍在运行的会话。 */
async function restoreSessions(): Promise<void> {
  try {
    const resp = await fetch("/api/v1/sessions");
    const json = await resp.json();
    if (!json.ok) return;
    for (const session of json.data as { id: string; work_dir?: string }[]) {
      openTab(session.id, session.work_dir ?? "");
    }
  } catch {
    // 首次启动没有会话，或后端尚未就绪：保持空工作台。
  }
}

void restoreSessions();
