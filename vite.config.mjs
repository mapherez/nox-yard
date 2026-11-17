import { defineConfig } from "vite-plus";
import formatOptions from "./web/format-options.json" with { type: "json" };

export default defineConfig({ fmt: formatOptions });
