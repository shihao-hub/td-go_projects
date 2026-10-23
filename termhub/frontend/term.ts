// term.ts：单个终端 tab 的运行时——xterm.js 渲染 + WS 事件桥。
// 职责最小面：把键盘输入打包上行，把 WS 下行字节喂给 xterm 渲染。

// terminal 通过原生 TS 编写，xterm 提供类型。
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";

export interface TabHandle {
  // id 该 tab 对应的终端记忆（slot）身份，稳定不变。
  id: string;
  // sessionId 本次 shell 运行的会话身份；WS 与 resize 都走它。
  sessionId: string;
  term: Terminal;
  ws: WebSocket;
  dispose(): void;
}

const wsMsg = (type: string, extra: object = {}) =>
  JSON.stringify({ type, ...extra });

/** 创建一个终端 tab：发起 WS 连接、绑定输入与尺寸。 */
export function createTerminalTab(
  slotId: string,
  sessionId: string,
  container: HTMLElement,
  onExited: (slotId: string) => void,
): TabHandle {
  const term = new Terminal({
    fontFamily: "'Cascadia Mono', Consolas, 'Microsoft YaHei Mono', monospace",
    fontSize: 14,
    cursorBlink: true,
    theme: {
      background: "#0d1117",
      foreground: "#d7dde5",
      cursor: "#3fb950",
      cursorAccent: "#0d1117",
      selectionBackground: "rgba(63, 185, 80, 0.28)",
      black: "#1b222c",
      red: "#f85149",
      green: "#3fb950",
      yellow: "#d29922",
      blue: "#58a6ff",
      magenta: "#bc8cff",
      cyan: "#39c5cf",
      white: "#b1bac4",
      brightBlack: "#6e7681",
      brightRed: "#ff7b72",
      brightGreen: "#56d364",
      brightYellow: "#e3b341",
      brightBlue: "#79c0ff",
      brightMagenta: "#d2a8ff",
      brightCyan: "#56d4dd",
      brightWhite: "#f0f6fc",
    },
  });
  // xterm v6 的 element 在 open() 之前不存在，必须用 open(container) 挂载。
  term.open(container);
  term.onData((data) => {
    if (handle.ws.readyState === WebSocket.OPEN) {
      handle.ws.send(wsMsg("input", { data }));
    }
  });

  // WS 实时输出下行（先回放 backlog 再接实时流）。
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/api/v1/ws/${sessionId}`);
  ws.binaryType = "arraybuffer";
  const fitAddon = new FitAddon();
  term.loadAddon(fitAddon);

  const handle: TabHandle = {
    id: slotId,
    sessionId,
    term,
    ws,
    dispose() {
      ws.close();
      term.dispose();
    },
  };

  ws.onmessage = (ev) => {
    term.write(new Uint8Array(ev.data as ArrayBuffer));
  };
  ws.onclose = () => onExited(slotId);
  ws.onerror = () => onExited(slotId);

  // 尺寸变化（tab 激活/窗口缩放）触发 fit + 上行 resize。
  const fitAndResize = () => {
    try {
      fitAddon.fit();
    } catch {
      /* 容器不可见时跳过 */
    }
    if (handle.ws.readyState === WebSocket.OPEN) {
      handle.ws.send(wsMsg("resize", { cols: term.cols, rows: term.rows }));
    }
  };
  // 直接监听容器尺寸（ResizeObserver 兼容隐藏 tab 激活时序）。
  const ro = new ResizeObserver(fitAndResize);
  ro.observe(container);

  return handle;
}
