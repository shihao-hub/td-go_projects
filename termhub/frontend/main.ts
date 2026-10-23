// main.ts：termhub 工作台入口——tab 管理 + 新建终端按钮。
import { createTerminalTab, type TabHandle } from "./term";

const tabs = new Map<string, TabHandle>();

const tabbar = document.getElementById("tabbar")!;
const panes = document.getElementById("panes")!;
const emptyHint = document.getElementById("empty-hint")!;

/** 拉起一个新终端会话并激活对应 tab。 */
async function newTerminal(): Promise<void> {
  const resp = await fetch("/api/v1/sessions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  });
  const json = await resp.json();
  if (!json.ok) {
    alert(`拉起终端失败: ${json.error}`);
    return;
  }
  openTab(json.data.id as string);
}

/** 打开并激活一个 tab；已有会话复用。 */
function openTab(id: string): void {
  if (tabs.has(id)) {
    activateTab(id);
    return;
  }

  // tab 头。
  const tabEl = document.createElement("div");
  tabEl.className = "tab";
  tabEl.dataset.id = id;
  tabEl.innerHTML = `<span class="tab-label">PS ${id}</span><span class="tab-close" title="关闭">×</span>`;
  tabEl.addEventListener("click", () => activateTab(id));

  // 终端容器。
  const paneEl = document.createElement("div");
  paneEl.className = "pane";
  paneEl.dataset.id = id;

  tabbar.appendChild(tabEl);
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
  for (const el of tabbar.children) {
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
    tabs.delete(id);
    removeTab(id);
  }
}

/** 从 UI 移除 tab 与 pane。 */
function removeTab(id: string): void {
  tabs.delete(id);
  tabbar.querySelector(`[data-id="${id}"]`)?.remove();
  panes.querySelector(`[data-id="${id}"]`)?.remove();
  const remaining = tabs.keys().next();
  if (remaining.done) {
    emptyHint.hidden = false;
  } else {
    activateTab(remaining.value);
  }
}

document.getElementById("new-terminal")!.addEventListener("click", () => {
  void newTerminal();
});
