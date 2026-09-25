<script setup lang="ts">
// Dashboard：套餐等级 + 窗口卡片 + 手动刷新 + 错误横幅 + 未配置 token 引导 +
// 演示横幅（倒计时与退出）。浅色产品风，采样时间在页脚状态栏。
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
  <div class="mx-auto flex max-w-3xl flex-col gap-3">
    <!-- 演示横幅 -->
    <div
      v-if="store.state?.mode === 'demo'"
      class="flex items-center gap-3 rounded-xl border border-warn/25 bg-warn/10 px-4 py-2.5"
    >
      <span class="flex items-center gap-1.5 text-xs font-medium text-warn">
        <span class="h-1.5 w-1.5 rounded-full bg-warn"></span>
        演示模式
      </span>
      <span class="text-xs text-warn tnum">剩余 {{ demoRemain }}</span>
      <span class="text-[11px] text-warn/70">60 倍速 · 5 分钟走完 5 小时窗口</span>
      <button
        class="ml-auto rounded-lg border border-warn/30 bg-panel px-2.5 py-1 text-xs text-warn transition-colors hover:bg-warn/10 disabled:opacity-50"
        :disabled="exiting"
        @click="exitDemo"
      >
        退出演示
      </button>
    </div>

    <!-- 采样失败横幅 -->
    <div
      v-if="store.state?.last_error"
      class="rounded-xl border border-crit/25 bg-crit/5 px-4 py-2.5"
    >
      <div class="text-xs font-medium text-crit">
        采样失败（{{ store.state.last_error.code }}）
      </div>
      <div class="mt-0.5 text-[11px] text-crit/70">{{ store.state.last_error.message }}，下轮自动重试</div>
    </div>

    <!-- 未配置 token 引导（FR-6 / AC-10） -->
    <div
      v-if="store.state?.config && !store.state.config.has_token"
      class="rounded-xl border border-dashed border-edge bg-panel px-6 py-10 text-center shadow-sm"
    >
      <div class="text-sm font-medium text-ink">尚未配置 GLM API Token</div>
      <div class="mt-1 text-xs text-dim">配置后自动开始周期采样与阈值告警</div>
      <button
        class="mt-4 rounded-lg bg-brand px-4 py-1.5 text-xs font-medium text-white transition-colors hover:bg-brand/90"
        @click="store.tab = 'settings'"
      >
        前往设置
      </button>
    </div>

    <template v-else>
      <!-- 概览行：套餐徽标 + 采样按钮（采样时间在页脚状态栏） -->
      <div class="flex items-center gap-2 text-xs">
        <span
          v-if="store.state?.status"
          class="rounded-full bg-well px-2.5 py-0.5 text-[11px] text-dim"
        >
          套餐 {{ store.state.status.level }}
        </span>
        <button
          class="ml-auto flex items-center gap-1.5 rounded-lg bg-brand px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-brand/90 disabled:opacity-50"
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

      <!-- 窗口卡片：auto-fit 网格，单卡占满行，多卡自动两列 -->
      <div
        v-if="store.state?.status?.windows?.length"
        class="grid grid-cols-[repeat(auto-fit,minmax(300px,1fr))] gap-3"
      >
        <UsageCard
          v-for="w in store.state.status.windows"
          :key="w.key"
          :win="w"
          :config="store.state.config ?? null"
        />
      </div>
      <div
        v-else-if="store.state?.status"
        class="rounded-xl border border-edge bg-panel px-6 py-8 text-center text-xs text-faint shadow-sm"
      >
        本次采样未返回窗口数据
      </div>
      <div
        v-else
        class="rounded-xl border border-edge bg-panel px-6 py-8 text-center text-xs text-faint shadow-sm"
      >
        正在等待首次采样…
      </div>
    </template>
  </div>
</template>
