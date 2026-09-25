<script setup lang="ts">
// UsageCard：单个额度窗口卡片——状态彩点、大号百分比、按档分色进度条、
// 阈值刻度、倒计时（本地每秒重算）、已通知档位胶囊。浅色产品风。
import { computed, onUnmounted, ref } from "vue";
import type { WindowView, ConfigView } from "../api";

const props = defineProps<{
  win: WindowView;
  config: ConfigView | null;
}>();

const nowTs = ref(Date.now());
const timer = window.setInterval(() => (nowTs.value = Date.now()), 1000);
onUnmounted(() => window.clearInterval(timer));

const pct = computed(() => props.win.percentage ?? 0);

// 颜色分档：<50 绿 / 50-79 琥珀 / ≥80 红
const tone = computed(() => {
  if (pct.value >= 80) return "crit";
  if (pct.value >= 50) return "warn";
  return "brand";
});
const barClass = computed(
  () =>
    ({
      brand: "bg-brand",
      warn: "bg-warn",
      crit: "bg-crit",
    })[tone.value],
);
const numClass = computed(
  () =>
    ({
      brand: "text-brand-deep",
      warn: "text-warn",
      crit: "text-crit",
    })[tone.value],
);

// 倒计时：从 next_reset_time 本地重算（ResetIn 是采样时刻的快照）
const resetIn = computed(() => {
  if (!props.win.next_reset_time) return "";
  const remain = Date.parse(props.win.next_reset_time) - nowTs.value;
  if (remain <= 0) return "即将刷新";
  const m = Math.floor(remain / 60000);
  const h = Math.floor(m / 60);
  const d = Math.floor(h / 24);
  if (d > 0) return `${d}天${h % 24}时`;
  if (h > 0) return `${h}时${m % 60}分`;
  return `${m}分`;
});
</script>

<template>
  <div class="rounded-xl border border-edge bg-panel p-4 shadow-sm">
    <div class="flex items-center justify-between">
      <span class="flex items-center gap-1.5 text-xs text-dim">
        <span class="h-2 w-2 rounded-full" :class="barClass"></span>
        {{ win.label }}
      </span>
      <span v-if="resetIn" class="text-[11px] text-faint tnum">
        距刷新 {{ resetIn }}
      </span>
    </div>
    <div class="mt-1 flex items-baseline gap-1">
      <span class="text-4xl font-bold tnum" :class="numClass">{{ pct }}</span>
      <span class="text-sm text-faint">%</span>
    </div>

    <!-- 进度条 + 阈值刻度 -->
    <div class="relative mt-3 h-2 rounded-full bg-well">
      <div
        class="absolute inset-y-0 left-0 rounded-full transition-all duration-500"
        :class="barClass"
        :style="{ width: pct + '%' }"
      />
      <div
        v-for="th in config?.thresholds ?? []"
        :key="th"
        class="absolute -top-0.5 h-3 w-0.5 rounded-full bg-faint/70"
        :style="{ left: th + '%' }"
      />
    </div>

    <div class="mt-3 flex items-center gap-1.5">
      <span
        v-if="!win.notified?.length"
        class="text-[11px] text-faint"
      >无已告警档位</span>
      <span
        v-for="th in win.notified ?? []"
        :key="th"
        class="rounded-full bg-warn/10 px-2 py-0.5 text-[11px] font-medium text-warn tnum"
      >
        {{ th }}% 已告警
      </span>
    </div>
  </div>
</template>
