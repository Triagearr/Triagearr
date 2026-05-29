import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import { paraglideVitePlugin } from "@inlang/paraglide-js";
import path from "node:path";

export default defineConfig({
  plugins: [
    paraglideVitePlugin({
      project: "./project.inlang",
      outdir: "./src/paraglide",
      strategy: ["localStorage", "preferredLanguage", "baseLocale"],
    }),
    TanStackRouterVite({ target: "react", autoCodeSplitting: true }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: { "@": path.resolve(__dirname, "src") },
  },
  build: {
    target: "es2022",
    outDir: "dist",
    assetsDir: "assets",
    emptyOutDir: true,
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    // Fail loudly on a port clash instead of silently sliding to 5174 — a
    // slid port means a second dev stack is already running and the two fight
    // over the daemon/fixture ports.
    strictPort: true,
    proxy: {
      "/api": { target: "http://127.0.0.1:9494", changeOrigin: true },
      "/healthz": { target: "http://127.0.0.1:9494", changeOrigin: true },
    },
  },
});
