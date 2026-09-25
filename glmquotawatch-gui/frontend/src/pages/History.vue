<script setup lang="ts">
// History：窗口选择 + 时间范围（24h/7d）+ ECharts 折线（阈值虚线 markLine）。
import { computed, nextTick, onUnmounted, ref, watch } from "vue";
import * as echarts from "echarts";
import { bindings } from "../api";
import type { HistoryPoint, HistoryWindow } from "../api";
import { store, isDemo } from "../store";

const windows = ref<HistoryWindow[]>([]);
const winKey = ref("");
const hours = ref<24 | 168>(24);
const loading = ref(false);
const empty = ref(false);
const chartEl = ref<HTMLDivElement | null>(null);
let chart: echarts.ECharts | null = null;

const thresholds = computed(() => store.state?.config?.thresholds ?? [50, 60, 80, 90]);

async function loadWindows() {
  try {
    windows.value = ((await bindings.HistoryWindows()) ?? []) as HistoryWindow[];
    if (!windows.value.find((w) => w.key === winKey.value)) {
      winKey.value = windows.value[0]?.key ?? "";
    }
  } catch {
    windows.value = [];
  }
}

async function load() {
  if (!winKey.value) {
    empty.value = true;
    render([]);
    return;
  }
  loading.value = true;
  try {
    const pts = ((await bindings.ReadHistory(winKey.value, hours.value)) ?? []) as HistoryPoint[];
    empty.value = pts.length === 0;
    render(pts);
  } catch (e) {
    console.error("read history failed:", e);
    empty.value = true;
    render([]);
  } finally {
    loading.value = false;
  }
}

function render(pts: HistoryPoint[]) {
  nextTick(() => {
    if (!chartEl.value) return;
    if (!chart) {
      chart = echarts.init(chartEl.value);
      ro.observe(chartEl.value);
    }
    chart.setOption(option(pts), true);
  });
}

function option(pts: HistoryPoint[]): echarts.EChartsOption {
  return {
    backgroundColor: "transparent",
    grid: { left: 36, right: 12, top: 12, bottom: 24 },
    tooltip: {
      trigger: "axis",
      backgroundColor: "#181c25",
      borderColor: "#2a3444",
      textStyle: { color: "#cbd5e1", fontSize: 11 },
      valueFormatter: (v) => `${v}%`,
    },
    xAxis: {
      type: "time",
      axisLine: { lineStyle: { color: "#2a3444" } },
      axisLabel: { color: "#71717a", fontSize: 10 },
    },
    yAxis: {
      type: "value",
      min: 0,
      max: 100,
      interval: 20,
      axisLabel: { color: "#71717a", fontSize: 10, formatter: "{value}%" },
      splitLine: { lineStyle: { color: "#1c222d" } },
    },
    series: [
      {
        type: "line",
        showSymbol: false,
        smooth: false,
        step: false,
        lineStyle: { color: "#38bdf8", width: 1.5 },
        areaStyle: {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: "rgba(56,189,248,0.25)" },
            { offset: 1, color: "rgba(56,189,248,0)" },
          ]),
        },
        data: pts.map((p) => [p.ts, p.pct]),
        markLine: {
          silent: true,
          symbol: "none",
          lineStyle: { color: "#f59e0b", type: "dashed", width: 1 },
          label: { color: "#a1a1aa", fontSize: 10, formatter: "{c}%" },
          data: thresholds.value.map((t) => ({ yAxis: t })),
        },
      },
    ],
  };
}

async function reload() {
  await loadWindows();
  await load();
}

watch([winKey, hours], () => void load());
watch(
  () => store.state?.status?.sampled_at,
  () => {
    // 新采样到达时若正在查看历史，静默补点（演示模式 5s 一轮，同样适用）
    if (store.tab === "history") void load();
  },
);

const ro = new ResizeObserver(() => chart?.resize());
onUnmounted(() => {
  ro.disconnect();
  chart?.dispose();
  chart = null;
});

void reload();
</script>

<template>
  <div class="mx-auto flex max-w-2xl flex-col gap-3">
    <!-- 选择器行 -->
    <div class="flex items-center gap-2 text-xs">
      <select
        v-model="winKey"
        class="rounded-lg border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-zinc-300 outline-none"
      >
        <option v-for="w in windows" :key="w.key" :value="w.key">{{ w.label }}</option>
      </select>
      <div class="flex rounded-lg bg-zinc-900 p-0.5">
        <button
          v-for="h in [24, 168] as const"
          :key="h"
          class="rounded-md px-2.5 py-1 transition-colors"
          :class="hours === h ? 'bg-zinc-700 text-zinc-100' : 'text-zinc-400 hover:text-zinc-200'"
          @click="hours = h"
        >
          {{ h === 24 ? "24 小时" : "7 天" }}
        </button>
      </div>
      <span v-if="loading" class="text-zinc-500">加载中…</span>
    </div>

    <!-- 图表 / 空态 -->
    <div
      v-if="empty && !loading"
      class="rounded-xl border border-zinc-800 bg-zinc-900/60 px-6 py-12 text-center text-xs text-zinc-500"
    >
      所选时间范围内没有采样数据{{ isDemo ? "（演示进行中会逐步积累）" : "" }}
    </div>
    <div
      v-show="!empty || loading"
      ref="chartEl"
      class="h-72 rounded-xl border border-zinc-800 bg-zinc-900/60 p-2"
    ></div>
  </div>
</template>
