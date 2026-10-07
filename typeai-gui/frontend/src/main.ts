// typeai-gui 前端：xterm.js 终端 + Wails 事件桥。
// 界面即终端：无路由、无组件框架，窗口里只有一块 xterm 与一个状态覆盖层。
import "@xterm/xterm/css/xterm.css";
import "./style.css";

import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { Events } from "@wailsio/runtime";

import {
  Resize as PtyResize,
  Restart as PtyRestart,
  Start as PtyStart,
  State as PtyState,
  Write as PtyWrite,
} from "../bindings/typeai-gui/internal/guiapp/terminalservice";

// ---- 事件名（与 Go 侧 bindings.go 常量一致）----
const EV_DATA = "terminal:data";
const EV_EXIT = "terminal:exit";
const EV_ERROR = "terminal:error";

// ---- 状态视图（对应 Go TerminalState）----
interface TerminalState {
  gen: number;
  typeaiPath: string;
  running: boolean;
  startError: string;
}

const term = new Terminal({
  fontFamily:
    '"Cascadia Mono", "Cascadia Code", Consolas, "Sarasa Mono SC", "Microsoft YaHei Mono", monospace',
  fontSize: 15,
  lineHeight: 1.1,
  cursorBlink: true,
  scrollback: 5000,
  convertEol: false, // TUI 输出 VT 序列，按字节透传不加工
  theme: {
    background: "#0c0c0c",
    foreground: "#cccccc",
    cursor: "#cccccc",
    selectionBackground: "#264f78",
  },
});
const fit = new FitAddon();
term.loadAddon(fit);
term.loadAddon(new WebLinksAddon());
term.open(document.getElementById("terminal")!);

const overlay = document.getElementById("overlay")!;

// 当前会话代号：旧会话的退出事件不再作用于新界面
let currentGen = -1;
let sessionAlive = false;

// ---- base64 → bytes：JSON 事件通道下保证 PTY 字节保真 ----
function b64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return bytes;
}

function showError(message: string, hint?: string): void {
  overlay.innerHTML = "";
  const msg = document.createElement("div");
  msg.className = "error";
  msg.textContent = message;
  overlay.appendChild(msg);
  if (hint) {
    const h = document.createElement("div");
    h.className = "hint";
    h.textContent = hint;
    overlay.appendChild(h);
  }
  overlay.classList.remove("hidden");
}

function showExited(code: number): void {
  overlay.innerHTML = "";
  const msg = document.createElement("div");
  msg.textContent =
    code === 0 ? "会话已结束。" : `typeai 已退出（退出码 ${code}）。`;
  overlay.appendChild(msg);
  const h = document.createElement("div");
  h.className = "hint";
  h.textContent = "按 Alt+R 重新启动，或直接关闭窗口。";
  overlay.appendChild(h);
  overlay.classList.remove("hidden");
}

// ---- 尺寸同步：容器变化 → fit → PTY ----
function fitNow(): void {
  try {
    fit.fit(); // 触发 term.onResize → Go Resize
  } catch {
    // 容器尚未就绪时忽略
  }
}

term.onResize(({ cols, rows }) => {
  if (sessionAlive) {
    void PtyResize(cols, rows).catch(() => {});
  }
});
term.onData((data) => {
  if (sessionAlive) {
    void PtyWrite(data).catch(() => {});
  }
});

new ResizeObserver(() => fitNow()).observe(
  document.getElementById("terminal")!,
);

// ---- Go → 前端事件 ----
Events.On(EV_DATA, (ev) => {
  term.write(b64ToBytes(ev.data as string));
});
Events.On(EV_EXIT, (ev) => {
  const { gen, code } = ev.data as { gen: number; code: number };
  if (gen === currentGen) {
    sessionAlive = false;
    showExited(code);
  }
});
Events.On(EV_ERROR, (ev) => {
  const { message } = ev.data as { message: string };
  sessionAlive = false;
  showError(message, "可将 typeai.exe 放在本程序同目录，或设置环境变量 TYPEAI_GUI_TYPEAI_PATH 后重启。");
});

// 重启快捷键 Alt+R（覆盖层可见时生效）
window.addEventListener("keydown", (e) => {
  if (e.altKey && (e.key === "r" || e.key === "R")) {
    void restart();
  }
});

async function restart(): Promise<void> {
  overlay.classList.add("hidden");
  term.reset();
  const dims = fit.proposeDimensions();
  await PtyRestart(dims?.cols ?? 80, dims?.rows ?? 25).catch((err) => {
    showError(String(err));
  });
  await refreshState();
}

async function refreshState(): Promise<void> {
  const st = (await PtyState()) as TerminalState;
  currentGen = st.gen;
  sessionAlive = st.running;
  if (st.startError) {
    showError(
      st.startError,
      "可将 typeai.exe 放在本程序同目录，或设置环境变量 TYPEAI_GUI_TYPEAI_PATH 后重启。",
    );
  }
}

// ---- 启动流程：按当前容器尺寸起会话 ----
(async () => {
  fitNow();
  const dims = fit.proposeDimensions();
  try {
    await PtyStart(dims?.cols ?? 80, dims?.rows ?? 25);
  } catch (err) {
    // 启动失败细节经 terminal:error 事件展示；此处仅兜底
    void err;
  }
  await refreshState();
  term.focus();
})();
