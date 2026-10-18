<script setup lang="ts">
defineProps<{
  show: boolean;
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "export"): void;
  (e: "import"): void;
}>();
</script>

<template>
  <div v-if="show" class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 backdrop-blur-2xs">
    <div class="bg-white rounded-2xl shadow-xl border border-slate-200 w-full max-w-md p-6 overflow-hidden">
      <div class="flex items-center justify-between pb-3 border-b border-slate-100">
        <h3 class="font-bold text-slate-900 text-sm">自启动项整仓备份与恢复</h3>
        <button @click="emit('close')" class="text-slate-400 hover:text-slate-600 cursor-pointer">
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>

      <div class="py-4 space-y-3 text-xs">
        <div class="p-4 rounded-xl border border-slate-200 hover:border-blue-400 transition-colors flex items-center justify-between">
          <div>
            <div class="font-semibold text-slate-800">导出完整备份 (.reg / .json)</div>
            <div class="text-slate-400 text-2xs mt-0.5">将当前全部 4 个物理位置的注册表自启动项导出为文件</div>
          </div>
          <button
            @click="emit('export')"
            class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg shadow-xs cursor-pointer shrink-0"
          >
            导出文件
          </button>
        </div>

        <div class="p-4 rounded-xl border border-slate-200 hover:border-blue-400 transition-colors flex items-center justify-between">
          <div>
            <div class="font-semibold text-slate-800">导入并还原备份</div>
            <div class="text-slate-400 text-2xs mt-0.5">从此前导出的备份文件中批量写入并补齐启动项</div>
          </div>
          <button
            @click="emit('import')"
            class="px-3 py-1.5 bg-slate-100 hover:bg-slate-200 text-slate-700 font-medium rounded-lg cursor-pointer shrink-0"
          >
            选择文件导入
          </button>
        </div>
      </div>

      <div class="pt-2 border-t border-slate-100 flex justify-end">
        <button
          @click="emit('close')"
          class="px-4 py-1.5 rounded-lg border border-slate-200 text-slate-600 hover:bg-slate-50 font-medium text-xs cursor-pointer"
        >
          关闭
        </button>
      </div>
    </div>
  </div>
</template>