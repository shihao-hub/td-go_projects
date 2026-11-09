<script setup lang="ts">
import type { StartupItem } from "../types";

defineProps<{
  items: StartupItem[];
}>();

const emit = defineEmits<{
  (e: "toggle", item: StartupItem): void;
  (e: "locateFile", path: string): void;
  (e: "delete", item: StartupItem): void;
}>();

function formatResult(code?: number) {
  if (!code) return null;
  return `退出码 ${code}`;
}
</script>

<template>
  <div>
    <div class="bg-purple-50/60 border border-purple-200 rounded-xl p-3.5 mb-4 text-xs text-purple-800 flex items-center justify-between">
      <div class="flex items-center space-x-2">
        <svg class="w-4 h-4 text-purple-600 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span>
          聚合扫描系统计划任务中触发条件为「开机 / 登录 / 定时周期（每小时、每天等）」的第三方任务，
          支持无损启停、删除（自动落任务 XML 快照）与定位执行体。
        </span>
      </div>
    </div>

    <div class="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden">
      <div v-if="items.length === 0" class="py-16 text-center text-slate-400 text-xs">
        未发现第三方自启动 / 定时周期计划任务
      </div>
      <table v-else class="w-full text-left text-xs border-collapse">
        <thead>
          <tr class="bg-slate-50/80 border-b border-slate-200 text-slate-500 font-medium">
            <th class="py-3 px-4 w-44">任务 / 触发</th>
            <th class="py-3 px-4">执行操作 / 路径</th>
            <th class="py-3 px-4 w-40">运行时间</th>
            <th class="py-3 px-4 w-44">发布商</th>
            <th class="py-3 px-4 w-16 text-center">状态</th>
            <th class="py-3 px-4 w-20 text-right">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="item in items" :key="item.id" class="hover:bg-slate-50/60 transition-colors group">
            <td class="py-3 px-4 align-middle">
              <div class="font-semibold text-slate-900 truncate max-w-40" :title="item.task_path || item.name">
                {{ item.name }}
              </div>
              <div class="flex items-center flex-wrap gap-1 mt-1">
                <span
                  v-if="item.trigger_desc"
                  class="px-1.5 py-0.5 rounded text-2xs font-medium border bg-purple-50 text-purple-700 border-purple-200"
                  :title="item.task_path"
                >
                  {{ item.trigger_desc }}
                </span>
                <span
                  v-if="!item.file_exists"
                  class="px-1.5 py-0.5 rounded text-2xs font-medium bg-rose-100 text-rose-700 border border-rose-200"
                >
                  无效残留
                </span>
              </div>
            </td>
            <td class="py-3 px-4 align-middle">
              <div class="font-mono text-2xs text-slate-700 truncate max-w-md" :title="item.exe_path">
                {{ item.exe_path || "无执行体" }}
              </div>
              <div v-if="item.args" class="text-2xs text-slate-400 mt-0.5 truncate max-w-md">参数: {{ item.args }}</div>
            </td>
            <td class="py-3 px-4 align-middle">
              <div class="text-2xs text-slate-600" :title="item.next_run ? `下次: ${item.next_run}` : '未排期'">
                下次: {{ item.next_run || "—" }}
              </div>
              <div class="text-2xs text-slate-400 mt-0.5" :title="item.last_run ? `上次: ${item.last_run}` : '从未运行'">
                上次: {{ item.last_run || "—" }}
                <span v-if="formatResult(item.last_result)" class="text-rose-600 font-medium ml-1">
                  {{ formatResult(item.last_result) }}
                </span>
              </div>
            </td>
            <td class="py-3 px-4 align-middle text-slate-600 truncate max-w-40" :title="item.publisher">
              {{ item.publisher }}
            </td>
            <td class="py-3 px-4 text-center align-middle">
              <button
                @click="emit('toggle', item)"
                type="button"
                class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
                :class="item.is_enabled ? 'bg-blue-600' : 'bg-slate-200'"
                :title="item.is_enabled ? '点击停用该任务 (无损害，可随时恢复)' : '点击启用该任务'"
              >
                <span
                  class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow-sm ring-0 transition duration-200 ease-in-out"
                  :class="item.is_enabled ? 'translate-x-4' : 'translate-x-0'"
                />
              </button>
            </td>
            <td class="py-3 px-4 text-right align-middle">
              <div class="flex items-center justify-end space-x-1">
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
                <button
                  @click="emit('delete', item)"
                  class="p-1 text-slate-400 hover:text-rose-600 hover:bg-rose-50 rounded transition-colors cursor-pointer"
                  title="删除 (自动备份任务 XML)"
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
  </div>
</template>
