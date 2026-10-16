import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const target = process.env.RX_UI_TARGET ?? "http://localhost:8148";

export default defineConfig({
  base: "/",
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../../internal/receiver/localui/dist",
    emptyOutDir: true,
    // Fonts and the logo are tiny; keep them as files so the Go side can cache them.
    assetsInlineLimit: 0,
    chunkSizeWarningLimit: 600,
  },
  server: {
    proxy: {
      "/api": {
        target,
        changeOrigin: true,
        // Keep the SSE stream flowing: no buffering, no compression on the way through.
        configure: (proxy) => {
          proxy.on("proxyRes", (res) => {
            if (String(res.headers["content-type"] ?? "").startsWith("text/event-stream")) {
              res.headers["cache-control"] = "no-cache";
              res.headers["x-accel-buffering"] = "no";
            }
          });
        },
      },
    },
  },
});
