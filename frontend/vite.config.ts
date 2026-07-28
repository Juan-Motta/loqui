import { defineConfig } from "vite";
import { resolve } from "node:path";
import wails from "@wailsio/runtime/plugins/vite";

// Two pages, one per window — the same split as the Electron build, minus one.
// Electron needed a THIRD hidden renderer (`engine`) to host the Azure JS SDK and
// getUserMedia; in this port Go owns audio capture and every provider, so that
// window has no reason to exist. See docs/research/2026-07-27-azure-speech-go-macos.md.
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  build: {
    rollupOptions: {
      input: {
        settings: resolve(__dirname, "settings.html"),
        overlay: resolve(__dirname, "overlay.html"),
      },
    },
  },
  plugins: [wails("./bindings")],
});
