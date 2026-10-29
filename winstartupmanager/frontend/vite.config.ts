import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";
import wails from "@wailsio/runtime/plugins/vite";

export default defineConfig({
  plugins: [vue(), tailwindcss(), wails("./bindings")],
  build: {
    // 保留 dist/.gitkeep，避免空目录导致 //go:embed 在全新克隆时编译失败；
    // 真正的清理由 build.py 的 clean_dist 负责。
    emptyOutDir: false,
  },
});