import { defineConfig, loadEnv, lazyPlugins } from "vite-plus";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => ({
  fmt: {
    insertFinalNewline: true,
    endOfLine: "lf",
    useTabs: false,
    tabWidth: 2,
    sortPackageJson: false,
    ignorePatterns: [
      "**/node_modules/**",
      "**/dist/**",
      "**/build/**",
      "**/target/**",
      "**/vendor/**",
      "**/*.md",
      "**/*.mdx",
      "**/package-lock.json",
      "**/pnpm-lock.yaml",
    ],
  },
  staged: { "*": "vp check --fix" },
  lint: {
    ignorePatterns: [
      "**/node_modules/**",
      "**/dist/**",
      "**/build/**",
      "**/target/**",
      "**/vendor/**",
      "**/*.md",
      "**/*.mdx",
      "**/package-lock.json",
      "**/pnpm-lock.yaml",
    ],
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
    rules: { "vite-plus/prefer-vite-plus-imports": "error" },
    options: { typeAware: true, typeCheck: true },
  },
  plugins: lazyPlugins(() => [react()]),
  server: {
    host: "0.0.0.0",
    strictPort: true,
    proxy: {
      "/api": {
        target: loadEnv(mode, ".", "NOX_").NOX_API_PROXY_TARGET || "http://127.0.0.1:8080",
        ws: true,
      },
      "/auth/nox": {
        target: loadEnv(mode, ".", "NOX_").NOX_API_PROXY_TARGET || "http://127.0.0.1:8080",
      },
    },
  },
}));
