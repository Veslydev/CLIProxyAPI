import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { viteSingleFile } from "vite-plugin-singlefile";
import { fileURLToPath } from "node:url";

const resolve = (path: string) => fileURLToPath(new URL(path, import.meta.url));
export default defineConfig({
  plugins: [react(), tailwindcss(), viteSingleFile()],
  publicDir: false,
  css: { postcss: { plugins: [] } },
  resolve: { alias: [
    ...["navigation", "link", "image", "dynamic"].map((name) => ({ find: `next/${name}`, replacement: resolve(`./native/${name}.tsx`) })),
    { find: "next-intl", replacement: resolve("./native/intl.ts") },
    { find: "@", replacement: resolve("./src") },
    { find: "@native", replacement: resolve("./native") },
  ] },
  define: { "process.env.NODE_ENV": JSON.stringify("production") },
  server: { proxy: { "/api/control-plane": "http://127.0.0.1:18347", "/healthz": "http://127.0.0.1:18347" } },
  build: { target: "es2022", assetsInlineLimit: 100000000, cssCodeSplit: false, chunkSizeWarningLimit: 10000 },
});
