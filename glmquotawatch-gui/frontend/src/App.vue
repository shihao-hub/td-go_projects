<script setup lang="ts">
import { onMounted } from "vue";
import { store, refreshState } from "./store";
import { useEvents } from "./composables/useEvents";
import Dashboard from "./pages/Dashboard.vue";
import History from "./pages/History.vue";
import Settings from "./pages/Settings.vue";

const tabs = [
  { key: "dashboard", label: "仪表盘" },
  { key: "history", label: "历史" },
  { key: "settings", label: "设置" },
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

// 页脚状态栏：上次采样的本地时间（事件回流时整体 refreshState）
function lastSample(): string {
  const at = store.state?.status?.sampled_at;
  return at ? new Date(at).toLocaleTimeString() : "--:--:--";
}
</script>

<template>
  <div class="flex h-full flex-col">
    <!-- 顶栏：品牌名 + 演示徽标 + 分段导航 -->
    <header class="flex items-center gap-3 border-b border-edge bg-panel px-5 py-3">
      <h1 class="text-sm font-bold text-brand-deep">GLM 用量监控</h1>
      <span
        v-if="store.state?.mode === 'demo'"
        class="flex items-center gap-1.5 rounded-full bg-warn/10 px-2.5 py-0.5 text-[11px] font-medium text-warn"
      >
        <span class="h-1.5 w-1.5 rounded-full bg-warn"></span>
        演示模式 ×60
      </span>
      <nav class="ml-auto flex gap-1 rounded-lg bg-well p-1">
        <button
          v-for="t in tabs"
          :key="t.key"
          class="rounded-md px-3 py-1 text-xs transition-colors"
          :class="
            store.tab === t.key
              ? 'bg-panel font-medium text-ink shadow-sm'
              : 'text-dim hover:text-ink'
          "
          @click="store.tab = t.key"
        >
          {{ t.label }}
        </button>
      </nav>
    </header>

    <!-- 内容区 -->
    <main class="min-h-0 flex-1 overflow-y-auto px-5 py-4">
      <Dashboard v-show="store.tab === 'dashboard'" />
      <History v-show="store.tab === 'history'" />
      <Settings v-show="store.tab === 'settings'" />
    </main>

    <!-- 页脚状态栏 -->
    <footer class="flex items-center gap-5 border-t border-edge bg-panel px-5 py-1.5 text-[11px]">
      <span>
        <span class="text-faint">模式</span>
        <span
          class="ml-1.5 font-semibold"
          :class="store.state?.mode === 'demo' ? 'text-warn' : 'text-brand-deep'"
        >
          {{ store.state?.mode === "demo" ? "演示" : "监控中" }}
        </span>
      </span>
      <span>
        <span class="text-faint">间隔</span>
        <span class="ml-1.5 text-dim tnum">{{ store.state?.config?.interval ?? "--" }}</span>
      </span>
      <span>
        <span class="text-faint">上次采样</span>
        <span class="ml-1.5 text-dim tnum">{{ lastSample() }}</span>
      </span>
      <span class="ml-auto">
        <span class="text-faint">Token</span>
        <span
          class="ml-1.5 font-semibold"
          :class="store.state?.config?.has_token ? 'text-brand-deep' : 'text-warn'"
        >
          {{ store.state?.config?.has_token ? "已配置" : "未配置" }}
        </span>
      </span>
    </footer>
  </div>
</template>
