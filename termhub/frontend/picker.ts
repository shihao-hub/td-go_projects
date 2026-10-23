// picker.ts：终端初始目录选择弹窗——目录浏览 + 驱动器切换 + 上次目录记忆。

interface DirEntry {
  name: string;
  path: string;
}

interface DirListing {
  path: string;
  parent: string;
  home: string;
  dirs: DirEntry[];
}

const LAST_DIR_KEY = "termhub.lastDir";

let resolvePick: ((value: string | null) => void) | null = null;
let bound = false;
let currentDir = "";
let parentDir = "";
let homeDir = "";

function byId<T extends HTMLElement>(id: string): T {
  return document.getElementById(id) as T;
}

function closeModal(value: string | null): void {
  byId<HTMLDivElement>("dir-modal").hidden = true;
  const resolve = resolvePick;
  resolvePick = null;
  resolve?.(value);
}

function updateActions(): void {
  byId<HTMLButtonElement>("dir-ok").disabled = currentDir === "";
  byId<HTMLButtonElement>("dir-up").disabled = parentDir === "" && currentDir === "";
}

function messageItem(text: string): HTMLLIElement {
  const li = document.createElement("li");
  li.className = "dir-empty";
  li.textContent = text;
  return li;
}

function render(data: DirListing): void {
  currentDir = data.path;
  parentDir = data.parent;
  if (data.home) homeDir = data.home;

  byId<HTMLInputElement>("dir-path").value = data.path;
  byId("dir-current").textContent = data.path || "此电脑（请选择一个驱动器）";

  const list = byId<HTMLUListElement>("dir-list");
  list.replaceChildren();

  if (data.dirs.length === 0) {
    list.append(messageItem(currentDir ? "（没有子目录）" : "未发现可用驱动器"));
    updateActions();
    return;
  }

  for (const entry of data.dirs) {
    const li = document.createElement("li");
    li.className = "dir-item";
    li.tabIndex = 0;
    li.title = entry.path;

    const icon = document.createElement("span");
    icon.className = "dir-icon";
    const name = document.createElement("span");
    name.className = "dir-name";
    name.textContent = entry.name;

    li.append(icon, name);
    li.addEventListener("click", () => void load(entry.path));
    li.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") void load(entry.path);
    });
    list.append(li);
  }
  updateActions();
}

async function load(path: string): Promise<void> {
  const list = byId<HTMLUListElement>("dir-list");
  list.replaceChildren(messageItem("加载中…"));
  try {
    const resp = await fetch(`/api/v1/fs/list?path=${encodeURIComponent(path)}`);
    const json = await resp.json();
    if (!json.ok) throw new Error(json.error ?? "读取目录失败");
    render(json.data as DirListing);
  } catch (err) {
    currentDir = "";
    parentDir = "";
    updateActions();
    byId<HTMLInputElement>("dir-path").value = path;
    byId("dir-current").textContent = "无法读取该目录";
    list.replaceChildren(messageItem(err instanceof Error ? err.message : String(err)));
  }
}

function bind(): void {
  if (bound) return;
  bound = true;

  byId("dir-close").addEventListener("click", () => closeModal(null));
  byId("dir-cancel").addEventListener("click", () => closeModal(null));
  byId("dir-up").addEventListener("click", () => void load(parentDir));
  byId("dir-home").addEventListener("click", () => void load(homeDir));
  byId("dir-drives").addEventListener("click", () => void load(""));
  byId("dir-ok").addEventListener("click", () => {
    if (currentDir === "") return;
    localStorage.setItem(LAST_DIR_KEY, currentDir);
    closeModal(currentDir);
  });

  const input = byId<HTMLInputElement>("dir-path");
  const jump = () => {
    const value = input.value.trim();
    if (value !== "") void load(value);
  };
  input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") {
      ev.preventDefault();
      jump();
    }
  });
  byId("dir-go").addEventListener("click", jump);

  document.addEventListener("keydown", (ev) => {
    if (byId<HTMLDivElement>("dir-modal").hidden) return;
    if (ev.key === "Escape") {
      ev.preventDefault();
      closeModal(null);
    }
  });

  byId<HTMLDivElement>("dir-modal").addEventListener("mousedown", (ev) => {
    if (ev.target === ev.currentTarget) closeModal(null);
  });
}

/** 打开目录选择弹窗；确认返回目录绝对路径，取消返回 null。 */
export function pickDirectory(): Promise<string | null> {
  bind();
  if (resolvePick) {
    const previous = resolvePick;
    resolvePick = null;
    previous(null);
  }

  byId<HTMLDivElement>("dir-modal").hidden = false;
  const start = localStorage.getItem(LAST_DIR_KEY) || "~";
  void load(start);

  const promise = new Promise<string | null>((resolve) => {
    resolvePick = resolve;
  });
  byId<HTMLInputElement>("dir-path").focus();
  return promise;
}
