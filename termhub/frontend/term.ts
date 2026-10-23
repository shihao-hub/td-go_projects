// term.ts：单个终端 tab 的运行时——xterm.js 渲染 + WS 事件桥。
// 职责最小面：把键盘输入打包上行，把 WS 下行字节喂给 xterm 渲染。

// terminal 通过原生 TS 编写，xterm 提供类型。
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";

export interface TabHandle {
  id: string;
  term: Terminal;
  ws: WebSocket;
  dispose(): void;
}

const wsMsg = (type: string, extra: object = {}) =>
  JSON.stringify({ type, ...extra });

/** 创建一个终端 tab：发起 WS 连接、绑定输入与尺寸。 */
export function createTerminalTab(
  sessionId: string,
  container: HTMLElement,
  onExited: (id: string) => void,
): TabHandle {
  const term = new Terminal({
    fontFamily: "Consolas, 'Microsoft YaHei Mono', monospace",
    fontSize: 14,
    cursorBlink: true,
    theme: {
      background: "#f7f8f6", // 浅色亮堂底，禁暗色终端风
      foreground: "#1f2d24",
      cursor: "#2f9e44",
      selectionBackground: "#dcefe2",
    },
  });
  container.appendChild(term.element!);
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
    id: sessionId,
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
  ws.onclose = () => onExited(sessionId);
  ws.onerror = () => onExited(sessionId);

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
