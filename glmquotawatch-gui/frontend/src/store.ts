// 全局响应式状态：GUIState 快照 + 派生 UI 状态。
// 绑定调用返回的就是普通 JSON 对象（键名与 Go json tag 一致），
// 类型经 GUIState 约束即可，无需生成类的 createFrom 水合。
import { reactive, computed } from "vue";
import { bindings } from "./api";
import type { GUIState } from "./api";

export const store = reactive({
  state: null as GUIState | null,
  sampleBusy: false,
  tab: "dashboard" as "dashboard" | "history" | "settings",
});

export const isDemo = computed(() => store.state?.mode === "demo");
export const hasToken = computed(() => store.state?.config?.has_token === true);

export async function refreshState(): Promise<void> {
  store.state = (await bindings.GetState()) as GUIState;
}

export async function sampleNow(): Promise<void> {
  if (store.sampleBusy) return;
  store.sampleBusy = true;
  try {
    await bindings.SampleNow();
    // 成功结果经 "status" 事件回流（runtime.onSample 广播），此处无需处理
  } catch (e) {
    console.error("sample failed:", e);
    await refreshState().catch(() => undefined);
  } finally {
    store.sampleBusy = false;
  }
}
