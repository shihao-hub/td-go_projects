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
    <div class="bg-blue-50/60 border border-blue-200 rounded-xl p-3.5 mb-4 text-xs text-blue-800 flex items-center justify-between">
      <div class="flex items-center space-x-2">
        <svg class="w-4 h-4 text-blue-600 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span>启动文件夹仅供聚合审计看板查看，解析快捷方式（.lnk）指向的目标执行体与有效性。</span>
      </div>
    </div>

    <div class="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden">
      <div v-if="items.length === 0" class="py-16 text-center text-slate-400 text-xs">
        启动文件夹中暂无项目
      </div>
      <table v-else class="w-full text-left text-xs border-collapse">
        <thead>
          <tr class="bg-slate-50/80 border-b border-slate-200 text-slate-500 font-medium">
            <th class="py-3 px-4 w-48">名称 / 分类</th>
            <th class="py-3 px-4">快捷方式指向 / 目标路径</th>
            <th class="py-3 px-4 w-44">发布商</th>
            <th class="py-3 px-4 w-28 text-center">状态</th>
            <th class="py-3 px-4 w-24 text-right">定位</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="item in items" :key="item.id" class="hover:bg-slate-50/60 transition-colors">
            <td class="py-3 px-4">
              <div class="font-semibold text-slate-900 truncate max-w-44">{{ item.name }}</div>
              <span
                class="inline-block mt-1 text-2xs px-1.5 py-0.5 rounded font-medium border"
                :class="item.source === 'Folder_User' ? 'bg-sky-50 text-sky-700 border-sky-200' : 'bg-indigo-50 text-indigo-700 border-indigo-200'"
              >
                {{ item.source === 'Folder_User' ? '当前用户启动目录' : '所有用户公共目录' }}
              </span>
            </td>
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
                {{ item.is_enabled ? "已启用" : "已禁用" }}
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