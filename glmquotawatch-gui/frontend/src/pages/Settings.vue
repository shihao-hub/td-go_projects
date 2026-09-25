<script setup lang="ts">
// Settings：token（脱敏显示/录入/清除）+ interval/thresholds/hysteresis/silent +
// 开机自启 + 数据目录展示。演示模式下全部只读（服务端已拒，前端同步置灰）。
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
  // service.Error 序列化为 "code: message"，直接展示 message 部分
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
  <div class="mx-auto flex max-w-xl flex-col gap-4">
    <p v-if="readonly" class="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-[11px] text-amber-300">
      演示模式下配置只读（demo_readonly），退出演示后可修改。
    </p>

    <!-- token -->
    <section class="rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
      <h2 class="text-xs font-semibold text-zinc-300">API Token</h2>
      <div class="mt-1 flex items-center gap-2 text-[11px]">
        <span v-if="cfg?.has_token" class="rounded border border-emerald-500/40 bg-emerald-500/10 px-1.5 py-0.5 text-emerald-400 tnum">
          {{ cfg?.token }}
        </span>
        <span v-else class="text-zinc-500">未配置</span>
      </div>
      <div class="mt-3 flex gap-2">
        <input
          v-model="form.token"
          type="password"
          placeholder="粘贴 bigmodel 开放平台密钥（≥ 20 字符）"
          class="min-w-0 flex-1 rounded-lg border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-200 outline-none placeholder:text-zinc-600 focus:border-sky-500 disabled:opacity-50"
          :disabled="readonly || busy.token"
          @keyup.enter="saveToken"
        />
        <button
          class="rounded-lg bg-sky-500 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-sky-400 disabled:opacity-50"
          :disabled="readonly || busy.token || !form.token.trim()"
          @click="saveToken"
        >
          保存
        </button>
        <button
          v-if="cfg?.has_token"
          class="rounded-lg border border-zinc-700 px-3 py-1.5 text-xs text-zinc-300 transition-colors hover:bg-zinc-800 disabled:opacity-50"
          :disabled="readonly || busy.token"
          @click="removeToken"
        >
          清除
        </button>
      </div>
      <p v-if="msg.token" class="mt-1.5 text-[11px] text-red-400">{{ msg.token }}</p>
    </section>

    <!-- 采样配置 -->
    <section class="flex flex-col gap-3 rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
      <h2 class="text-xs font-semibold text-zinc-300">采样与告警</h2>

      <label class="flex items-center justify-between gap-3 text-xs text-zinc-400">
        <span>采样间隔（30s – 24h）</span>
        <span class="flex items-center gap-2">
          <input
            v-model="form.interval"
            placeholder="如 5m"
            class="w-24 rounded-lg border border-zinc-700 bg-zinc-900 px-2.5 py-1 text-right text-zinc-200 outline-none placeholder:text-zinc-600 focus:border-sky-500 disabled:opacity-50 tnum"
            :disabled="readonly || busy.cfg"
            @keyup.enter="saveKey('interval', form.interval)"
          />
          <span class="w-14 text-right tnum text-zinc-500">当前 {{ cfg?.interval }}</span>
          <button
            class="rounded-md border border-zinc-700 px-2 py-1 text-zinc-300 transition-colors hover:bg-zinc-800 disabled:opacity-40"
            :disabled="readonly || busy.cfg || !form.interval"
            @click="saveKey('interval', form.interval)"
          >保存</button>
        </span>
      </label>
      <p v-if="msg.interval" class="text-[11px] text-red-400">{{ msg.interval }}</p>

      <label class="flex items-center justify-between gap-3 text-xs text-zinc-400">
        <span>告警阈值（逗号分隔 1-99）</span>
        <span class="flex items-center gap-2">
          <input
            v-model="form.thresholds"
            placeholder="如 50,60,80,90"
            class="w-28 rounded-lg border border-zinc-700 bg-zinc-900 px-2.5 py-1 text-right text-zinc-200 outline-none placeholder:text-zinc-600 focus:border-sky-500 disabled:opacity-50 tnum"
            :disabled="readonly || busy.cfg"
            @keyup.enter="saveKey('thresholds', form.thresholds)"
          />
          <span class="w-14 text-right tnum text-zinc-500">当前 {{ (cfg?.thresholds ?? []).join(",") }}</span>
          <button
            class="rounded-md border border-zinc-700 px-2 py-1 text-zinc-300 transition-colors hover:bg-zinc-800 disabled:opacity-40"
            :disabled="readonly || busy.cfg || !form.thresholds"
            @click="saveKey('thresholds', form.thresholds)"
          >保存</button>
        </span>
      </label>
      <p v-if="msg.thresholds" class="text-[11px] text-red-400">{{ msg.thresholds }}</p>

      <label class="flex items-center justify-between gap-3 text-xs text-zinc-400">
        <span>滞回百分点（跌破最低档 − N 才重置告警）</span>
        <span class="flex items-center gap-2">
          <input
            v-model="form.hysteresis"
            placeholder="如 5"
            class="w-14 rounded-lg border border-zinc-700 bg-zinc-900 px-2.5 py-1 text-right text-zinc-200 outline-none placeholder:text-zinc-600 focus:border-sky-500 disabled:opacity-50 tnum"
            :disabled="readonly || busy.cfg"
            @keyup.enter="saveKey('hysteresis', form.hysteresis)"
          />
          <span class="w-14 text-right tnum text-zinc-500">当前 {{ cfg?.hysteresis }}</span>
          <button
            class="rounded-md border border-zinc-700 px-2 py-1 text-zinc-300 transition-colors hover:bg-zinc-800 disabled:opacity-40"
            :disabled="readonly || busy.cfg || !form.hysteresis"
            @click="saveKey('hysteresis', form.hysteresis)"
          >保存</button>
        </span>
      </label>
      <p v-if="msg.hysteresis" class="text-[11px] text-red-400">{{ msg.hysteresis }}</p>

      <label class="flex items-center justify-between text-xs text-zinc-400">
        <span>通知静音（仍弹通知，不响铃）</span>
        <button
          class="relative h-5 w-9 rounded-full transition-colors disabled:opacity-50"
          :class="(cfg?.silent ?? false) ? 'bg-sky-500' : 'bg-zinc-700'"
          :disabled="readonly"
          @click="toggleSilent"
        >
          <span
            class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all"
            :class="(cfg?.silent ?? false) ? 'left-4.5' : 'left-0.5'"
          />
        </button>
      </label>
      <p v-if="msg.silent" class="text-[11px] text-red-400">{{ msg.silent }}</p>
    </section>

    <!-- 系统集成 -->
    <section class="flex flex-col gap-3 rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
      <h2 class="text-xs font-semibold text-zinc-300">系统集成</h2>
      <label class="flex items-center justify-between text-xs text-zinc-400">
        <span>开机自启（登录后托盘静默运行）</span>
        <button
          class="relative h-5 w-9 rounded-full transition-colors disabled:opacity-50"
          :class="(store.state?.autostart ?? false) ? 'bg-sky-500' : 'bg-zinc-700'"
          @click="toggleAutostart"
        >
          <span
            class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all"
            :class="(store.state?.autostart ?? false) ? 'left-4.5' : 'left-0.5'"
          />
        </button>
      </label>
      <p v-if="msg.autostart" class="text-[11px] text-red-400">{{ msg.autostart }}</p>
      <div class="text-[11px] text-zinc-500">
        数据目录：<span class="tnum">{{ cfg?.dir }}</span>
      </div>
    </section>
  </div>
</template>
