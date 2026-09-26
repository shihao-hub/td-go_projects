<script setup lang="ts">
// Dashboard：套餐等级概览行 + 非对称主卡 + 沉浸式演示横幅 + 拒绝空洞的去 AI 味文案
import { computed, onUnmounted, ref } from "vue";
import { store, sampleNow, refreshState } from "../store";
import { bindings } from "../api";
import UsageCard from "../components/UsageCard.vue";

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
  <div class="mx-auto flex max-w-4xl flex-col gap-3.5">
    <!-- 演示横幅：紧凑且具象，不再是廉价大黄条 -->
    <div
      v-if="store.state?.mode === 'demo'"
      class="flex items-center justify-between rounded-xl border border-warn/30 bg-warn-light/80 px-4 py-2.5 text-xs shadow-xs"
    >
      <div class="flex items-center gap-2">
        <span class="relative flex h-2 w-2">
          <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-warn opacity-75"></span>
          <span class="relative inline-flex h-2 w-2 rounded-full bg-warn"></span>
        </span>
        <span class="font-semibold text-warn">沙盒加速演示中</span>
        <span class="text-dim">· 模拟 60x 速率回放 5 小时额度窗口</span>
      </div>
      <div class="flex items-center gap-3">
        <span class="text-dim">本轮剩余 <strong class="text-warn tnum">{{ demoRemain }}</strong></span>
        <button
          class="btn-press rounded-md border border-warn/30 bg-panel px-2.5 py-1 text-xs font-medium text-warn shadow-xs hover:bg-warn/10 disabled:opacity-50"
          :disabled="exiting"
          @click="exitDemo"
        >
          {{ exiting ? "正在退出…" : "结束演示" }}
        </button>
      </div>
    </div>

    <!-- 采样错误提示：具象场景化说明，非简单堆砌 code -->
    <div
      v-if="store.state?.last_error"
      class="flex items-start gap-3 rounded-xl border border-crit/30 bg-crit-light px-4 py-3 shadow-xs"
    >
      <div class="mt-0.5 rounded-full bg-crit/10 p-1 text-crit">
        <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="8" x2="12" y2="12" />
          <line x1="12" y1="16" x2="12.01" y2="16" />
        </svg>
      </div>
      <div class="flex-1">
        <div class="text-xs font-semibold text-crit">
          上游同步中断（{{ store.state.last_error.code }}）
        </div>
        <div class="mt-0.5 text-[11px] text-dim">
          {{ store.state.last_error.message }}。后台监控未受影响，将在下一轮探测窗口自动恢复。
        </div>
      </div>
    </div>

    <!-- 未配置 Token 引导卡片：工具化白底内凹样式，拒绝平庸虚线 -->
    <div
      v-if="store.state?.config && !store.state.config.has_token"
      class="flex flex-col items-center justify-center rounded-2xl border border-edge-strong bg-panel px-6 py-12 text-center shadow-card inset-highlight"
    >
      <div class="flex h-11 w-11 items-center justify-center rounded-xl bg-well text-dim shadow-inner">
        <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
          <path d="M7 11V7a5 5 0 0 1 10 0v4" />
        </svg>
      </div>
      <h3 class="mt-3 text-sm font-semibold text-ink">尚未绑定 GLM 开发者密钥</h3>
      <p class="mt-1 max-w-sm text-xs text-dim">
        绑定后将自动激活后台静默巡检、阶梯配额告警与用量历史轨迹收集。
      </p>
      <button
        class="btn-press mt-5 inline-flex items-center gap-1.5 rounded-lg bg-brand px-4 py-2 text-xs font-medium text-white shadow-subtle hover:bg-brand-deep"
        @click="store.tab = 'settings'"
      >
        <span>前往填入 API 密钥</span>
        <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M5 12h14M12 5l7 7-7 7" />
        </svg>
      </button>
    </div>

    <template v-else>
      <!-- 概览控制条：高密度工具属性 -->
      <div class="flex items-center justify-between px-1">
        <div class="flex items-center gap-2">
          <span class="text-xs font-semibold text-ink">配额观测窗口</span>
          <span
            v-if="store.state?.status?.level"
            class="rounded border border-edge-strong bg-panel px-1.5 py-0.5 text-[10px] font-medium text-dim shadow-xs"
          >
            Tier {{ store.state.status.level }}
          </span>
        </div>

        <button
          class="btn-press flex items-center gap-1.5 rounded-lg border border-edge-strong bg-panel px-2.5 py-1 text-xs font-medium text-ink shadow-xs hover:bg-panel-hover disabled:opacity-50"
          :disabled="store.sampleBusy"
          @click="sampleNow()"
        >
          <svg
            class="h-3.5 w-3.5 text-brand"
            :class="store.sampleBusy ? 'animate-spin' : ''"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path d="M21 12a9 9 0 1 1-3-6.7M21 3v6h-6" />
          </svg>
          <span>{{ store.sampleBusy ? "探测同步中…" : "即刻核对用量" }}</span>
        </button>
      </div>

      <!-- 额度窗口网格：自适应排布 -->
      <div
        v-if="store.state?.status?.windows?.length"
        class="grid grid-cols-[repeat(auto-fit,minmax(320px,1fr))] gap-3"
      >
        <UsageCard
          v-for="w in store.state.status.windows"
          :key="w.key"
          :win="w"
          :config="store.state.config ?? null"
        />
      </div>

      <!-- 具象自然空态文案（去 AI 味） -->
      <div
        v-else-if="store.state?.status"
        class="rounded-xl border border-edge bg-panel px-6 py-10 text-center shadow-card"
      >
        <p class="text-xs font-medium text-ink">上游未下发窗口采样数据</p>
        <p class="mt-1 text-[11px] text-faint">请检查该 API Token 在开放平台是否已开通额度或包含生效模型包。</p>
      </div>
      <div
        v-else
        class="rounded-xl border border-edge bg-panel px-6 py-10 text-center shadow-card"
      >
        <div class="inline-block h-4 w-4 animate-spin rounded-full border-2 border-brand border-t-transparent"></div>
        <p class="mt-2 text-xs text-dim">正在向 GLM 服务建立首次指标通讯…</p>
      </div>
    </template>
  </div>
</template>