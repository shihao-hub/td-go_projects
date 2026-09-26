<script setup lang="ts">
// Settings：Linear 风格紧凑型配置列表——分组面板、内联操作、去模板化的友好表单与就地反馈
import { computed, reactive } from "vue";
import { bindings } from "../api";
import { store, refreshState, isDemo } from "../store";

const cfg = computed(() => store.state?.config ?? null);
const readonly = computed(() => isDemo.value);

const form = reactive({
  token: "",
  interval: "",
  thresholds: "",
  hysteresis: "",
});
const busy = reactive({ token: false, cfg: false });
const msg = reactive({ token: "", interval: "", thresholds: "", hysteresis: "", silent: "", autostart: "" });

function errText(e: unknown): string {
  const s = String(e ?? "");
  const i = s.indexOf(": ");
  return i > 0 ? s.slice(i + 2) : s;
}

async function saveToken() {
  if (!form.token.trim()) return;
  busy.token = true;
  msg.token = "";
  try {
    await bindings.SetToken(form.token);
    form.token = "";
    await refreshState();
  } catch (e) {
    msg.token = errText(e);
  } finally {
    busy.token = false;
  }
}

async function removeToken() {
  busy.token = true;
  msg.token = "";
  try {
    await bindings.RemoveToken();
    await refreshState();
  } catch (e) {
    msg.token = errText(e);
  } finally {
    busy.token = false;
  }
}

async function saveKey(key: "interval" | "thresholds" | "hysteresis", value: string) {
  busy.cfg = true;
  msg[key] = "";
  try {
    await bindings.SetConfig(key, value);
    form[key] = "";
    await refreshState();
  } catch (e) {
    msg[key] = errText(e);
  } finally {
    busy.cfg = false;
  }
}

async function toggleSilent() {
  msg.silent = "";
  try {
    await bindings.SetSilent(!(cfg.value?.silent ?? false));
    await refreshState();
  } catch (e) {
    msg.silent = errText(e);
  }
}

async function toggleAutostart() {
  msg.autostart = "";
  try {
    await bindings.SetAutostart(!(store.state?.autostart ?? false));
    await refreshState();
  } catch (e) {
    msg.autostart = errText(e);
  }
}
</script>

<template>
  <div class="mx-auto flex max-w-3xl flex-col gap-4">
    <!-- 演示模式提示 -->
    <div
      v-if="readonly"
      class="flex items-center gap-2 rounded-xl border border-warn/30 bg-warn-light px-3.5 py-2 text-xs text-warn shadow-xs"
    >
      <svg class="h-4 w-4 shrink-0 text-warn" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="12" cy="12" r="10" />
        <line x1="12" y1="16" x2="12" y2="12" />
        <line x1="12" y1="8" x2="12.01" y2="8" />
      </svg>
      <span>演示沙盒运行中，配置项处于受保护只读状态（demo_readonly）。结束演示后可自由调整。</span>
    </div>

    <!-- 分组一：服务凭据 -->
    <div class="rounded-2xl border border-edge bg-panel p-4 shadow-card inset-highlight">
      <div class="flex items-center justify-between border-b border-edge/60 pb-3">
        <div>
          <h2 class="text-xs font-semibold text-ink">智谱 GLM 开放平台凭证</h2>
          <p class="mt-0.5 text-[11px] text-faint">配置后即可建立安全通信链路并开始采集配额信息。</p>
        </div>
        <span
          v-if="cfg?.has_token"
          class="rounded-md border border-brand/20 bg-brand-light px-2 py-0.5 text-[11px] font-medium text-brand-deep tnum"
        >
          已载入 {{ cfg?.token }}
        </span>
        <span v-else class="text-[11px] text-faint">凭据未登记</span>
      </div>

      <div class="mt-3 flex gap-2">
        <input
          v-model="form.token"
          type="password"
          placeholder="粘贴 BigModel 开发者 API Key（不少于 20 字符）"
          class="min-w-0 flex-1 rounded-lg border border-edge bg-well/60 px-3 py-1.5 text-xs text-ink outline-none placeholder:text-faint focus:border-brand focus:bg-panel transition-all disabled:opacity-50"
          :disabled="readonly || busy.token"
          @keyup.enter="saveToken"
        />
        <button
          class="btn-press rounded-lg bg-brand px-3.5 py-1.5 text-xs font-medium text-white shadow-xs hover:bg-brand-deep disabled:opacity-50"
          :disabled="readonly || busy.token || !form.token.trim()"
          @click="saveToken"
        >
          {{ busy.token ? "保存中…" : "更新凭证" }}
        </button>
        <button
          v-if="cfg?.has_token"
          class="btn-press rounded-lg border border-edge-strong bg-panel px-3 py-1.5 text-xs font-medium text-dim hover:bg-panel-hover disabled:opacity-50"
          :disabled="readonly || busy.token"
          @click="removeToken"
        >
          移除
        </button>
      </div>
      <p v-if="msg.token" class="mt-1.5 text-[11px] text-crit">{{ msg.token }}</p>
    </div>

    <!-- 分组二：轮询与告警策略 -->
    <div class="divide-y divide-edge/60 rounded-2xl border border-edge bg-panel px-4 py-1 shadow-card inset-highlight">
      <!-- 轮询周期 -->
      <div class="flex items-center justify-between py-3">
        <div class="pr-4">
          <span class="text-xs font-medium text-ink">探测轮询周期</span>
          <p class="text-[11px] text-faint">合法区间为 30s 至 24h。建议设定为 5m ~ 15m。</p>
        </div>
        <div class="flex items-center gap-2">
          <input
            v-model="form.interval"
            placeholder="如 5m"
            class="w-20 rounded-lg border border-edge bg-well/60 px-2.5 py-1 text-right text-xs text-ink outline-none placeholder:text-faint focus:border-brand focus:bg-panel tnum disabled:opacity-50"
            :disabled="readonly || busy.cfg"
            @keyup.enter="saveKey('interval', form.interval)"
          />
          <span class="w-16 text-right text-[11px] text-dim tnum">生效中: {{ cfg?.interval }}</span>
          <button
            class="btn-press rounded-md border border-edge bg-panel px-2 py-1 text-xs font-medium text-ink hover:bg-panel-hover disabled:opacity-40"
            :disabled="readonly || busy.cfg || !form.interval"
            @click="saveKey('interval', form.interval)"
          >
            保存
          </button>
        </div>
      </div>
      <p v-if="msg.interval" class="py-1 text-[11px] text-crit">{{ msg.interval }}</p>

      <!-- 告警梯级 -->
      <div class="flex items-center justify-between py-3">
        <div class="pr-4">
          <span class="text-xs font-medium text-ink">配额触发梯级</span>
          <p class="text-[11px] text-faint">逗号分隔的百分比数值（1-99），跨越该刻度即向系统推送通知。</p>
        </div>
        <div class="flex items-center gap-2">
          <input
            v-model="form.thresholds"
            placeholder="如 50,60,80,90"
            class="w-28 rounded-lg border border-edge bg-well/60 px-2.5 py-1 text-right text-xs text-ink outline-none placeholder:text-faint focus:border-brand focus:bg-panel tnum disabled:opacity-50"
            :disabled="readonly || busy.cfg"
            @keyup.enter="saveKey('thresholds', form.thresholds)"
          />
          <span class="w-16 text-right text-[11px] text-dim tnum truncate" :title="(cfg?.thresholds ?? []).join(',')">
            {{ (cfg?.thresholds ?? []).join(",") }}
          </span>
          <button
            class="btn-press rounded-md border border-edge bg-panel px-2 py-1 text-xs font-medium text-ink hover:bg-panel-hover disabled:opacity-40"
            :disabled="readonly || busy.cfg || !form.thresholds"
            @click="saveKey('thresholds', form.thresholds)"
          >
            保存
          </button>
        </div>
      </div>
      <p v-if="msg.thresholds" class="py-1 text-[11px] text-crit">{{ msg.thresholds }}</p>

      <!-- 滞回容差 -->
      <div class="flex items-center justify-between py-3">
        <div class="pr-4">
          <span class="text-xs font-medium text-ink">防抖滞回百分点</span>
          <p class="text-[11px] text-faint">当用量跌破（最低警戒线 − N%）时才重置并允许再次触发该梯级告警。</p>
        </div>
        <div class="flex items-center gap-2">
          <input
            v-model="form.hysteresis"
            placeholder="如 5"
            class="w-16 rounded-lg border border-edge bg-well/60 px-2.5 py-1 text-right text-xs text-ink outline-none placeholder:text-faint focus:border-brand focus:bg-panel tnum disabled:opacity-50"
            :disabled="readonly || busy.cfg"
            @keyup.enter="saveKey('hysteresis', form.hysteresis)"
          />
          <span class="w-16 text-right text-[11px] text-dim tnum">生效中: {{ cfg?.hysteresis }}%</span>
          <button
            class="btn-press rounded-md border border-edge bg-panel px-2 py-1 text-xs font-medium text-ink hover:bg-panel-hover disabled:opacity-40"
            :disabled="readonly || busy.cfg || !form.hysteresis"
            @click="saveKey('hysteresis', form.hysteresis)"
          >
            保存
          </button>
        </div>
      </div>
      <p v-if="msg.hysteresis" class="py-1 text-[11px] text-crit">{{ msg.hysteresis }}</p>

      <!-- 静默通知 -->
      <div class="flex items-center justify-between py-3">
        <div class="pr-4">
          <span class="text-xs font-medium text-ink">静默通知推送</span>
          <p class="text-[11px] text-faint">保留系统桌面弹窗横幅，但静默通知音效，适合专注编码环境。</p>
        </div>
        <button
          class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none disabled:opacity-50"
          :class="(cfg?.silent ?? false) ? 'bg-brand' : 'bg-edge-strong'"
          :disabled="readonly"
          @click="toggleSilent"
        >
          <span
            class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow-sm ring-0 transition duration-200 ease-in-out"
            :class="(cfg?.silent ?? false) ? 'translate-x-4' : 'translate-x-0'"
          />
        </button>
      </div>
      <p v-if="msg.silent" class="py-1 text-[11px] text-crit">{{ msg.silent }}</p>
    </div>

    <!-- 分组三：环境与桌面集成 -->
    <div class="divide-y divide-edge/60 rounded-2xl border border-edge bg-panel px-4 py-1 shadow-card inset-highlight">
      <!-- 开机自启 -->
      <div class="flex items-center justify-between py-3">
        <div class="pr-4">
          <span class="text-xs font-medium text-ink">登录时自启动</span>
          <p class="text-[11px] text-faint">跟随 Windows 系统启动并在后台以常驻托盘图标静默就绪。</p>
        </div>
        <button
          class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none disabled:opacity-50"
          :class="(store.state?.autostart ?? false) ? 'bg-brand' : 'bg-edge-strong'"
          @click="toggleAutostart"
        >
          <span
            class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow-sm ring-0 transition duration-200 ease-in-out"
            :class="(store.state?.autostart ?? false) ? 'translate-x-4' : 'translate-x-0'"
          />
        </button>
      </div>
      <p v-if="msg.autostart" class="py-1 text-[11px] text-crit">{{ msg.autostart }}</p>

      <!-- 数据存储目录 -->
      <div class="flex items-center justify-between py-3">
        <span class="text-xs font-medium text-ink">本地缓存与数据库路径</span>
        <span class="max-w-md truncate rounded bg-well px-2 py-0.5 text-[11px] text-dim tnum" :title="cfg?.dir">
          {{ cfg?.dir }}
        </span>
      </div>
    </div>
  </div>
</template>