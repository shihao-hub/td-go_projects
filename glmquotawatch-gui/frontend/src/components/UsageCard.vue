<script setup lang="ts">
// UsageCard：单个额度窗口卡片——大号百分比、按档分色进度条、阈值刻度、
// 倒计时（本地每秒重算）、已通知档位标签。
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

// 颜色分档：<50 绿 / 50-79 黄 / ≥80 红
const tone = computed(() => {
  if (pct.value >= 80) return "red";
  if (pct.value >= 50) return "amber";
  return "emerald";
});
const barClass = computed(
  () =>
    ({
      emerald: "bg-emerald-500",
      amber: "bg-amber-500",
      red: "bg-red-500",
    })[tone.value],
);
const numClass = computed(
  () =>
    ({
      emerald: "text-emerald-400",
      amber: "text-amber-400",
      red: "text-red-400",
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
  <div class="rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
    <div class="flex items-baseline justify-between">
      <span class="text-xs text-zinc-400">{{ win.label }}</span>
      <span v-if="resetIn" class="text-[11px] text-zinc-500 tnum">
        距刷新 {{ resetIn }}
      </span>
    </div>
    <div class="mt-1 flex items-baseline gap-1">
      <span class="text-3xl font-semibold tnum" :class="numClass">{{ pct }}</span>
      <span class="text-sm text-zinc-500">%</span>
    </div>

    <!-- 进度条 + 阈值刻度 -->
    <div class="relative mt-3 h-2 rounded-full bg-zinc-800">
      <div
        class="absolute inset-y-0 left-0 rounded-full transition-all duration-500"
        :class="barClass"
        :style="{ width: pct + '%' }"
      />
      <div
        v-for="th in config?.thresholds ?? []"
        :key="th"
        class="absolute -top-0.5 h-3 w-px bg-zinc-600"
        :style="{ left: th + '%' }"
      />
    </div>

    <div class="mt-3 flex items-center gap-1.5">
      <span
        v-if="!win.notified?.length"
        class="text-[11px] text-zinc-500"
      >无已告警档位</span>
      <span
        v-for="th in win.notified ?? []"
        :key="th"
        class="rounded border border-zinc-700 px-1.5 py-0.5 text-[10px] text-zinc-400 tnum"
      >
        {{ th }}% 已告警
      </span>
    </div>
  </div>
</template>
