// Overlay renderer: a small presence pill. Ported from the Electron renderer
// (src/overlay/overlay.ts) with one deliberate change.
//
// WHAT MOVED: Electron ran the pure `reduceOverlay` reducer HERE, in the renderer,
// because main and renderer shared TypeScript. In the port the backend is Go, so the
// reducer lives there (internal/session/overlay.go) and this file receives the state
// it already computed. The rule now has exactly one implementation.
//
// WHAT DIDN'T CHANGE: the markup, the CSS and the bar animation are byte-identical to
// the Electron overlay, and it still shows NO transcript — that is read at the cursor,
// where the text is injected.
import { Events } from "@wailsio/runtime";

const BARS = 10;

type OverlayStatus = "idle" | "listening" | "reconnecting" | "error";
interface OverlayState {
  status: OverlayStatus;
  error: string | null;
}

let state: OverlayState = { status: "idle", error: null };

const pillEl = document.getElementById("pill") as HTMLElement;
const barsEl = document.getElementById("bars") as HTMLElement;
const labelEl = document.getElementById("label") as HTMLElement;

// Only reconnect/error need words; listening is bars-only.
const LABELS: Record<string, string> = {
  idle: "",
  listening: "",
  reconnecting: "reconectando…",
  error: "",
};

// Static bars with an out-of-phase pulse delay and a per-bar level multiplier,
// so the row reads like an equalizer rather than one block moving. Mirrors the
// Home waveform so the two feel like the same indicator.
function buildBars(): void {
  let html = "";
  for (let i = 0; i < BARS; i++) {
    const delay = (((i * 53) % 80) / 100).toFixed(2);
    const m = (0.55 + (((i * 29) % 50) / 50) * 0.85).toFixed(2);
    html += `<span style="animation-delay:${delay}s;--m:${m}"></span>`;
  }
  barsEl.innerHTML = html;
}

function render(): void {
  // `metering` (set once real levels arrive) must survive a re-render, otherwise
  // every state change would knock the bars back to the pulse baseline.
  const metering = pillEl.classList.contains("metering") && state.status === "listening";
  pillEl.className = state.status + (metering ? " metering" : "");
  labelEl.textContent = state.status === "error" ? state.error || "error" : LABELS[state.status] || "";
}

// Go pushes the already-reduced overlay state.
Events.On("overlay:state", (e: { data: OverlayState | OverlayState[] }) => {
  const next = Array.isArray(e.data) ? e.data[0] : e.data;
  if (!next || next.status === state.status) {
    if (next && next.error === state.error) return; // nothing to repaint
  }
  state = { status: next.status, error: next.error ?? null };
  if (state.status !== "listening") {
    pillEl.classList.remove("metering"); // reset so the next session starts on the pulse
    pillEl.style.removeProperty("--level");
  }
  render();
});

// Live mic level 0..1 → bar heights. Emitted by the Go audio meter.
Events.On("meter:level", (e: { data: number | number[] }) => {
  if (state.status !== "listening") return;
  const level = Array.isArray(e.data) ? e.data[0] : e.data;
  pillEl.classList.add("metering");
  pillEl.style.setProperty("--level", String(Number(level) || 0));
});

buildBars();
render();

Events.Emit("ui:ready", { page: "overlay", title: document.title, bars: BARS });
