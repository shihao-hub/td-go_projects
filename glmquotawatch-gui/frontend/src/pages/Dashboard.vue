<script setup lang="ts">
// Dashboard：套餐等级 + 采样时间 + 窗口卡片 + 手动刷新 + 错误横幅 +
// 未配置 token 引导 + 演示横幅（倒计时与退出）。
import { computed, onUnmounted, ref } from "vue";
import { store, sampleNow, refreshState } from "../store";
import { bindings } from "../api";
import UsageCard from "../components/UsageCard.vue";

// 演示剩余时间（本地每秒重算）
const nowTs = ref(Date.now());
const timer = window.setInterval(() => (nowTs.value = Date.now()), 1000);
onUnmounted(() => window.clearInterval(timer));

const demoRemain = computed(() => {
  if (store.state?.mode !== "demo" || !store.state?.demo_ends_at) return "";
  const sec = Math.max(0, Math.floor((Date.parse(store.state.demo_ends_at) - nowTs.value) / 1000));
  return `${Math.floor(sec / 60)}:${String(sec % 60).padStart(2, "0")}`;
});

const exiting = ref(false);
async function exitDemo() {
  if (exiting.value) return;
  exiting.value = true;
  try {
    await bindings.ExitDemo();
    await refreshState();
  } finally {
    exiting.value = false;
  }
}
</script>

<template>
  <div class="mx-auto flex max-w-2xl flex-col gap-3">
    <!-- 演示横幅 -->
    <div
      v-if="store.state?.mode === 'demo'"
      class="flex items-center gap-3 rounded-xl border border-amber-500/40 bg-amber-500/10 px-4 py-2.5"
    >
      <span class="text-xs font-medium text-amber-400">演示模式</span>
      <span class="text-xs text-amber-300/80 tnum">剩余 {{ demoRemain }}</span>
      <span class="text-[11px] text-amber-300/60">60 倍速 · 5 分钟走完 5 小时窗口</span>
      <button
        class="ml-auto rounded-md border border-amber-500/40 px-2.5 py-1 text-xs text-amber-300 transition-colors hover:bg-amber-500/10 disabled:opacity-50"
        :disabled="exiting"
        @click="exitDemo"
      >
        退出演示
      </button>
    </div>

    <!-- 采样失败横幅 -->
    <div
      v-if="store.state?.last_error"
      class="rounded-xl border border-red-500/40 bg-red-500/10 px-4 py-2.5"
    >
      <div class="text-xs font-medium text-red-400">
        采样失败（{{ store.state.last_error.code }}）
      </div>
      <div class="mt-0.5 text-[11px] text-red-300/70">{{ store.state.last_error.message }}，下轮自动重试</div>
    </div>

    <!-- 未配置 token 引导（FR-6 / AC-10） -->
    <div
      v-if="store.state?.config && !store.state.config.has_token"
      class="rounded-xl border border-dashed border-zinc-700 bg-zinc-900/60 px-6 py-10 text-center"
    >
      <div class="text-sm text-zinc-300">尚未配置 GLM API token</div>
      <div class="mt-1 text-xs text-zinc-500">配置后自动开始周期采样与阈值告警</div>
      <button
        class="mt-4 rounded-lg bg-sky-500 px-4 py-1.5 text-xs font-medium text-white transition-colors hover:bg-sky-400"
        @click="store.tab = 'settings'"
      >
        前往设置
      </button>
    </div>

    <template v-else>
      <!-- 概览行 -->
      <div class="flex items-center gap-2 text-xs text-zinc-500">
        <span
          v-if="store.state?.status"
          class="rounded border border-zinc-700 px-1.5 py-0.5 text-[10px] text-zinc-400"
        >
          套餐 {{ store.state.status.level }}
        </span>
        <span v-if="store.state?.status" class="tnum">
          采样于 {{ new Date(store.state.status.sampled_at).toLocaleTimeString() }}
        </span>
        <button
          class="ml-auto flex items-center gap-1.5 rounded-lg border border-zinc-700 px-3 py-1.5 text-xs text-zinc-300 transition-colors hover:bg-zinc-800 disabled:opacity-50"
          :disabled="store.sampleBusy"
          @click="sampleNow()"
        >
          <svg
            class="h-3.5 w-3.5"
            :class="store.sampleBusy ? 'animate-spin' : ''"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path d="M21 12a9 9 0 1 1-3-6.7M21 3v6h-6" />
          </svg>
          {{ store.sampleBusy ? "采样中…" : "立即采样" }}
        </button>
      </div>

      <!-- 窗口卡片 -->
      <div v-if="store.state?.status?.windows?.length" class="flex flex-col gap-3">
        <UsageCard
          v-for="w in store.state.status.windows"
          :key="w.key"
          :win="w"
          :config="store.state.config ?? null"
        />
      </div>
      <div
        v-else-if="store.state?.status"
        class="rounded-xl border border-zinc-800 bg-zinc-900/60 px-6 py-8 text-center text-xs text-zinc-500"
      >
        本次采样未返回 token 窗口数据
      </div>
      <div
        v-else
        class="rounded-xl border border-zinc-800 bg-zinc-900/60 px-6 py-8 text-center text-xs text-zinc-500"
      >
        正在等待首次采样…
      </div>
    </template>
  </div>
</template>
