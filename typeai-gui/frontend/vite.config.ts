import { defineConfig } from "vite";
import wails from "@wailsio/runtime/plugins/vite";

export default defineConfig({
  server: {
    host: "127.0.0.1",
    // 端口与 glmquotawatch-gui（9245）错开，避免并行开发冲突
    port: Number(process.env.WAILS_VITE_PORT) || 9246,
    strictPort: true,
  },
  plugins: [wails("./bindings")],
});
