<script setup lang="ts">
import { onMounted } from "vue";
import { store, refreshState } from "./store";
import { useEvents } from "./composables/useEvents";
import Dashboard from "./pages/Dashboard.vue";
import History from "./pages/History.vue";
import Settings from "./pages/Settings.vue";

const tabs = [
  { key: "dashboard", label: "仪表盘" },
  { key: "history", label: "历史走势" },
  { key: "settings", label: "偏好配置" },
] as const;

useEvents({
  status: () => void refreshState(),
  "sample-error": () => void refreshState(),
  "notify-error": () => void refreshState(),
  "mode-changed": () => void refreshState(),
  "config-changed": () => void refreshState(),
});

onMounted(() => {
  void refreshState();
});

// 页脚状态栏：上次采样的本地时间
function lastSample(): string {
  const at = store.state?.status?.sampled_at;
  return at ? new Date(at).toLocaleTimeString() : "--:--:--";
}
</script>

<template>
  <div class="flex h-full flex-col bg-bg text-ink">
    <!-- 顶栏：Raycast 质感桌面工具栏（内高光 + 悬浮胶囊 Tab + 状态微标） -->
    <header class="flex h-12 shrink-0 items-center justify-between border-b border-edge bg-panel px-4 shadow-subtle inset-highlight">
      <div class="flex items-center gap-2.5">
        <!-- 精密微图标：脉冲状态指示器 -->
        <div class="relative flex h-6 w-6 items-center justify-center rounded-md border border-edge-strong bg-well shadow-inner">
          <svg class="h-3.5 w-3.5 text-brand" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" />
          </svg>
          <span
            class="absolute -top-0.5 -right-0.5 h-1.5 w-1.5 rounded-full"
            :class="store.state?.mode === 'demo' ? 'bg-warn animate-pulse-subtle' : (store.state?.config?.has_token ? 'bg-brand' : 'bg-faint')"
          />
        </div>
        <div class="flex flex-col">
          <span class="text-xs font-semibold tracking-tight text-ink">GLM 用量监控</span>
        </div>
        <span
          v-if="store.state?.mode === 'demo'"
          class="flex items-center gap-1.5 rounded-full border border-warn/25 bg-warn-light px-2 py-0.5 text-[10px] font-medium text-warn shadow-xs"
        >
          <span class="h-1 w-1 rounded-full bg-warn animate-pulse-subtle"></span>
          演示 ×60
        </span>
      </div>

      <!-- Linear 风格精巧分段导航 -->
      <nav class="flex items-center gap-0.5 rounded-lg border border-edge bg-well/90 p-0.5 shadow-inner">
        <button
          v-for="t in tabs"
          :key="t.key"
          class="btn-press rounded-[6px] px-3 py-1 text-xs font-medium transition-all"
          :class="
            store.tab === t.key
              ? 'bg-panel text-ink shadow-xs inset-highlight'
              : 'text-dim hover:text-ink'
          "
          @click="store.tab = t.key"
        >
          {{ t.label }}
        </button>
      </nav>
    </header>

    <!-- 主展示区 -->
    <main class="min-h-0 flex-1 overflow-y-auto px-5 py-4">
      <Dashboard v-show="store.tab === 'dashboard'" />
      <History v-show="store.tab === 'history'" />
      <Settings v-show="store.tab === 'settings'" />
    </main>

    <!-- 精细状态状态栏（类似状态微调器） -->
    <footer class="flex h-7 shrink-0 items-center justify-between border-t border-edge bg-panel px-4 text-[11px] text-dim select-none shadow-xs">
      <div class="flex items-center gap-4">
        <div class="flex items-center gap-1.5">
          <span class="h-1.5 w-1.5 rounded-full" :class="store.state?.mode === 'demo' ? 'bg-warn' : 'bg-brand'"></span>
          <span class="font-medium text-ink">{{ store.state?.mode === "demo" ? "演示模式" : "持续监控中" }}</span>
        </div>
        <span class="text-edge-strong">|</span>
        <div>
          <span class="text-faint">周期 </span>
          <span class="font-medium text-ink tnum">{{ store.state?.config?.interval ?? "--" }}</span>
        </div>
        <span class="text-edge-strong">|</span>
        <div>
          <span class="text-faint">上次快照 </span>
          <span class="font-medium text-ink tnum">{{ lastSample() }}</span>
        </div>
      </div>

      <div class="flex items-center gap-1.5">
        <span class="text-faint">API 凭据</span>
        <span
          class="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium"
          :class="store.state?.config?.has_token ? 'bg-brand-light text-brand-deep' : 'bg-warn-light text-warn'"
        >
          {{ store.state?.config?.has_token ? "已就绪" : "待配置" }}
        </span>
      </div>
    </footer>
  </div>
</template>