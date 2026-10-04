import { defineConfig } from "vitest/config";
import vite from "./vite.config";
export default defineConfig({ ...vite, test: { include: ["native/**/*.test.ts"], maxWorkers: 2 } });
