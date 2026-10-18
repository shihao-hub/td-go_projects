<script setup lang="ts">
defineProps<{
  isAdmin: boolean;
  loading: boolean;
}>();

const emit = defineEmits<{
  (e: "restartAdmin"): void;
  (e: "openBackup"): void;
  (e: "openAdd"): void;
  (e: "refresh"): void;
}>();
</script>

<template>
  <header class="bg-white border-b border-slate-200 px-6 py-3.5 flex items-center justify-between shrink-0 shadow-xs">
    <div class="flex items-center space-x-3">
      <div class="w-9 h-9 rounded-xl bg-gradient-to-tr from-sky-500 to-blue-600 flex items-center justify-center text-white shadow-sm font-black text-lg">
        🚀
      </div>
      <div>
        <div class="flex items-center space-x-2">
          <h1 class="font-bold text-slate-900 text-base leading-none">WinStartupManager</h1>
          <span
            class="text-xs px-2 py-0.5 rounded-full font-medium"
            :class="isAdmin ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' : 'bg-slate-100 text-slate-600 border border-slate-200'"
          >
            {{ isAdmin ? "管理员权限 (HKLM 可写)" : "普通权限" }}
          </span>
        </div>
        <p class="text-xs text-slate-500 mt-1">Windows 开机自启深度管理与无损启停工具</p>
      </div>
    </div>

    <div class="flex items-center space-x-2.5">
      <button
        v-if="!isAdmin"
        @click="emit('restartAdmin')"
        class="inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg bg-amber-50 text-amber-800 border border-amber-300 hover:bg-amber-100 transition-colors cursor-pointer"
        title="以管理员权限重启以支持修改全机 HKLM 启动项"
      >
        <svg class="w-3.5 h-3.5 mr-1 text-amber-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
        </svg>
        以管理员身份重启
      </button>

      <button
        @click="emit('openBackup')"
        class="inline-flex items-center px-3 py-1.5 text-xs font-medium rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 transition-colors cursor-pointer"
      >
        <svg class="w-3.5 h-3.5 mr-1 text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7H5a2 2 0 00-2 2v9a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-3m-1 4l-3 3m0 0l-3-3m3 3V4" />
        </svg>
        备份 / 恢复
      </button>

      <button
        @click="emit('openAdd')"
        class="inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg bg-blue-600 text-white hover:bg-blue-700 transition-colors shadow-xs cursor-pointer"
      >
        <svg class="w-3.5 h-3.5 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
        </svg>
        新增启动项
      </button>

      <button
        @click="emit('refresh')"
        class="p-1.5 rounded-lg text-slate-500 hover:bg-slate-100 hover:text-slate-700 transition-colors cursor-pointer"
        :class="{ 'animate-spin': loading }"
        title="刷新全部列表"
      >
        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
        </svg>
      </button>
    </div>
  </header>
</template>