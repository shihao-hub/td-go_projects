<script setup lang="ts">
import { ref, watch } from "vue";

const props = defineProps<{
  show: boolean;
  isEditing: boolean;
  initialSource: string;
  initialName: string;
  initialExePath: string;
  initialArgs: string;
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "selectExe"): void;
  (e: "submit", data: { source: string; name: string; exePath: string; args: string }): void;
}>();

const form = ref({
  source: "HKCU_Run",
  name: "",
  exePath: "",
  args: "",
});

watch(
  () => props.show,
  (val) => {
    if (val) {
      form.value = {
        source: props.initialSource || "HKCU_Run",
        name: props.initialName || "",
        exePath: props.initialExePath || "",
        args: props.initialArgs || "",
      };
    }
  },
  { immediate: true }
);

function handleSubmit() {
  emit("submit", { ...form.value });
}
</script>

<template>
  <div v-if="show" class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 backdrop-blur-2xs">
    <div class="bg-white rounded-2xl shadow-xl border border-slate-200 w-full max-w-lg p-6 overflow-hidden">
      <div class="flex items-center justify-between pb-3 border-b border-slate-100">
        <h3 class="font-bold text-slate-900 text-sm">
          {{ isEditing ? "编辑自启动项" : "新增自启动项" }}
        </h3>
        <button @click="emit('close')" class="text-slate-400 hover:text-slate-600 cursor-pointer">
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>

      <div class="py-4 space-y-4 text-xs">
        <div>
          <label class="block font-medium text-slate-700 mb-1.5">目标物理注册表域</label>
          <div class="grid grid-cols-2 gap-2">
            <label
              class="flex items-center space-x-2 p-2.5 rounded-xl border cursor-pointer transition-colors"
              :class="form.source === 'HKCU_Run' ? 'bg-blue-50/50 border-blue-400 text-blue-900' : 'border-slate-200 text-slate-600 hover:bg-slate-50'"
            >
              <input type="radio" v-model="form.source" value="HKCU_Run" class="text-blue-600" />
              <span>当前用户 (HKCU Run - 推荐)</span>
            </label>
            <label
              class="flex items-center space-x-2 p-2.5 rounded-xl border cursor-pointer transition-colors"
              :class="form.source === 'HKLM_Run' ? 'bg-purple-50/50 border-purple-400 text-purple-900' : 'border-slate-200 text-slate-600 hover:bg-slate-50'"
            >
              <input type="radio" v-model="form.source" value="HKLM_Run" class="text-purple-600" />
              <span>全机系统 (HKLM Run - 需管理员)</span>
            </label>
          </div>
        </div>

        <div>
          <label class="block font-medium text-slate-700 mb-1">启动项名称 <span class="text-rose-500">*</span></label>
          <input
            v-model="form.name"
            type="text"
            placeholder="例如: MyTool"
            class="w-full px-3 py-2 border border-slate-200 rounded-lg focus:outline-none focus:border-blue-500"
          />
        </div>

        <div>
          <label class="block font-medium text-slate-700 mb-1">程序文件路径 <span class="text-rose-500">*</span></label>
          <div class="flex space-x-2">
            <input
              v-model="form.exePath"
              type="text"
              placeholder="例如: C:\Program Files\App\app.exe"
              class="flex-1 px-3 py-2 border border-slate-200 rounded-lg focus:outline-none focus:border-blue-500 font-mono text-2xs"
            />
            <button
              @click="emit('selectExe')"
              type="button"
              class="px-3 py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 font-medium rounded-lg shrink-0 cursor-pointer"
            >
              浏览...
            </button>
          </div>
        </div>

        <div>
          <label class="block font-medium text-slate-700 mb-1">附加命令行参数 (可选)</label>
          <input
            v-model="form.args"
            type="text"
            placeholder="例如: --minimized --autostart"
            class="w-full px-3 py-2 border border-slate-200 rounded-lg focus:outline-none focus:border-blue-500 font-mono text-2xs"
          />
        </div>
      </div>

      <div class="pt-3 border-t border-slate-100 flex items-center justify-end space-x-2">
        <button
          @click="emit('close')"
          class="px-4 py-1.5 rounded-lg border border-slate-200 text-slate-600 hover:bg-slate-50 font-medium cursor-pointer"
        >
          取消
        </button>
        <button
          @click="handleSubmit"
          class="px-4 py-1.5 rounded-lg bg-blue-600 hover:bg-blue-700 text-white font-medium shadow-xs cursor-pointer"
        >
          保存并生效
        </button>
      </div>
    </div>
  </div>
</template>