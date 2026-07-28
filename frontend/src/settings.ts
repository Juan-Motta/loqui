// Settings / app-shell renderer.
//
// PORT IN PROGRESS. The markup in settings.html is the Electron page verbatim; this
// script is not ported yet. The Electron original (src/settings/settings.ts, 1828
// lines) imports ten pure shared modules — i18n, languageCatalog, connectionStatus,
// permissions, triggerKey, modelSpec, historyFilter, inputDevices, languageSlots,
// settings — because main and renderer shared one language there.
//
// Those rules now live in Go (single source of truth). This file will be ported to
// read them from one bootstrap payload the backend computes, so the DOM code keeps
// its current shape while the decisions stay on the Go side. See
// docs/plans/loqui-go-port.md, phase 4.
//
// Until then the page renders its static markup so the window, the tray and the
// overlay can be exercised end to end.
import { Events } from "@wailsio/runtime";
import * as Settings from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/settingsservice.js";

console.info("[loqui] settings shell loaded (script port pending)");

// The bootstrap payload — the seam the whole ported UI hangs off. Fetched here already, ahead
// of the DOM work, so the round trip is proven in the packaged app rather than only in Go
// tests: the binding is generated code, and a service that fails to register looks perfectly
// healthy from the Go side.
//
// Logged through the SAME channel as the page-load report, because the Go log is where this is
// actually read from; a console.log inside the webview needs devtools open to exist.
Settings.Load().then(
  (payload) => {
    Events.Emit("ui:bootstrap", {
      provider: payload.provider,
      region: payload.region,
      mode: payload.mode,
      triggerKey: payload.triggerKey,
      appearance: payload.appearance,
      // Presence only — the payload never carries a key, and this must not become the
      // place that changes that.
      // The generated DTO types these as nullable because Go maps and slices can marshal to
      // null. Payload() guarantees they never do, but the guards keep the seam honest for a
      // caller that cannot see that guarantee.
      keys: (payload.keys ?? [])
        .map((k) => `${k.slot}=${k.status}${k.fromEnv ? "(env)" : ""}`)
        .join(" "),
      permissions: payload.permissions,
      devices: (payload.inputDevices ?? []).length,
      langSlots: Object.keys(payload.languageBySlot ?? {}).length,
      devicesError: payload.devicesError,
    });
  },
  (err: unknown) => {
    Events.Emit("ui:bootstrap-failed", { error: String(err) });
  },
);

// Tell the backend the page really loaded. Not decoration: a missing index.html put the
// Wails asset server into an error state where EVERY route returned "no index.html could be
// found", and from the Go side that is indistinguishable from a page that loaded fine.
Events.Emit("ui:ready", {
  page: "settings",
  title: document.title,
  views: document.querySelectorAll("section.view").length,
  navItems: document.querySelectorAll(".nav-item").length,
});

// Proof the event plumbing reaches this window: the same channel the ported UI will
// use to animate the Home waveform.
Events.On("dictation:state", (e: { data: boolean | boolean[] }) => {
  const active = Array.isArray(e.data) ? e.data[0] : e.data;
  document.documentElement.dataset.dictating = String(!!active);
});
