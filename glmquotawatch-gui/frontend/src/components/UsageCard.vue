<script setup lang="ts">
// UsageCard：非对称布局微型仪表——左侧大数与状态评级，右侧倒计时与精细通知胶囊
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

// 三态分级：<50 稳健 / 50-79 需关注 / ≥80 临界
const tone = computed(() => {
  if (pct.value >= 80) return "crit";
  if (pct.value >= 50) return "warn";
  return "brand";
});

const statusDesc = computed(() => {
  if (pct.value >= 90) return "配额极度紧绷";
  if (pct.value >= 80) return "已逼近警戒线";
  if (pct.value >= 50) return "用量消耗过半";
  return "额度充裕，负载平稳";
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

// 倒计时重算
const resetIn = computed(() => {
  if (!props.win.next_reset_time) return "";
  const remain = Date.parse(props.win.next_reset_time) - nowTs.value;
  if (remain <= 0) return "即将释放";
  const m = Math.floor(remain / 60000);
  const h = Math.floor(m / 60);
  const d = Math.floor(h / 24);
  if (d > 0) return `${d} 天 ${h % 24} 小时`;
  if (h > 0) return `${h} 小时 ${m % 60} 分`;
  return `${m} 分钟`;
});
</script>

<template>
  <div class="relative overflow-hidden rounded-xl border border-edge bg-panel p-4 shadow-card inset-highlight transition-all duration-200 hover:border-edge-strong">
    <!-- 顶栏：标签与重置周期 -->
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-2">
        <span class="h-2 w-2 rounded-full" :class="barClass"></span>
        <span class="text-xs font-semibold text-ink">{{ win.label }}</span>
        <span class="text-[11px] text-faint">· {{ statusDesc }}</span>
      </div>

      <div v-if="resetIn" class="flex items-center gap-1 rounded bg-well px-1.5 py-0.5 text-[10px] text-dim tnum">
        <svg class="h-3 w-3 text-faint" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10" />
          <path d="M12 6v6l4 2" />
        </svg>
        <span>周期倒计时 {{ resetIn }}</span>
      </div>
    </div>

    <!-- 非对称主体：左侧超大数字，右侧阈值与水位标尺 -->
    <div class="mt-3 flex items-end justify-between">
      <div class="flex items-baseline gap-1">
        <span class="text-4xl font-extrabold tracking-tight tnum" :class="numClass">{{ pct }}</span>
        <span class="text-sm font-semibold text-faint">%</span>
        <span class="ml-2 text-[11px] text-faint">已占用</span>
      </div>

      <!-- 右侧阶梯告警标签 -->
      <div class="flex flex-wrap items-center justify-end gap-1.5">
        <span v-if="!win.notified?.length" class="text-[10px] text-faint">未跨越告警阶梯</span>
        <span
          v-for="th in win.notified ?? []"
          :key="th"
          class="inline-flex items-center gap-1 rounded-md border border-warn/30 bg-warn-light px-1.5 py-0.5 text-[10px] font-medium text-warn tnum"
        >
          <span class="h-1 w-1 rounded-full bg-warn"></span>
          {{ th }}% 梯级已弹窗
        </span>
      </div>
    </div>

    <!-- 带有微刻度指针的高精仪表轨道 -->
    <div class="relative mt-3.5 pt-1">
      <div class="relative h-2 w-full overflow-hidden rounded-full bg-well shadow-inner">
        <div
          class="h-full rounded-full transition-all duration-700 ease-out"
          :class="barClass"
          :style="{ width: `${Math.min(100, Math.max(0, pct))}%` }"
        />
      </div>

      <!-- 刻度分档垂直标尺 -->
      <div
        v-for="th in config?.thresholds ?? []"
        :key="th"
        class="group absolute top-0 -translate-x-1/2"
        :style="{ left: `${th}%` }"
      >
        <div class="h-4 w-[1.5px] rounded-full bg-faint/80 shadow-xs transition-colors group-hover:bg-ink"></div>
        <span class="absolute -top-3.5 left-1/2 -translate-x-1/2 opacity-0 transition-opacity group-hover:opacity-100 text-[9px] font-bold text-dim tnum">
          {{ th }}%
        </span>
      </div>
    </div>
  </div>
</template>