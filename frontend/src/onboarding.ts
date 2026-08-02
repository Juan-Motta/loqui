// The onboarding tutorial: six steps, shown once on the first launch and reopenable from the footer.
//
// PORTED FROM the Electron wizard (openWizard / showStep / renderWiz*), class names included — the
// stylesheet in index.html is the original's, so `.step.active`, `.dots .dot.on`, `.wiz-engine.on`
// and `.conn-state <state>` are what make it look like anything at all.
//
// IT DECIDES NOTHING ITSELF. The engine rows, the language controls, the shortcut rules and the
// permission rows all arrive from the payload that Go already builds and tests, and the steps here
// mount the SAME components the Ajustes tabs use. That is deliberate: the wizard showing different
// engine states from the Conexiones tab would be worse than no wizard.
import { Events } from "@wailsio/runtime";
import * as Settings from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/settingsservice.js";
import type {
  SettingsPayload,
  WriteResult,
} from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/models.js";
import { renderLanguageInto } from "./language.js";
import { renderTriggerInto } from "./system.js";
import { addPermissionsMount, refreshPermissions } from "./permissions.js";

const $ = <T extends HTMLElement>(id: string) =>
  document.getElementById(id) as T | null;

const STEPS = 6;
let step = 0;
let payload: SettingsPayload | null = null;

// What the page does after a write inside the wizard: the caller owns repainting the rest of the
// window, because a change made here (engine, appearance, shortcut) must show up in Ajustes too.
//
// It answers whether the snapshot was APPLIED. The page drops payloads older than the one already
// drawn, and the wizard has to drop the same ones: keeping a rejected snapshot here would leave the
// window correct and the wizard showing the engine and preferences from before it.
type OnSaved = (p: SettingsPayload) => boolean;
let onSaved: OnSaved = () => true;
export function setOnboardingSaveHandler(fn: OnSaved): void {
  onSaved = fn;
}

async function run(action: () => Promise<WriteResult>): Promise<void> {
  try {
    const res = await action();
    // Stored and drawn only if the page accepted it. A snapshot it declined is older than what is
    // already on screen, and the wizard reads from the same state.
    if (onSaved(res.payload)) {
      payload = res.payload;
      render();
    }
  } catch {
    /* the control simply stays as it was; the payload is the source of truth */
  }
}

// ---- steps ---------------------------------------------------------------------------

function showStep(next: number): void {
  step = Math.max(0, Math.min(STEPS - 1, next));
  for (const el of document.querySelectorAll<HTMLElement>("#onboarding .step")) {
    el.classList.toggle("active", Number(el.dataset.step) === step);
  }

  const dots = $<HTMLElement>("wizDots");
  if (dots) {
    dots.innerHTML = "";
    for (let i = 0; i < STEPS; i++) {
      const dot = document.createElement("span");
      dot.className = "dot" + (i === step ? " on" : "");
      dots.appendChild(dot);
    }
  }

  // Back is hidden rather than disabled on the first step: a dead button invites clicking.
  const back = $<HTMLElement>("wizBack");
  if (back) back.style.visibility = step === 0 ? "hidden" : "";
  const next_ = $<HTMLElement>("wizNext");
  if (next_) next_.textContent = step === STEPS - 1 ? "Empezar" : "Continuar";
  // Nothing left to skip on the last step, where Skip and Continue would do the same thing.
  const skip = $<HTMLElement>("wizSkip");
  if (skip) skip.style.visibility = step === STEPS - 1 ? "hidden" : "";

  render();
  // The permission states are read fresh when that step is reached: the user may have granted them in
  // System Settings while this window sat open, which is exactly what the step asks them to do.
  if (step === 1) void refreshPermissions();

  Events.Emit("ui:wizard", { step, steps: STEPS, open: isOpen() });
}

function render(): void {
  if (!payload) return;
  renderEngines(payload);
  renderConfig(payload);
  renderPrefs(payload);
}

// ---- step 2: the engines -------------------------------------------------------------

function renderEngines(p: SettingsPayload): void {
  const box = $<HTMLElement>("wizEngines");
  if (!box) return;
  box.innerHTML = "";
  for (const row of p.connections ?? []) {
    const b = document.createElement("button");
    b.type = "button";
    const on = row.id === p.provider;
    b.className = "wiz-engine" + (on ? " on" : "");
    b.setAttribute("aria-pressed", String(on));

    const top = document.createElement("div");
    top.className = "we-top";
    const name = document.createElement("span");
    name.className = "we-name";
    name.textContent = row.name;
    // The state travels as a CLASS: that is what colours the dot. Text alone would make every engine
    // look equally ready, which is the mistake the Conexiones port already made once.
    const state = document.createElement("span");
    state.className = "conn-state " + row.state;
    const dot = document.createElement("span");
    dot.className = "dot";
    state.append(dot, document.createTextNode(row.label));
    top.append(name, state);

    const kind = document.createElement("div");
    kind.className = "we-kind";
    kind.textContent = row.kind;
    b.append(top, kind);

    // An engine this build cannot drive is disabled, not silently rejected on click.
    if (row.state === "unsupported") b.disabled = true;
    else {
      b.onclick = () =>
        // A plain switch, like "Usar este motor": an unconfigured engine can be chosen here and
        // configured on the very next step, so this must not demand a full valid configuration.
        void run(() => Settings.SetProvider(row.id));
    }
    box.appendChild(b);
  }
}

// ---- step 3: credentials + language --------------------------------------------------

function renderConfig(p: SettingsPayload): void {
  const box = $<HTMLElement>("wizConfig");
  if (!box) return;
  box.innerHTML = "";

  const sub = $<HTMLElement>("wizConfigSub");
  if (sub) sub.textContent = p.providerHint ?? "";

  // The active engine's row carries both slots it needs — the key's and the language's — resolved in
  // Go. Neither is derived here: Azure's depend on its sub-service, and getting that wrong stores a
  // credential where dictation will never look for it.
  const row = (p.connections ?? []).find((c) => c.id === p.provider);
  const slot = row?.keySlot
    ? (p.keys ?? []).find((k) => k.slot === row.keySlot)
    : undefined;
  if (slot) {
    const label = document.createElement("label");
    label.textContent = "Clave de API";
    const input = document.createElement("input");
    input.type = "password";
    // "present" is the value store.KeyStatus carries. The vocabulary belongs to Go: a word invented
    // here would compare equal to nothing, and the placeholder would tell someone who already has a
    // key stored to paste one.
    input.placeholder =
      slot.status === "present" ? "Ya configurada — escribe para reemplazarla" : "Pega tu clave";
    input.autocomplete = "off";
    // On change, not on every keystroke: a keystroke-by-keystroke write would store dozens of
    // truncated prefixes in the Keychain.
    input.onchange = () => {
      const value = input.value.trim();
      if (value === "") return;
      input.value = "";
      void run(() => Settings.SetKey(slot.slot, value));
    };
    box.append(label, input);
  }

  // The per-engine language control, mounted from the same component Ajustes uses, found through the
  // row's langSlot for the same reason.
  const control = (p.languageControls ?? []).find((c) => c.slot === row?.langSlot);
  if (control) {
    const holder = document.createElement("div");
    holder.className = "conn-lang";
    box.appendChild(holder);
    renderLanguageInto(holder, control);
  }
}

// ---- step 4: preferences -------------------------------------------------------------

function renderPrefs(p: SettingsPayload): void {
  const box = $<HTMLElement>("wizPrefs");
  if (!box) return;
  box.innerHTML = "";

  const appearanceLabel = document.createElement("label");
  appearanceLabel.textContent = "Apariencia de la aplicación";
  const seg = document.createElement("div");
  seg.className = "seg";
  seg.setAttribute("role", "radiogroup");
  for (const [value, text] of [
    ["system", "Sistema"],
    ["light", "Claro"],
    ["dark", "Oscuro"],
  ]) {
    const label = document.createElement("label");
    const radio = document.createElement("input");
    radio.type = "radio";
    // A name of its own: sharing "appearance" with the Sistema tab would make the two controls one
    // radio group, and checking either would uncheck the other's rendering.
    radio.name = "wizAppearance";
    radio.value = value;
    radio.checked = p.appearance === value;
    radio.addEventListener("change", () => {
      if (radio.checked) void run(() => Settings.SetAppearance(value));
    });
    const span = document.createElement("span");
    span.textContent = text;
    label.append(radio, span);
    seg.appendChild(label);
  }
  box.append(appearanceLabel, seg);

  const langLabel = document.createElement("label");
  langLabel.textContent = "Idioma de la interfaz";
  const lang = document.createElement("select");
  lang.innerHTML =
    `<option value="">Seguir el sistema</option>` +
    `<option value="es">Español</option>` +
    `<option value="en">Inglés</option>`;
  lang.value = p.appLanguage;
  lang.onchange = () => void run(() => Settings.SetAppLanguage(lang.value));
  box.append(langLabel, lang);

  const trigLabel = document.createElement("label");
  trigLabel.textContent = "Atajo de teclado";
  const trig = document.createElement("div");
  trig.className = "trigger-box";
  box.append(trigLabel, trig);
  renderTriggerInto(trig, p);
}

// ---- open / close --------------------------------------------------------------------

export function isOpen(): boolean {
  const el = $<HTMLElement>("onboarding");
  return !!el && !el.hidden;
}

export function openWizard(): void {
  const el = $<HTMLElement>("onboarding");
  if (!el) return;
  el.hidden = false;
  showStep(0);
}

// Marks the tutorial done so it stops opening by itself.
//
// The UI closes even if the write fails. Otherwise a Keychain or disk problem would trap the user
// inside the wizard with no way out — the one failure they could not work around.
async function closeWizard(): Promise<void> {
  try {
    const res = await Settings.SetOnboarded(true);
    if (onSaved(res.payload)) payload = res.payload;
  } catch {
    /* non-fatal, see above */
  }
  const el = $<HTMLElement>("onboarding");
  if (el) el.hidden = true;
  Events.Emit("ui:wizard", { step, steps: STEPS, open: false, closed: true });
}

// Skip keeps every default: local whisper (no key, no internet), the OS interface language, system
// appearance, fn as the trigger. Nothing is left half-configured, so there is nothing to undo.
async function skipWizard(): Promise<void> {
  if (!payload?.provider) {
    try {
      await Settings.SetProvider("whisper");
    } catch {
      /* the stored defaults already cover it */
    }
  }
  await closeWizard();
}

export function paintOnboarding(p: SettingsPayload): void {
  payload = p;
  if (isOpen()) render();
}

export function wireOnboarding(): void {
  // The wizard's Permisos step shows the same rows as the Permisos tab, from the same renderer.
  addPermissionsMount("wizPerms");

  const back = $<HTMLButtonElement>("wizBack");
  if (back) back.onclick = () => showStep(step - 1);
  const next = $<HTMLButtonElement>("wizNext");
  if (next)
    next.onclick = () =>
      step === STEPS - 1 ? void closeWizard() : showStep(step + 1);
  const skip = $<HTMLButtonElement>("wizSkip");
  if (skip) skip.onclick = () => void skipWizard();

  // Reopening from the footer must NOT clear the flag: it is a tutorial, not a reset.
  const open = $<HTMLButtonElement>("openTutorial");
  if (open) open.onclick = () => openWizard();
}

// Dev affordance: drive the wizard without a mouse, same reason as the other probes.
Events.On("debug:wizard", (e: { data: unknown }) => {
  const arg = Array.isArray(e.data) ? e.data[0] : e.data;
  const want = String(arg ?? "open");
  // Clicks the REAL footer button rather than calling openWizard, because those are different
  // claims: the user's complaint was that the button does nothing, and driving the function directly
  // would pass with the button unwired. Falls back only if the button is missing.
  if (want === "open") {
    const btn = $<HTMLButtonElement>("openTutorial");
    if (btn) btn.click();
    else openWizard();
  } else if (want === "next") showStep(step + 1);
  else if (want === "back") showStep(step - 1);
  else if (want === "skip") void skipWizard();
  else if (want === "finish") {
    openWizard();
    showStep(STEPS - 1);
    void closeWizard();
  } else if (/^\d+$/.test(want)) {
    // A step by number, so any panel can be looked at without clicking through the ones before it.
    openWizard();
    showStep(Number(want));
  }
  Events.Emit("ui:wizard", {
    step,
    steps: STEPS,
    open: isOpen(),
    // What actually landed in the visible step, so an empty panel cannot pass as a working one.
    engines: document.querySelectorAll("#wizEngines .wiz-engine").length,
    permRows: document.querySelectorAll("#wizPerms .prow").length,
    prefsControls: document.querySelectorAll("#wizPrefs input, #wizPrefs select").length,
    configControls: document.querySelectorAll("#wizConfig input, #wizConfig select, #wizConfig button").length,
  });
});
