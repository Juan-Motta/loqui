// The Sistema tab: appearance, interface language, activation shortcut, mode and input device.
//
// PORTED FROM renderTriggerInto and the surrounding controls, class names included. What this file
// does NOT own: whether an accelerator is valid, whether a mode is possible, or what the note under
// the shortcut says. All of that arrives in the payload from internal/store/trigger.go, because a
// shortcut that fails to register does so SILENTLY — the user presses the key and nothing happens —
// so the rule has to live where it can be tested.
//
// The only DOM-specific piece is turning a keydown into an accelerator string, which is inherently a
// browser concern. Even then the string is sent to Go to be validated and canonicalised.
import { Events } from "@wailsio/runtime";
import { loadTranslations, t } from "./i18n.js";
import { setHistoryLocale } from "./history.js";
import * as Settings from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/settingsservice.js";
import type {
  SettingsPayload,
  WriteResult,
} from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/models.js";

const $ = <T extends HTMLElement>(id: string) =>
  document.getElementById(id) as T | null;

function esc(s: string): string {
  return s.replace(
    /[&<>"]/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c] ?? c,
  );
}

type OnSaved = (p: SettingsPayload) => void;
let onSaved: OnSaved = () => {};
export function setSystemSaveHandler(fn: OnSaved): void {
  onSaved = fn;
}

// run reports into a status element and repaints from the returned payload, success or failure — the
// same contract the rest of the page uses, and the reason every setter returns a WriteResult.
async function run(
  status: HTMLElement | null,
  okText: string,
  action: () => Promise<WriteResult>,
): Promise<void> {
  try {
    const res = await action();
    if (status) {
      status.className = "lang-status " + (res.error ? "err" : "ok");
      status.textContent = res.error ? "✗ " + res.error : okText;
    }
    onSaved(res.payload);
  } catch (err) {
    if (status) {
      status.className = "lang-status err";
      status.textContent = "✗ " + String(err);
    }
  }
}

// ---- segmented controls (appearance, mode) -----------------------------------------

function setSegValue(name: string, value: string): void {
  const el = document.querySelector<HTMLInputElement>(
    `input[name="${name}"][value="${value}"]`,
  );
  if (el) el.checked = true;
}

// ---- the shortcut control -----------------------------------------------------------

// capturing tracks the "press your shortcut" mode. Module-level because the keydown listener is on
// the document: a shortcut is captured by pressing keys, which cannot be scoped to a focused input.
let capturing = false;
let captureBox: HTMLElement | null = null;

// MODIFIER_KEY_NAMES are the keys that mean "still composing". Pressing Shift alone is not a
// shortcut, and treating it as one would capture ⇧ the moment the user reached for ⇧D.
const MODIFIER_KEY_NAMES = [
  "shift",
  "control",
  "alt",
  "meta",
  "altgraph",
  "capslock",
  "fn",
];

const EVENT_KEYS: Record<string, string> = {
  " ": "Space",
  spacebar: "Space",
  arrowup: "Up",
  arrowdown: "Down",
  arrowleft: "Left",
  arrowright: "Right",
  enter: "Return",
  escape: "Escape",
  tab: "Tab",
  backspace: "Backspace",
  delete: "Delete",
  insert: "Insert",
  home: "Home",
  end: "End",
  pageup: "PageUp",
  pagedown: "PageDown",
};

function isFunctionKey(k: string): boolean {
  const m = /^F(\d{1,2})$/.exec(k);
  return !!m && Number(m[1]) >= 1 && Number(m[1]) <= 24;
}

// acceleratorFromEvent maps a keydown into an accelerator, or null when the combination is not a
// usable global shortcut yet.
//
// Ported verbatim, including the fallback to the physical `code`: on layouts where `key` is a dead or
// composed character there is nothing usable in it, and without the fallback those keyboards simply
// could not set a shortcut.
function acceleratorFromEvent(e: KeyboardEvent): string | null {
  const lower = String(e.key || "").toLowerCase();
  if (MODIFIER_KEY_NAMES.includes(lower)) return null;

  const mods: string[] = [];
  if (e.metaKey) mods.push("Command");
  if (e.ctrlKey) mods.push("Control");
  if (e.altKey) mods.push("Alt");
  if (e.shiftKey) mods.push("Shift");

  let key: string | null = EVENT_KEYS[lower] ?? null;
  if (!key && /^[a-z0-9]$/.test(lower)) key = lower.toUpperCase();
  if (!key && isFunctionKey(String(e.key).toUpperCase()))
    key = String(e.key).toUpperCase();
  if (!key) {
    const m = /^Key([A-Z])$|^Digit(\d)$/.exec(String(e.code || ""));
    if (m) key = m[1] || m[2] || null;
  }
  if (!key) return null;

  // A bare ordinary key would swallow that key in every app; function keys exist to be alone.
  if (mods.length === 0 && !isFunctionKey(key)) return null;
  return [...mods, key].join("+");
}

function triggerParts(box: HTMLElement) {
  return {
    chip: box.querySelector<HTMLElement>(".trig-chip"),
    change: box.querySelector<HTMLButtonElement>(".trig-change"),
    reset: box.querySelector<HTMLButtonElement>(".trig-reset"),
    note: box.querySelector<HTMLElement>(".trig-note"),
    status: box.querySelector<HTMLElement>(".trig-status"),
  };
}

function stopCapture(): void {
  capturing = false;
  const box = captureBox;
  captureBox = null;
  if (!box) return;
  const p = triggerParts(box);
  if (p.change) p.change.textContent = "Cambiar";
  if (p.status) {
    p.status.className = "lang-status";
    p.status.textContent = "";
  }
}

function startCapture(box: HTMLElement): void {
  capturing = true;
  captureBox = box;
  const p = triggerParts(box);
  if (p.change) p.change.textContent = "Cancelar";
  if (p.status) {
    p.status.className = "lang-status";
    p.status.textContent = "Pulsa la combinación…";
  }
}

// The capture listener sits on the document in CAPTURE phase, so a shortcut can be recorded without
// first focusing anything — and so the keys do not also reach whatever control has focus.
document.addEventListener(
  "keydown",
  (e) => {
    if (!capturing || !captureBox) return;
    const box = captureBox;
    e.preventDefault();
    e.stopPropagation();

    if (e.key === "Escape") {
      stopCapture();
      return;
    }
    const accel = acceleratorFromEvent(e);
    if (!accel) return; // still composing, or not usable alone

    const status = triggerParts(box).status;
    stopCapture();
    void run(status, "✓ atajo activo", () => Settings.SetTrigger(accel));
  },
  true,
);

// renderTriggerInto draws the shortcut control. The markup is created once and then only updated, so
// the capture handlers are not rebound on every repaint.
// Exported so the tutorial's Preferencias step mounts THIS control rather than a second one: the
// capture rules, the disabled hold radio and the note all come from the payload, and a copy would be
// the same rules written twice.
export function renderTriggerInto(box: HTMLElement, p: SettingsPayload): void {
  if (!box.querySelector(".trig-chip")) {
    box.innerHTML =
      '<div class="trigger-row">' +
      '<span class="keycap trig-chip"></span>' +
      '<button class="btn small trig-change"></button>' +
      '<button class="btn small trig-reset"></button>' +
      "</div>" +
      '<div class="trigger-note trig-note"></div>' +
      '<div class="lang-status trig-status"></div>';
    const parts = triggerParts(box);
    if (parts.change) {
      parts.change.onclick = () =>
        capturing && captureBox === box ? stopCapture() : startCapture(box);
    }
    if (parts.reset) {
      parts.reset.onclick = () =>
        void run(triggerParts(box).status, "✓ atajo activo", () =>
          Settings.SetTrigger("fn"),
        );
    }
  }

  const t = p.trigger;
  const parts = triggerParts(box);
  if (parts.chip) parts.chip.innerHTML = `<kbd>${esc(t.label)}</kbd>`;
  if (parts.change && !capturing) parts.change.textContent = "Cambiar";
  if (parts.reset) {
    parts.reset.textContent = t.resetLabel;
    // Hidden when pressing it would be a no-op — already fn.
    parts.reset.style.display = t.showReset ? "" : "none";
  }
  if (parts.note) parts.note.textContent = t.note;

  // Hold is unavailable unless the trigger can report release, so the radio is DISABLED rather than
  // accepted and silently downgraded. The label is dimmed with it, or the control looks enabled.
  const holdRadio = document.querySelector<HTMLInputElement>(
    'input[name="mode"][value="hold"]',
  );
  if (holdRadio) {
    holdRadio.disabled = !t.supportsHold;
    holdRadio.closest("label")?.classList.toggle("disabled", !t.supportsHold);
  }
}

// ---- painting and wiring ------------------------------------------------------------

export function paintSystem(p: SettingsPayload): void {
  setSegValue("appearance", p.appearance);
  setSegValue("mode", p.mode);

  const appLang = $<HTMLSelectElement>("appLanguage");
  if (appLang) {
    appLang.innerHTML =
      `<option value="">${t("Seguir el sistema")}</option>` +
      `<option value="es">${t("Español")}</option>` +
      `<option value="en">${t("Inglés")}</option>`;
    appLang.value = p.appLanguage;
  }

  const device = $<HTMLSelectElement>("device");
  if (device) {
    const devices = p.inputDevices ?? [];
    // The stored id is kept selectable even when the device is not present: a microphone can be
    // unplugged without the user wanting to forget it, and the capture path falls back to the
    // default meanwhile. Dropping it here would silently reset their choice.
    const listed = devices.some((d) => d.id === p.inputDeviceId);
    const extra =
      p.inputDeviceId !== "" && !listed
        ? `<option value="${esc(p.inputDeviceId)}">(no conectado ahora)</option>`
        : "";
    device.innerHTML =
      `<option value="">Predeterminado del sistema</option>` +
      devices
        .map(
          (d) =>
            `<option value="${esc(d.id)}">${esc(d.name)}${d.default ? " (predeterminado)" : ""}</option>`,
        )
        .join("") +
      extra;
    device.value = p.inputDeviceId;
    // Says WHY the list is short rather than showing an empty picker.
    if (p.devicesError) {
      const status = $<HTMLElement>("ajustesStatus");
      if (status) {
        status.className = "status err";
        status.textContent =
          "No se pudieron enumerar los micrófonos: " + p.devicesError;
      }
    }
  }

  const box = $<HTMLElement>("triggerBox");
  if (box) renderTriggerInto(box, p);

  Events.Emit("ui:system", {
    appearance: p.appearance,
    mode: p.mode,
    trigger: p.trigger.label,
    supportsHold: p.trigger.supportsHold,
    devices: (p.inputDevices ?? []).length,
  });
}

// Dev affordance: exercise one Sistema control without a mouse, same reason as the other probes.
//
// It clicks the REAL radio rather than calling the setter, because those are different claims: with
// no Guardar button, persistence depends entirely on each control's own change listener, and driving
// the binding from Go would pass even if that listener were missing. Falls back to the setter only
// when the radio isn't there, so the probe still reports something instead of silently doing nothing.
Events.On("debug:set-appearance", (e: { data: unknown }) => {
  const arg = Array.isArray(e.data) ? e.data[0] : e.data;
  const value = String(arg ?? "");
  const radio = document.querySelector<HTMLInputElement>(
    `input[name="appearance"][value="${value}"]`,
  );
  if (radio) {
    radio.click(); // fires `change`, so the listener under test is the one a mouse reaches
    Events.Emit("ui:system-probe", { control: "appearance", value, via: "radio" });
    return;
  }
  Events.Emit("ui:system-probe", { control: "appearance", value, via: "binding" });
  void run($<HTMLElement>("ajustesStatus"), "✓ apariencia aplicada", () =>
    Settings.SetAppearance(value),
  );
});

export function wireSystem(): void {
  const status = $<HTMLElement>("ajustesStatus");

  for (const radio of document.querySelectorAll<HTMLInputElement>(
    'input[name="appearance"]',
  )) {
    radio.addEventListener("change", () => {
      if (radio.checked)
        void run(status, "✓ apariencia aplicada", () =>
          Settings.SetAppearance(radio.value),
        );
    });
  }

  for (const radio of document.querySelectorAll<HTMLInputElement>(
    'input[name="mode"]',
  )) {
    radio.addEventListener("change", () => {
      if (radio.checked)
        void run(status, "✓ modo guardado", () =>
          Settings.SetMode(radio.value),
        );
    });
  }

  $<HTMLSelectElement>("appLanguage")?.addEventListener("change", (e) => {
    const value = (e.target as HTMLSelectElement).value;
    // THE ORDER IS WRITE → FETCH → REPAINT, and each step is there because the other two are not
    // enough on their own.
    //
    //  · WRITE first, or the fetch asks Go for a language it has not been told about yet and comes
    //    back with the OLD table. (Fetching first was an attempt at this that made it worse.)
    //  · FETCH next, so the catalogue in the page is the new one.
    //  · REPAINT last, and this is the part a plain applyTranslations() cannot cover: that only
    //    rewrites marked static nodes, while the engine options, the key-state label and the busy
    //    lines are BUILT through t() during a paint. Without a repaint they keep the old language on
    //    an otherwise switched page. Review finding.
    void run(status, "✓ idioma de la interfaz guardado", () =>
      Settings.SetAppLanguage(value),
    )
      .then(() => loadTranslations())
      .catch(() => {
        /* stays in the previous language, which is still readable */
      })
      .then(() => Settings.Load())
      .then((p) => {
        onSaved(p);
        // The history formats its own timestamps and is not part of what paint() rebuilds.
        setHistoryLocale(p.locale || p.appLanguage);
      });
  });

  $<HTMLSelectElement>("device")?.addEventListener("change", (e) => {
    const value = (e.target as HTMLSelectElement).value;
    void run(status, "✓ micrófono guardado", () =>
      Settings.SetInputDevice(value),
    );
  });
}

// Drives the interface-language select from outside, for the E2E that proves a live switch reaches
// the whole window. A real `change` event, so the page's own handler is what runs — an affordance
// that bypassed it would verify itself instead of the app.
Events.On("debug:set-language", (e: { data: unknown }) => {
  const arg = Array.isArray(e.data) ? e.data[0] : e.data;
  const value = String(arg ?? "");
  const select = $<HTMLSelectElement>("appLanguage");
  if (!select) {
    Events.Emit("ui:lang-switch", { error: "no language select" });
    return;
  }
  // VALIDATED against what the control actually offers, and the raw value is never echoed. An
  // unsupported string like "en-US" would otherwise leave the select on its empty option, PERSIST
  // "follow the system" — silently changing a real preference on a typo — and write whatever the
  // environment held into the log. Review finding.
  const allowed = Array.from(select.options).map((o) => o.value);
  if (!allowed.includes(value)) {
    Events.Emit("ui:lang-switch", { rejected: true, allowed: allowed.join("|") });
    return;
  }
  select.value = value;
  select.dispatchEvent(new Event("change"));
  Events.Emit("ui:lang-switch", { requested: value === "" ? "system" : value });
});
