<script setup lang="ts">
import type { StartupItem } from "../types";

defineProps<{
  show: boolean;
  item: StartupItem | null;
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "confirm"): void;
}>();
</script>

<template>
  <div v-if="show && item" class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 backdrop-blur-2xs">
    <div class="bg-white rounded-2xl shadow-xl border border-slate-200 w-full max-w-md p-6 overflow-hidden">
      <div class="flex items-start space-x-3">
        <div class="w-10 h-10 rounded-full bg-rose-100 text-rose-600 flex items-center justify-center shrink-0">
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        </div>
        <div>
          <h3 class="font-bold text-slate-900 text-sm">确认彻底删除该启动项？</h3>
          <p class="text-xs text-slate-500 mt-1">
            您即将从注册表移除启动项 <span class="font-semibold text-slate-800">"{{ item.name }}"</span>。
          </p>
        </div>
      </div>

      <div class="my-4 bg-emerald-50 border border-emerald-200 rounded-xl p-3 text-xs text-emerald-800">
        <div class="font-semibold flex items-center space-x-1.5">
          <svg class="w-4 h-4 text-emerald-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
          </svg>
          <span>预删除安全快照防护 (FR-4)</span>
        </div>
        <p class="mt-1 text-2xs text-emerald-700 leading-relaxed">
          执行删除前系统将自动在本地备份目录生成标准的 <code class="bg-emerald-100/60 px-1 py-0.5 rounded">.reg</code> 恢复脚本，彻底杜绝误删风险。
        </p>
      </div>

      <div class="flex items-center justify-end space-x-2 pt-2 border-t border-slate-100">
        <button
          @click="emit('close')"
          class="px-4 py-1.5 rounded-lg border border-slate-200 text-slate-600 hover:bg-slate-50 font-medium text-xs cursor-pointer"
        >
          取消
        </button>
        <button
          @click="emit('confirm')"
          class="px-4 py-1.5 rounded-lg bg-rose-600 hover:bg-rose-700 text-white font-medium text-xs shadow-xs cursor-pointer"
        >
          安全删除
        </button>
      </div>
    </div>
  </div>
</template>