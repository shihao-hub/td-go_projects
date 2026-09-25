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
</script>

<template>
  <div class="flex h-full flex-col">
    <!-- 顶栏：应用名 + 模式徽标 + 导航 -->
    <header class="flex items-center gap-3 border-b border-zinc-800 px-5 py-3">
      <div class="flex items-center gap-2">
        <span class="inline-block h-2.5 w-2.5 rounded-full bg-sky-400"></span>
        <h1 class="text-sm font-semibold tracking-wide text-zinc-100">GLM 用量监控</h1>
      </div>
      <span
        v-if="store.state?.mode === 'demo'"
        class="rounded-full border border-amber-500/40 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-400"
      >
        演示模式
      </span>
      <nav class="ml-auto flex gap-1 rounded-lg bg-zinc-900 p-1">
        <button
          v-for="t in tabs"
          :key="t.key"
          class="rounded-md px-3 py-1 text-xs transition-colors"
          :class="
            store.tab === t.key
              ? 'bg-zinc-700 text-zinc-100'
              : 'text-zinc-400 hover:text-zinc-200'
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
  </div>
</template>
