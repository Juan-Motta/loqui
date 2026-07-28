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
        // index.html IS the settings page. Wails' AssetFileServerFS locates the root of
        // the embedded FS by finding index.html, and without one it fails EVERY request
        // with "no index.html could be found" — including /overlay.html. So the app's
        // main page has to carry that name.
        index: resolve(__dirname, "index.html"),
        overlay: resolve(__dirname, "overlay.html"),
      },
    },
  },
  plugins: [wails("./bindings")],
});
