import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => ({
  plugins: [react()],
  server: {
    host: "0.0.0.0",
    strictPort: true,
    proxy: {
      "/api": { target: loadEnv(mode, ".", "NOX_").NOX_API_PROXY_TARGET || "http://127.0.0.1:8080", ws: true },
    },
  },
}));
