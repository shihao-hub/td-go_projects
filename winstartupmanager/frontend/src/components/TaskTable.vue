<script setup lang="ts">
import type { StartupItem } from "../types";

defineProps<{
  items: StartupItem[];
}>();

const emit = defineEmits<{
  (e: "locateFile", path: string): void;
}>();
</script>

<template>
  <div>
    <div class="bg-purple-50/60 border border-purple-200 rounded-xl p-3.5 mb-4 text-xs text-purple-800 flex items-center justify-between">
      <div class="flex items-center space-x-2">
        <svg class="w-4 h-4 text-purple-600 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span>只读聚合扫描系统计划任务中触发条件为“登录时 (Logon)”或“启动时 (Boot)”的自启动任务项。</span>
      </div>
    </div>

    <div class="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden">
      <div v-if="items.length === 0" class="py-16 text-center text-slate-400 text-xs">
        未发现第三方登录/开机计划任务
      </div>
      <table v-else class="w-full text-left text-xs border-collapse">
        <thead>
          <tr class="bg-slate-50/80 border-b border-slate-200 text-slate-500 font-medium">
            <th class="py-3 px-4 w-52">计划任务名称</th>
            <th class="py-3 px-4">执行操作 / 路径</th>
            <th class="py-3 px-4 w-44">发布商</th>
            <th class="py-3 px-4 w-28 text-center">状态</th>
            <th class="py-3 px-4 w-24 text-right">定位</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="item in items" :key="item.id" class="hover:bg-slate-50/60 transition-colors">
            <td class="py-3 px-4 font-semibold text-slate-900 truncate max-w-48">{{ item.name }}</td>
            <td class="py-3 px-4">
              <div class="font-mono text-2xs text-slate-700 truncate max-w-md">{{ item.exe_path }}</div>
              <div v-if="item.args" class="text-2xs text-slate-400 mt-0.5">参数: {{ item.args }}</div>
            </td>
            <td class="py-3 px-4 text-slate-600 truncate max-w-40">{{ item.publisher }}</td>
            <td class="py-3 px-4 text-center">
              <span
                class="px-2 py-0.5 rounded-full text-2xs font-medium"
                :class="item.is_enabled ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' : 'bg-slate-100 text-slate-500 border border-slate-200'"
              >
                {{ item.is_enabled ? "已启用" : "已停用" }}
              </span>
            </td>
            <td class="py-3 px-4 text-right">
              <button
                v-if="item.file_exists"
                @click="emit('locateFile', item.exe_path)"
                class="p-1 text-slate-400 hover:text-blue-600 hover:bg-blue-50 rounded transition-colors cursor-pointer"
                title="在资源管理器中定位"
              >
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
                </svg>
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>