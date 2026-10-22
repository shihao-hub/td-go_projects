import { createApp } from "vue";
import App from "./App.vue";
import "./style.css";
import * as service from "../bindings/winstartupmanager/internal/guiapp/startupappservice.js";

const reportedErrors = new Set<string>();

function reportError(source: string, message: unknown, stack?: string) {
  const text = String(message ?? "").trim();
  if (!text) return;

  const key = `${source}|${text}`;
  if (reportedErrors.has(key)) return;
  reportedErrors.add(key);

  service.ReportFrontendError(`[${source}] ${text}`, stack || "").catch(() => {
    // 上报失败时静默，避免错误处理本身造成循环
  });
}

window.addEventListener("error", (event) => {
  reportError("window.error", event.message, (event.error as Error | undefined)?.stack);
});

window.addEventListener("unhandledrejection", (event) => {
  const reason = event.reason as { message?: string; stack?: string } | undefined;
  reportError("unhandledrejection", reason?.message ?? event.reason, reason?.stack);
});

createApp(App).mount("#app");
