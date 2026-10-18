<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import type { StartupItem } from "./types";
import * as service from "../bindings/winstartupmanager/internal/guiapp/startupappservice.js";

import Header from "./components/Header.vue";
import RegistryTable from "./components/RegistryTable.vue";
import FolderTable from "./components/FolderTable.vue";
import TaskTable from "./components/TaskTable.vue";
import AddEditModal from "./components/AddEditModal.vue";
import DeleteModal from "./components/DeleteModal.vue";
import BackupModal from "./components/BackupModal.vue";

const activeTab = ref<"registry" | "folder" | "task">("registry");
const searchQuery = ref("");
const statusFilter = ref<"all" | "enabled" | "disabled" | "invalid">("all");
const sourceFilter = ref<string>("all");

const registryItems = ref<StartupItem[]>([]);
const folderItems = ref<StartupItem[]>([]);
const taskItems = ref<StartupItem[]>([]);

const isAdmin = ref(false);
const loading = ref(false);
const toastMessage = ref("");
const toastType = ref<"success" | "error" | "info">("info");

const showAddEditModal = ref(false);
const isEditing = ref(false);
const editItem = ref<StartupItem | null>(null);

const showDeleteModal = ref(false);
const itemToDelete = ref<StartupItem | null>(null);

const showBackupModal = ref(false);

function showToast(msg: string, type: "success" | "error" | "info" = "info") {
  toastMessage.value = msg;
  toastType.value = type;
  setTimeout(() => {
    if (toastMessage.value === msg) {
      toastMessage.value = "";
    }
  }, 4000);
}

async function loadAll() {
  loading.value = true;
  try {
    const [adminStatus, reg, folders, tasks] = await Promise.all([
      service.IsAdmin().catch(() => false),
      service.ListRegistryItems().catch(() => []),
      service.ListFolderItems().catch(() => []),
      service.ListScheduledTasks().catch(() => []),
    ]);
    isAdmin.value = adminStatus;
    registryItems.value = (reg || []) as StartupItem[];
    folderItems.value = (folders || []) as StartupItem[];
    taskItems.value = (tasks || []) as StartupItem[];
  } catch (err: any) {
    showToast("加载数据失败: " + (err?.message || err), "error");
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  loadAll();
});

const filteredRegistryItems = computed(() => {
  return registryItems.value.filter((item) => {
    const q = searchQuery.value.trim().toLowerCase();
    if (q) {
      const match =
        item.name.toLowerCase().includes(q) ||
        item.exe_path.toLowerCase().includes(q) ||
        item.args.toLowerCase().includes(q) ||
        item.publisher.toLowerCase().includes(q);
      if (!match) return false;
    }

    if (sourceFilter.value !== "all" && item.source !== sourceFilter.value) {
      return false;
    }

    if (statusFilter.value === "enabled" && !item.is_enabled) return false;
    if (statusFilter.value === "disabled" && item.is_enabled) return false;
    if (statusFilter.value === "invalid" && item.file_exists) return false;

    return true;
  });
});

const invalidRegistryCount = computed(() => {
  return registryItems.value.filter((it) => !it.file_exists).length;
});

const enabledRegistryCount = computed(() => {
  return registryItems.value.filter((it) => it.is_enabled).length;
});

async function handleToggle(item: StartupItem) {
  const targetState = !item.is_enabled;
  if (item.is_elevated && !isAdmin.value) {
    showToast("修改系统级 (HKLM) 启动项需要管理员权限，请点击顶栏提权重启", "error");
    return;
  }

  try {
    await service.ToggleItem(item.source as any, item.name, targetState);
    item.is_enabled = targetState;
    showToast(`已${targetState ? "启用" : "禁用"}启动项: ${item.name}`, "success");
  } catch (err: any) {
    showToast("切换状态失败: " + (err?.message || err), "error");
  }
}

async function handleRestartAsAdmin() {
  try {
    await service.RestartAsAdmin();
  } catch (err: any) {
    showToast("以管理员身份重启失败: " + (err?.message || err), "error");
  }
}

function openAddModal() {
  isEditing.value = false;
  editItem.value = null;
  showAddEditModal.value = true;
}

function openEditModal(item: StartupItem) {
  isEditing.value = true;
  editItem.value = item;
  showAddEditModal.value = true;
}

async function selectExeFile() {
  try {
    const chosen = await service.SelectExeFileDialog();
    if (chosen && editItem.value) {
      editItem.value.exe_path = chosen;
    }
  } catch (err: any) {
    showToast("文件选择失败: " + (err?.message || err), "error");
  }
}

async function submitAddEdit(data: { source: string; name: string; exePath: string; args: string }) {
  if (!data.name.trim()) {
    showToast("请输入启动项名称", "error");
    return;
  }
  if (!data.exePath.trim()) {
    showToast("请输入或选择程序路径", "error");
    return;
  }

  try {
    await service.SaveItem(
      data.source as any,
      data.name.trim(),
      data.exePath.trim(),
      data.args.trim()
    );
    showToast(`已成功保存自启动项: ${data.name}`, "success");
    showAddEditModal.value = false;
    await loadAll();
  } catch (err: any) {
    showToast("保存失败: " + (err?.message || err), "error");
  }
}

function promptDelete(item: StartupItem) {
  itemToDelete.value = item;
  showDeleteModal.value = true;
}

async function confirmDelete() {
  if (!itemToDelete.value) return;
  const item = itemToDelete.value;
  try {
    await service.DeleteItem(item.source as any, item.name);
    showToast(`已安全删除 "${item.name}"，快照备份已生成`, "success");
    showDeleteModal.value = false;
    itemToDelete.value = null;
    await loadAll();
  } catch (err: any) {
    showToast("删除失败: " + (err?.message || err), "error");
  }
}

async function locateFile(path: string) {
  try {
    await service.LocateInExplorer(path);
  } catch (err: any) {
    showToast("在资源管理器中定位失败: " + (err?.message || err), "error");
  }
}

async function locateRegistry(source: string, name: string) {
  try {
    await service.LocateInRegistry(source as any, name);
  } catch (err: any) {
    showToast("在注册表中定位失败: " + (err?.message || err), "error");
  }
}

async function exportBackup() {
  try {
    const dest = await service.SelectBackupSaveDialog();
    if (!dest) return;
    await service.ExportBackup(dest);
    showToast(`已成功导出备份文件到: ${dest}`, "success");
    showBackupModal.value = false;
  } catch (err: any) {
    showToast("导出备份失败: " + (err?.message || err), "error");
  }
}

async function importBackup() {
  try {
    const src = await service.SelectBackupOpenDialog();
    if (!src) return;
    await service.ImportBackup(src);
    showToast(`已成功恢复注册表备份: ${src}`, "success");
    showBackupModal.value = false;
    await loadAll();
  } catch (err: any) {
    showToast("恢复备份失败: " + (err?.message || err), "error");
  }
}
</script>

<template>
  <div class="h-full flex flex-col bg-slate-50 text-slate-800">
    <Header
      :is-admin="isAdmin"
      :loading="loading"
      @restart-admin="handleRestartAsAdmin"
      @open-backup="showBackupModal = true"
      @open-add="openAddModal"
      @refresh="loadAll"
    />

    <!-- Navigation & Filter Bar -->
    <div class="bg-white border-b border-slate-200 px-6 py-2.5 flex flex-wrap items-center justify-between gap-3 shrink-0">
      <nav class="flex space-x-1.5">
        <button
          @click="activeTab = 'registry'"
          class="px-3.5 py-1.5 rounded-lg text-xs font-medium transition-all flex items-center space-x-1.5 cursor-pointer"
          :class="activeTab === 'registry' ? 'bg-blue-50 text-blue-700 font-semibold shadow-2xs' : 'text-slate-600 hover:bg-slate-100'"
        >
          <span>注册表启动项</span>
          <span
            class="px-1.5 py-0.2 rounded-full text-2xs"
            :class="activeTab === 'registry' ? 'bg-blue-200 text-blue-800' : 'bg-slate-100 text-slate-500'"
          >
            {{ registryItems.length }}
          </span>
        </button>

        <button
          @click="activeTab = 'folder'"
          class="px-3.5 py-1.5 rounded-lg text-xs font-medium transition-all flex items-center space-x-1.5 cursor-pointer"
          :class="activeTab === 'folder' ? 'bg-blue-50 text-blue-700 font-semibold shadow-2xs' : 'text-slate-600 hover:bg-slate-100'"
        >
          <span>启动文件夹</span>
          <span
            class="px-1.5 py-0.2 rounded-full text-2xs"
            :class="activeTab === 'folder' ? 'bg-blue-200 text-blue-800' : 'bg-slate-100 text-slate-500'"
          >
            {{ folderItems.length }}
          </span>
        </button>

        <button
          @click="activeTab = 'task'"
          class="px-3.5 py-1.5 rounded-lg text-xs font-medium transition-all flex items-center space-x-1.5 cursor-pointer"
          :class="activeTab === 'task' ? 'bg-blue-50 text-blue-700 font-semibold shadow-2xs' : 'text-slate-600 hover:bg-slate-100'"
        >
          <span>计划任务 (只读)</span>
          <span
            class="px-1.5 py-0.2 rounded-full text-2xs"
            :class="activeTab === 'task' ? 'bg-blue-200 text-blue-800' : 'bg-slate-100 text-slate-500'"
          >
            {{ taskItems.length }}
          </span>
        </button>
      </nav>

      <div class="flex items-center space-x-2.5">
        <div class="relative w-56">
          <input
            v-model="searchQuery"
            type="text"
            placeholder="搜索名称、路径、发布商..."
            class="w-full pl-8 pr-3 py-1.5 text-xs bg-slate-50 border border-slate-200 rounded-lg focus:outline-none focus:border-blue-500 focus:bg-white transition-all"
          />
          <svg class="w-3.5 h-3.5 text-slate-400 absolute left-2.5 top-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
        </div>

        <template v-if="activeTab === 'registry'">
          <select
            v-model="statusFilter"
            class="text-xs bg-slate-50 border border-slate-200 rounded-lg px-2.5 py-1.5 focus:outline-none text-slate-700 cursor-pointer"
          >
            <option value="all">所有状态 ({{ registryItems.length }})</option>
            <option value="enabled">仅已启用 ({{ enabledRegistryCount }})</option>
            <option value="disabled">仅已禁用 ({{ registryItems.length - enabledRegistryCount }})</option>
            <option value="invalid">无效残留项 ({{ invalidRegistryCount }})</option>
          </select>

          <select
            v-model="sourceFilter"
            class="text-xs bg-slate-50 border border-slate-200 rounded-lg px-2.5 py-1.5 focus:outline-none text-slate-700 cursor-pointer"
          >
            <option value="all">全物理位置</option>
            <option value="HKCU_Run">HKCU Run (当前用户)</option>
            <option value="HKCU_RunOnce">HKCU RunOnce (单次)</option>
            <option value="HKLM_Run">HKLM Run (全机系统)</option>
            <option value="HKLM_WOW6432Node">HKLM WOW64 (32位全机)</option>
          </select>
        </template>
      </div>
    </div>

    <!-- Main Content -->
    <main class="flex-1 overflow-y-auto px-6 py-4">
      <div v-if="activeTab === 'registry'">
        <!-- Stat Cards -->
        <div class="grid grid-cols-4 gap-3.5 mb-4">
          <div class="bg-white border border-slate-200 rounded-xl p-3.5 shadow-2xs">
            <div class="text-xs text-slate-500 font-medium">总注册表自启项</div>
            <div class="text-xl font-bold text-slate-900 mt-1">{{ registryItems.length }}</div>
          </div>
          <div class="bg-white border border-slate-200 rounded-xl p-3.5 shadow-2xs">
            <div class="text-xs text-emerald-600 font-medium">开机启用中</div>
            <div class="text-xl font-bold text-emerald-700 mt-1">{{ enabledRegistryCount }}</div>
          </div>
          <div class="bg-white border border-slate-200 rounded-xl p-3.5 shadow-2xs">
            <div class="text-xs text-slate-500 font-medium">已禁用 (无损)</div>
            <div class="text-xl font-bold text-slate-700 mt-1">{{ registryItems.length - enabledRegistryCount }}</div>
          </div>
          <div
            class="bg-white border border-slate-200 rounded-xl p-3.5 shadow-2xs"
            :class="{ 'border-rose-200 bg-rose-50/30': invalidRegistryCount > 0 }"
          >
            <div class="text-xs font-medium" :class="invalidRegistryCount > 0 ? 'text-rose-600' : 'text-slate-500'">
              无效残留项
            </div>
            <div class="text-xl font-bold mt-1" :class="invalidRegistryCount > 0 ? 'text-rose-700' : 'text-slate-700'">
              {{ invalidRegistryCount }}
            </div>
          </div>
        </div>

        <RegistryTable
          :items="filteredRegistryItems"
          @toggle="handleToggle"
          @locate-file="locateFile"
          @locate-reg="locateRegistry"
          @edit="openEditModal"
          @delete="promptDelete"
        />
      </div>

      <FolderTable
        v-else-if="activeTab === 'folder'"
        :items="folderItems"
        @locate-file="locateFile"
      />

      <TaskTable
        v-else-if="activeTab === 'task'"
        :items="taskItems"
        @locate-file="locateFile"
      />
    </main>

    <!-- Modals -->
    <AddEditModal
      :show="showAddEditModal"
      :is-editing="isEditing"
      :initial-source="editItem?.source || 'HKCU_Run'"
      :initial-name="editItem?.name || ''"
      :initial-exe-path="editItem?.exe_path || ''"
      :initial-args="editItem?.args || ''"
      @close="showAddEditModal = false"
      @select-exe="selectExeFile"
      @submit="submitAddEdit"
    />

    <DeleteModal
      :show="showDeleteModal"
      :item="itemToDelete"
      @close="showDeleteModal = false"
      @confirm="confirmDelete"
    />

    <BackupModal
      :show="showBackupModal"
      @close="showBackupModal = false"
      @export="exportBackup"
      @import="importBackup"
    />

    <!-- Toast Notification -->
    <transition
      enter-active-class="transform ease-out duration-200 transition"
      enter-from-class="translate-y-2 opacity-0"
      enter-to-class="translate-y-0 opacity-100"
      leave-active-class="transition ease-in duration-150"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-if="toastMessage"
        class="fixed bottom-5 right-5 z-50 max-w-sm px-4 py-2.5 rounded-xl shadow-lg border text-xs font-medium flex items-center space-x-2"
        :class="{
          'bg-emerald-50 text-emerald-800 border-emerald-300': toastType === 'success',
          'bg-rose-50 text-rose-800 border-rose-300': toastType === 'error',
          'bg-slate-800 text-white border-slate-700': toastType === 'info',
        }"
      >
        <span>{{ toastMessage }}</span>
      </div>
    </transition>
  </div>
</template>