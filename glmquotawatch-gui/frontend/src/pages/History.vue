<script setup lang="ts">
// History：高定 ECharts 折线走势 + 精致胶囊选择器 + 拒绝原生糙感下拉
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
    grid: { left: 42, right: 20, top: 20, bottom: 28 },
    tooltip: {
      trigger: "axis",
      backgroundColor: "#ffffff",
      borderColor: "#e1e6e3",
      borderWidth: 1,
      padding: [8, 12],
      textStyle: { color: "#111827", fontSize: 11, fontFamily: "sans-serif" },
      extraCssText: "box-shadow: 0 4px 16px -2px rgba(15,23,42,0.08); border-radius: 8px;",
      valueFormatter: (v) => `${v}% 占用`,
    },
    xAxis: {
      type: "time",
      axisLine: { lineStyle: { color: "#e1e6e3" } },
      axisTick: { show: false },
      axisLabel: { color: "#8b97a2", fontSize: 10 },
      splitLine: { show: false },
    },
    yAxis: {
      type: "value",
      min: 0,
      max: 100,
      interval: 20,
      axisLabel: { color: "#8b97a2", fontSize: 10, formatter: "{value}%" },
      splitLine: { lineStyle: { color: "#f0f4f2", type: "dashed" } },
    },
    series: [
      {
        type: "line",
        showSymbol: false,
        smooth: 0.15,
        step: false,
        lineStyle: { color: "#0d9468", width: 2 },
        areaStyle: {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: "rgba(13,148,104,0.14)" },
            { offset: 1, color: "rgba(13,148,104,0.01)" },
          ]),
        },
        data: pts.map((p) => [p.ts, p.pct]),
        markLine: {
          silent: true,
          symbol: "none",
          lineStyle: { color: "#c25e00", type: "dashed", width: 1, opacity: 0.6 },
          label: {
            position: "end",
            color: "#8b97a2",
            fontSize: 9,
            formatter: "{c}% 梯级",
            padding: [0, 4, 0, 0],
          },
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
  <div class="mx-auto flex max-w-4xl flex-col gap-3.5">
    <!-- 过滤器栏：Linear 式胶囊切换 -->
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-1.5">
        <span class="text-xs font-semibold text-ink">观察窗口</span>
        <div class="flex rounded-lg border border-edge bg-well/80 p-0.5 shadow-inner">
          <button
            v-for="w in windows"
            :key="w.key"
            class="btn-press rounded-[6px] px-2.5 py-1 text-xs transition-all"
            :class="
              winKey === w.key
                ? 'bg-panel font-medium text-ink shadow-xs inset-highlight'
                : 'text-dim hover:text-ink'
            "
            @click="winKey = w.key"
          >
            {{ w.label }}
          </button>
        </div>
      </div>

      <div class="flex items-center gap-2">
        <span v-if="loading" class="text-[11px] text-faint">同步数据点…</span>
        <div class="flex rounded-lg border border-edge bg-well/80 p-0.5 shadow-inner">
          <button
            v-for="h in [24, 168] as const"
            :key="h"
            class="btn-press rounded-[6px] px-2.5 py-1 text-xs transition-all"
            :class="
              hours === h
                ? 'bg-panel font-medium text-ink shadow-xs inset-highlight'
                : 'text-dim hover:text-ink'
            "
            @click="hours = h"
          >
            {{ h === 24 ? "近 24 小时" : "近 7 天" }}
          </button>
        </div>
      </div>
    </div>

    <!-- 图表容器卡片 -->
    <div class="relative rounded-2xl border border-edge bg-panel p-3 shadow-card inset-highlight">
      <div
        v-if="empty && !loading"
        class="flex h-80 flex-col items-center justify-center text-center"
      >
        <div class="rounded-full bg-well p-3 text-dim">
          <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
          </svg>
        </div>
        <p class="mt-2.5 text-xs font-medium text-ink">所选时间跨度内暂无轨迹记录</p>
        <p class="mt-1 text-[11px] text-faint">
          {{ isDemo ? "沙盒正在后台以 60 倍速率回放，请稍候几秒等待点阵汇入。" : "监控守护进程将随着每次探测周期自动描绘折线。" }}
        </p>
      </div>

      <div
        v-show="!empty || loading"
        ref="chartEl"
        class="h-80 w-full"
      ></div>
    </div>
  </div>
</template>