// 事件订阅封装：组件卸载时自动注销。
import { onUnmounted } from "vue";
import { Events } from "@wailsio/runtime";

export function useEvents(listeners: Record<string, (data: unknown) => void>): void {
  const offs = Object.entries(listeners).map(([name, cb]) =>
    Events.On(name, (ev) => cb(ev.data)),
  );
  onUnmounted(() => offs.forEach((off) => off()));
}
