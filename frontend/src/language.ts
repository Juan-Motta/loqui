// The per-engine dictation-language control inside each Conexiones row.
//
// PORTED FROM renderLanguageInto, class names included. Each engine gets only the control it can
// honour, and the three shapes are not cosmetic — they are three different VALUE SPACES:
//
//	multi         toggleable locale chips for Azure Speech's continuous LID
//	auto-or-one   a select of base codes plus t("Detección automática")
//	one-required  a select of full locales for Apple's engine, which cannot detect
//
// WHAT THIS FILE DOES NOT DECIDE: which shape, which options, or whether a value is valid. All of
// that arrives in the payload from store.LangCapabilityFor / LanguageOptionsFor, and the setter
// validates again on the way in. The page draws and reports; a second opinion here is exactly how a
// picker ends up offering "es" to an engine that needs "es-CO".
import { Events } from "@wailsio/runtime";
import { t } from "./i18n.js";
import * as Settings from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/settingsservice.js";
import type {
  LanguageControl,
  SettingsPayload,
} from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/models.js";

// onSaved is how this module hands the freshly repainted state back, so the whole page repaints from
// one snapshot instead of this control patching itself.
type OnSaved = (p: SettingsPayload) => void;

let onSaved: OnSaved = () => {};
export function setLanguageSaveHandler(fn: OnSaved): void {
  onSaved = fn;
}

// baseOf is the base language of a locale: "es-CO" -> "es".
function baseOf(code: string): string {
  return code.split("-")[0] ?? code;
}

async function persist(
  slot: string,
  values: string[],
  status: HTMLElement,
): Promise<void> {
  const res = await Settings.SetLanguages(slot, values);
  if (res.error) {
    status.className = "lang-status err";
    status.textContent = "✗ " + res.error;
    // Repaint from what is ACTUALLY stored, so the control snaps back rather than showing a value
    // the backend refused.
    onSaved(res.payload);
    return;
  }
  status.className = "lang-status ok";
  status.textContent = "✓ idioma guardado";
  onSaved(res.payload);
}

// renderLanguageInto draws one slot's control into a container.
export function renderLanguageInto(
  box: HTMLElement,
  control: LanguageControl,
): void {
  const selected = control.selected ?? [];
  const options = control.options ?? [];

  box.innerHTML = "";
  const label = document.createElement("div");
  label.className = "lang-label";
  label.textContent = control.label;
  const desc = document.createElement("div");
  desc.className = "lang-desc";
  desc.textContent = control.desc;
  box.append(label, desc);

  const status = document.createElement("div");
  status.className = "lang-status";

  if (control.kind === "multi") {
    const chips = document.createElement("div");
    chips.className = "lang-chips";
    for (const opt of options) {
      const on = selected.includes(opt.code);
      // ONE LOCALE PER BASE LANGUAGE: a sibling of an already-chosen locale is disabled rather than
      // accepted and then rejected on save. Azure does not reject it either — it quietly degrades
      // detection — so the control is where the user finds out.
      const baseTaken =
        !on && selected.some((c) => baseOf(c) === baseOf(opt.code));
      const atMax = !on && selected.length >= control.max;
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = opt.label;
      b.setAttribute("aria-pressed", String(on));
      if (baseTaken || atMax) b.disabled = true;
      b.onclick = () => {
        const next = on
          ? selected.filter((c) => c !== opt.code)
          : [...selected, opt.code];
        if (next.length === 0) {
          // Refused here rather than sent: an empty list is the one edit whose failure the user
          // cannot interpret, because nothing on screen changed.
          status.className = "lang-status err";
          status.textContent = t("✗ Deja al menos un idioma");
          return;
        }
        void persist(control.slot, next, status);
      };
      chips.appendChild(b);
    }
    box.append(chips, status);
    return;
  }

  const sel = document.createElement("select");
  if (control.kind === "auto-or-one") {
    // "Automatic" comes first and is the default for these engines: sending no language at all lets
    // the provider deduce it from the audio, which is better than forcing the first configured one.
    const auto = document.createElement("option");
    auto.value = "auto";
    auto.textContent = t("Detección automática");
    sel.appendChild(auto);
  }
  for (const opt of options) {
    const o = document.createElement("option");
    o.value = opt.code;
    o.textContent = opt.label;
    sel.appendChild(o);
  }
  sel.value = selected[0] ?? "";
  sel.onchange = () => void persist(control.slot, [sel.value], status);
  box.append(sel, status);
}

// renderAllLanguages draws every row's control.
//
// The container is created HERE rather than in the markup, as in the original, so the six rows stay
// free of duplicated HTML — and it is inserted before .conn-actions so the control sits above the
// row's buttons.
//
// The Azure row's slot depends on its SUB-SERVICE, so it is resolved per render: switching Speech to
// OpenAI realtime changes which slot the row is editing, and a stale data-slot would silently save
// the language into the other service's.
export function renderAllLanguages(p: SettingsPayload): void {
  const drawn: string[] = [];
  const controls = new Map((p.languageControls ?? []).map((c) => [c.slot, c]));

  // The slot per row comes from the CONNECTION ROW, which Go resolved with LangSlotFor. Working it
  // out here would be a second copy of a rule that has a real trap in it — Azure's slot depends on
  // its sub-service — and the two copies drifting would save a language into the wrong service.
  const slotByProvider = new Map(
    (p.connections ?? []).map((r) => [r.id, r.langSlot]),
  );

  for (const card of document.querySelectorAll<HTMLElement>(
    ".conn[data-provider]",
  )) {
    const provider = card.dataset.provider ?? "";
    const slot = slotByProvider.get(provider);
    if (!slot) continue;
    card.dataset.slot = slot;

    const control = controls.get(slot);
    if (!control) continue;

    const form = card.querySelector<HTMLElement>(".conn-form");
    if (!form) continue;
    let box = form.querySelector<HTMLElement>(".conn-lang");
    if (!box) {
      box = document.createElement("div");
      box.className = "conn-lang";
      form.insertBefore(box, form.querySelector(".conn-actions"));
    }
    renderLanguageInto(box, control);
    drawn.push(
      `${slot}:${control.kind}:${box.querySelectorAll(".lang-chips button").length}chips/${box.querySelectorAll("select option").length}opts`,
    );
  }

  // Reported so fidelity is checkable from the log: which shape each slot got and how many choices
  // it offers. A control drawn with the wrong value space is the failure mode here, and it is not
  // visible from a screenshot without reading every option.
  Events.Emit("ui:languages", { drawn: drawn.join(" ") });
}
