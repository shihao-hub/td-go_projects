<script setup lang="ts">
import type { StartupItem } from "../types";

defineProps<{
  items: StartupItem[];
}>();

const emit = defineEmits<{
  (e: "toggle", item: StartupItem): void;
  (e: "locateFile", path: string): void;
  (e: "locateReg", source: string, name: string): void;
  (e: "edit", item: StartupItem): void;
  (e: "delete", item: StartupItem): void;
}>();

function sourceBadge(source: string) {
  switch (source) {
    case "HKCU_Run":
      return { label: "HKCU Run", bg: "bg-blue-50 text-blue-700 border-blue-200" };
    case "HKCU_RunOnce":
      return { label: "HKCU RunOnce", bg: "bg-amber-50 text-amber-700 border-amber-200" };
    case "HKLM_Run":
      return { label: "HKLM Run", bg: "bg-purple-50 text-purple-700 border-purple-200" };
    case "HKLM_WOW6432Node":
      return { label: "HKLM WOW64", bg: "bg-indigo-50 text-indigo-700 border-indigo-200" };
    default:
      return { label: source, bg: "bg-slate-50 text-slate-700 border-slate-200" };
  }
}
</script>

<template>
  <div class="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden">
    <div v-if="items.length === 0" class="py-16 text-center text-slate-400 text-xs">
      没有找到匹配的注册表自启动项
    </div>

    <table v-else class="w-full text-left text-xs border-collapse">
      <thead>
        <tr class="bg-slate-50/80 border-b border-slate-200 text-slate-500 font-medium">
          <th class="py-3 px-4 w-16 text-center">状态</th>
          <th class="py-3 px-4 w-48">名称 / 来源</th>
          <th class="py-3 px-4">执行命令 / 目标程序</th>
          <th class="py-3 px-4 w-44">数字签名 / 发布商</th>
          <th class="py-3 px-4 w-36 text-right">操作</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-slate-100">
        <tr
          v-for="item in items"
          :key="item.id"
          class="hover:bg-slate-50/60 transition-colors group"
          :class="{ 'bg-rose-50/20': !item.file_exists }"
        >
          <td class="py-3.5 px-4 text-center align-middle">
            <button
              @click="emit('toggle', item)"
              type="button"
              class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="item.is_enabled ? 'bg-blue-600' : 'bg-slate-200'"
              :title="item.is_enabled ? '点击禁用该项 (无损)' : '点击启用该项 (无损)'"
            >
              <span
                class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow-sm ring-0 transition duration-200 ease-in-out"
                :class="item.is_enabled ? 'translate-x-4' : 'translate-x-0'"
              />
            </button>
          </td>

          <td class="py-3.5 px-4 align-middle">
            <div class="font-semibold text-slate-900 truncate max-w-44" :title="item.name">
              {{ item.name }}
            </div>
            <div class="flex items-center space-x-1.5 mt-1">
              <span class="px-1.5 py-0.5 rounded text-2xs font-medium border" :class="sourceBadge(item.source).bg">
                {{ sourceBadge(item.source).label }}
              </span>
              <span
                v-if="!item.file_exists"
                class="px-1.5 py-0.5 rounded text-2xs font-medium bg-rose-100 text-rose-700 border border-rose-200"
              >
                无效残留
              </span>
            </div>
          </td>

          <td class="py-3.5 px-4 align-middle">
            <div class="font-mono text-2xs text-slate-700 truncate max-w-md" :title="item.raw_command">
              {{ item.raw_command }}
            </div>
            <div v-if="item.args" class="text-2xs text-slate-400 mt-0.5 truncate max-w-md">
              参数: {{ item.args }}
            </div>
          </td>

          <td class="py-3.5 px-4 align-middle">
            <span class="text-xs text-slate-600 truncate block max-w-40" :title="item.publisher">
              {{ item.publisher || "未知" }}
            </span>
          </td>

          <td class="py-3.5 px-4 text-right align-middle">
            <div class="flex items-center justify-end space-x-1 opacity-80 group-hover:opacity-100 transition-opacity">
              <button
                v-if="item.file_exists"
                @click="emit('locateFile', item.exe_path)"
                class="p-1 text-slate-400 hover:text-blue-600 hover:bg-blue-50 rounded transition-colors cursor-pointer"
                title="在文件管理器中定位"
              >
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
                </svg>
              </button>

              <button
                @click="emit('locateReg', item.source, item.name)"
                class="p-1 text-slate-400 hover:text-purple-600 hover:bg-purple-50 rounded transition-colors cursor-pointer"
                title="在注册表编辑器中定位"
              >
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
                </svg>
              </button>

              <button
                @click="emit('edit', item)"
                class="p-1 text-slate-400 hover:text-emerald-600 hover:bg-emerald-50 rounded transition-colors cursor-pointer"
                title="编辑"
              >
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                </svg>
              </button>

              <button
                @click="emit('delete', item)"
                class="p-1 text-slate-400 hover:text-rose-600 hover:bg-rose-50 rounded transition-colors cursor-pointer"
                title="删除 (预备份)"
              >
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>