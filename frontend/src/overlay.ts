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
import * as Settings from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/settingsservice.js";

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
//
// The overlay is a SEPARATE WINDOW with its own module instance, so it fetches the catalogue itself
// rather than sharing the settings page's. It is also created once and lives for the whole session,
// which is why the language is re-read on the `settings:language` event instead of only at startup:
// otherwise a user who switches language would keep seeing this word in the old one until a restart.
const LABELS: Record<string, string> = {
  idle: "",
  listening: "",
  reconnecting: "reconectando…",
  error: "",
};

let overlayCatalog: Record<string, string | undefined> = {};

function tr(key: string): string {
  return key === "" ? "" : (overlayCatalog[key] ?? key);
}

async function loadOverlayCatalog(): Promise<void> {
  try {
    const res = await Settings.Translations();
    overlayCatalog = (res.catalog ?? {}) as Record<string, string | undefined>;
    // Repaint whatever word is on screen right now, so a language change lands immediately rather
    // than at the next state transition.
    const shown = labelEl.dataset.key ?? "";
    labelEl.textContent = tr(shown);
  } catch {
    /* stays in the authored language, which is readable */
  }
}

void loadOverlayCatalog();
Events.On("settings:language", () => void loadOverlayCatalog());

// Static bars with a per-bar level multiplier, so the row reads like an equalizer rather
// than one block moving. Mirrors the Home waveform so the two feel like the same indicator.
//
// The animation-delay is kept for the reduced-motion and transition timing, but there is no
// longer a baseline sweep to be out of phase with: see the CSS for why that pulse was removed.
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
  // `metering` (set once real levels arrive) must survive a re-render, otherwise every state
  // change would knock the bars back to the flat armed line and the meter would appear to die
  // mid-sentence.
  const metering =
    pillEl.classList.contains("metering") && state.status === "listening";
  pillEl.className = state.status + (metering ? " metering" : "");
  // The KEY is remembered on the element, so a language change can retranslate whatever is showing
  // without waiting for the next state transition — this window can sit visible for a whole
  // dictation. The error text comes from Go, already translated there.
  const key = LABELS[state.status] ?? "";
  labelEl.dataset.key = key;
  labelEl.textContent =
    state.status === "error" ? state.error || "error" : tr(key);
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

// Report the real geometry once laid out.
//
// A pill that looks off-centre or larger than it should be cannot be diagnosed by reading the CSS:
// what matters is what the layout engine actually produced inside a 216x60 transparent window. This
// measures it rather than reasoning about it.
requestAnimationFrame(() => {
  const pill = pillEl.getBoundingClientRect();
  const bars = barsEl.getBoundingClientRect();
  Events.Emit("ui:overlay-geometry", {
    window: `${window.innerWidth}x${window.innerHeight}`,
    pill: `${Math.round(pill.width)}x${Math.round(pill.height)} at ${Math.round(pill.left)},${Math.round(pill.top)}`,
    bars: `${Math.round(bars.width)}x${Math.round(bars.height)} at ${Math.round(bars.left)},${Math.round(bars.top)}`,
    labelHidden: getComputedStyle(labelEl).display === "none",
    pillCentredX: Math.round(
      pill.left + pill.width / 2 - window.innerWidth / 2,
    ),
    pillCentredY: Math.round(
      pill.top + pill.height / 2 - window.innerHeight / 2,
    ),
  });
});
