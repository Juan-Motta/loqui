// Settings / app-shell renderer.
//
// PORT IN PROGRESS. The markup in index.html is the Electron page verbatim (1249 lines). Wired so
// far: the sidebar navigation and the Ajustes tabs, the Home record button, the Historial list with
// its search/date filter and clear action, and the loop that makes the app CONFIGURABLE at all — the
// engine picker, the API key fields and the Azure region.
//
// NAVIGATION CAME LAST AND SHOULD HAVE COME FIRST. Every control the settings loop added lives
// inside the Ajustes view, and the stylesheet hides every view but the active one — so until the
// sidebar was hooked up, none of it could be reached by a mouse. The end-to-end check that had
// "verified" the loop drove the binding from Go, which proves the plumbing and says nothing about
// reachability. They are two different claims and need two different checks.
//
// THE DIVISION OF LABOUR. The Electron original (src/settings/settings.ts, 1828 lines) imported
// ten pure shared modules — i18n, languageCatalog, connectionStatus, permissions, triggerKey,
// modelSpec, historyFilter, inputDevices, languageSlots, settings — because main and renderer
// shared one language there. Here those rules live in Go as the single source of truth, so this
// file decides nothing: it reads one payload, paints it, sends the user's action back, and
// repaints from whatever Go returns. No local state and no optimistic updates — the payload IS
// the state.
//
// Still inert: the Azure subservice switch (speech vs openai), the language pickers, the trigger
// key, appearance, the input device, the permission rows, About and the onboarding wizard. See
// docs/plans/loqui-go-port.md, phase 4.
import { Events } from "@wailsio/runtime";
import * as Settings from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/settingsservice.js";
import * as Dictation from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/dictationservice.js";
import * as Links from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/linksservice.js";
import type {
  SettingsPayload,
  WriteResult,
} from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/models.js";
import {
  refreshHistory,
  setHistoryLocale,
  setHistoryTriggerContext,
  wireHistory,
} from "./history.js";
import { renderAllLanguages, setLanguageSaveHandler } from "./language.js";
import { paintSystem, setSystemSaveHandler, wireSystem } from "./system.js";
import { paintAbout } from "./about.js";
import {
  openWizard,
  paintOnboarding,
  setOnboardingSaveHandler,
  wireOnboarding,
} from "./onboarding.js";
import {
  refreshPermissions,
  setPermissionsChangeHandler,
  wirePermissions,
} from "./permissions.js";

console.info(
  "[loqui] settings shell loaded (partial port: nav + engine + keys + history)",
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

// ---- navigation --------------------------------------------------------------------
//
// WITHOUT THIS NOTHING ELSE IS REACHABLE. The stylesheet hides every `.view` and shows only the
// one carrying `.active`, and the markup ships with `inicio` active — so until the sidebar is
// wired, Ajustes, Historial and About do not exist as far as the user is concerned. The controls
// this file spent its effort on live inside Ajustes.
//
// It is worth naming how that was missed: the settings loop was verified end to end by driving the
// binding from Go, which proved the loop worked and said nothing about whether a human could reach
// it. Reachability is not a detail of the same test — it is a different claim.
function showView(name: string): void {
  for (const view of document.querySelectorAll<HTMLElement>(
    "section.view[data-view]",
  )) {
    view.classList.toggle("active", view.dataset.view === name);
  }
  // The sidebar highlight follows, but only for entries that ARE sidebar entries: the bug-report
  // view is opened from the footer and has no nav item, so nothing should end up highlighted.
  for (const item of document.querySelectorAll<HTMLElement>(
    ".nav-item[data-view]",
  )) {
    item.classList.toggle("active", item.dataset.view === name);
  }
  Events.Emit("ui:view", { view: name });
}

function wireNavigation(): void {
  for (const item of document.querySelectorAll<HTMLElement>(
    ".nav-item[data-view]",
  )) {
    item.addEventListener("click", () =>
      showView(item.dataset.view ?? "inicio"),
    );
  }
  // "Ver todo" and the footer's bug-report link: same navigation, different affordance.
  for (const link of document.querySelectorAll<HTMLElement>("[data-goto]")) {
    link.addEventListener("click", () =>
      showView(link.dataset.goto ?? "inicio"),
    );
  }
  // "Invítame un café", in the sidebar footer AND in Acerca de: the same action from two places, so
  // one handler for both. The URL is Go's — see internal/app/links_service.go.
  //
  // A failure has to be visible somewhere. It lands in the status line rather than the console: the
  // webview's devtools are not open in a packaged build, so a console-only error is no error at all,
  // and this button already spent the whole port doing nothing without anyone noticing.
  const donate = (id: string) => {
    $<HTMLElement>(id)?.addEventListener("click", () => {
      void Links.OpenDonate().then(
        () => Events.Emit("ui:donate", { from: id, ok: true }),
        (err: unknown) => {
          Events.Emit("ui:donate", { from: id, error: String(err) });
          const status = $<HTMLElement>("inicioStatus");
          if (status) {
            status.className = "status err";
            status.textContent = "No se pudo abrir el enlace: " + String(err);
          }
        },
      );
    });
  };
  donate("openDonate");
  donate("aboutDonate");

  // Dev affordance: click the REAL button. Opening a browser is invisible from here, and driving
  // Links.OpenDonate() directly would pass with the button unwired — which is the state it was in.
  Events.On("debug:donate", (e: { data: unknown }) => {
    const arg = Array.isArray(e.data) ? e.data[0] : e.data;
    const id = String(arg ?? "openDonate");
    const button = $<HTMLElement>(id);
    Events.Emit("ui:donate", { probe: id, found: !!button });
    button?.click();
  });

  const report = $<HTMLElement>("openReport");
  report?.addEventListener("click", () =>
    showView(report.dataset.view ?? "report"),
  );
}

// ---- the Home waveform -------------------------------------------------------------
//
// The markup ships an EMPTY <div class="waveform" id="heroWave">, and the stylesheet already has
// everything it needs: `.active` runs a baseline pulse, `.metering` drives bar heights from --level
// and a per-bar --m multiplier. Only the bars themselves and the wiring were missing, so the
// indicator never appeared at all.
// buildWave creates the hero equalizer, PORTED VERBATIM from the Electron renderWave.
//
// The numbers are not arbitrary and the first attempt at this got them wrong: it used 28 bars with
// the overlay pill's formulas, which produced a visibly different wave from the original. The real
// shape comes from a sine plus a small deterministic noise term feeding a per-bar --m factor, and a
// per-bar OPACITY derived from that same factor — that opacity is what gives the row its depth, and
// omitting it flattens the whole thing.
const WAVE_BARS = 46;

function buildWave(): void {
  const wave = $<HTMLElement>("heroWave");
  if (!wave || wave.childElementCount > 0) return;
  let html = "";
  for (let i = 0; i < WAVE_BARS; i++) {
    const base = Math.sin(i / 2.4) * 0.5 + 0.5;
    const noise = ((i * 37) % 11) / 11;
    const m = (0.3 + (base * 0.62 + noise * 0.38) * 0.95).toFixed(2);
    const op = (0.45 + Number(m) * 0.5).toFixed(2);
    // Out of phase so the row reads as a wave rather than one block moving.
    const delay = (((i * 53) % 80) / 100).toFixed(2);
    html += `<span style="opacity:${op};animation-delay:${delay}s;--m:${m}"></span>`;
  }
  wave.innerHTML = html;
}

// ---- the record button -------------------------------------------------------------

// setDictating reflects the engine's state in the Home button, the waveform and the root element.
function setDictating(active: boolean): void {
  document.documentElement.dataset.dictating = String(active);
  const label =
    $<HTMLElement>("testDictate")?.querySelector<HTMLElement>(".btn-label");
  if (label) label.textContent = active ? "Detener" : "Probar dictado";

  const wave = $<HTMLElement>("heroWave");
  if (wave) {
    // `armed` is a flat line above the idle baseline: dictating, nothing heard yet. It is NOT an
    // animation, and that is the point — the sweeping pulse it replaced was indistinguishable from
    // real metering, so it claimed to be hearing audio during whisper's one-to-two-second model
    // load, and for any provider that cannot report levels at all.
    wave.classList.toggle("armed", active);
    if (!active) {
      // Cleared on stop so the next session starts from the baseline rather than resuming at
      // whatever level the last frame happened to leave behind.
      wave.classList.remove("metering");
      wave.style.removeProperty("--level");
    }
  }
}

// The engine is the authority on whether a dictation is running: it can start from the trigger key
// or the tray with this window never involved, and it can stop on its own (the idle guard).
Events.On("dictation:state", (e: { data: boolean | boolean[] }) => {
  const active = Array.isArray(e.data) ? e.data[0] : e.data;
  setDictating(!!active);
});

// Live microphone level, 0..1. The same event the overlay pill listens to, so the two indicators
// cannot disagree.
//
// `metering` is only added once a real level arrives, which is what distinguishes "audio is reaching
// the app" from the baseline pulse. That distinction is the point: a pulse that runs regardless says
// the app is hearing you whether or not it is.
Events.On("meter:level", (e: { data: number | number[] }) => {
  const wave = $<HTMLElement>("heroWave");
  if (!wave || !wave.classList.contains("armed")) return;
  const level = Array.isArray(e.data) ? e.data[0] : e.data;
  wave.classList.add("metering");
  wave.style.setProperty("--level", String(Number(level) || 0));
});

function wireRecordButton(): void {
  const button = $<HTMLButtonElement>("testDictate");
  button?.addEventListener("click", () => {
    // Not routed through run(): this touches no settings, so it has no payload to repaint from and
    // nothing to serialise against. The dictation:state event is what updates the label.
    button.disabled = true;
    Dictation.Toggle().then(
      (active) => {
        setDictating(active);
        button.disabled = false;
        Events.Emit("ui:action", {
          action: "dictation.toggle",
          ok: true,
          active,
        });
      },
      (err: unknown) => {
        button.disabled = false;
        const hint = $<HTMLElement>("ctaHint");
        if (hint) hint.textContent = String(err);
        Events.Emit("ui:action", {
          action: "dictation.toggle",
          ok: false,
          error: String(err),
        });
      },
    );
  });
}

// ---- the Ajustes tabs --------------------------------------------------------------
//
// A second navigation layer INSIDE Ajustes, with the same failure as the sidebar had: the stylesheet
// hides every `.tab-panel` and shows only `.active`, so until this is wired the Sistema and Permisos
// panels are as unreachable as the whole view was.
function wireTabs(): void {
  const tabs = Array.from(
    document.querySelectorAll<HTMLElement>(".tab[data-tab]"),
  );
  for (const tab of tabs) {
    tab.addEventListener("click", () => {
      const name = tab.dataset.tab;
      for (const t of tabs) t.classList.toggle("active", t === tab);
      for (const panel of document.querySelectorAll<HTMLElement>(
        ".tab-panel[data-tab]",
      )) {
        panel.classList.toggle("active", panel.dataset.tab === name);
      }
      Events.Emit("ui:tab", { tab: name });
    });
  }
}

// ---- which credential belongs to which card ---------------------------------------
// A card's data-provider is a PROVIDER; the credential is a key SLOT, and the two are not the same
// list (see store.AllProviders vs store.AllKeySlots). The local engines have no credential at all.
//
// AZURE HAS TWO SLOTS, and only one of them is ported. The card maps to azure-speech; azure-openai
// is the realtime subservice the #azureService select still offers. Leaving that option live while
// this mapping ignores it meant entering an Azure OpenAI key and clicking Guardar OVERWROTE the
// Azure Speech credential — so the option is disabled from the payload until it is wired, and the
// backend refuses writes to unusable slots regardless of what the page does.
const KEY_SLOT_BY_PROVIDER: Record<string, string> = {
  azure: "azure-speech",
  openai: "openai",
  grok: "grok",
  elevenlabs: "elevenlabs",
};

// The #azureService options, by the slot each one would configure.
const AZURE_SERVICE_SLOT: Record<string, string> = {
  speech: "azure-speech",
  openai: "azure-openai",
};

// The key input is a different element per card, inherited from the Electron markup.
const KEY_INPUT_BY_PROVIDER: Record<string, string> = {
  azure: "key",
  openai: "openaiApiKey",
  grok: "grokApiKey",
  elevenlabs: "elevenApiKey",
};

const $ = <T extends HTMLElement>(id: string) =>
  document.getElementById(id) as T | null;

// The engine labels as the markup shipped them, captured ONCE at load.
//
// paint() rebuilds the picker's options and appends "— no disponible aún" to the unavailable ones.
// Reading the labels back off those rebuilt options — which is what it used to do — appended the
// suffix again on every repaint, so an unavailable engine grew a longer name with each write.
const ENGINE_LABELS: Map<string, string> = new Map(
  Array.from($<HTMLSelectElement>("homeEngine")?.options ?? []).map((o) => [
    o.value,
    (o.textContent ?? o.value).trim(),
  ]),
);

function escapeHtml(s: string): string {
  return s.replace(
    /[&<>"]/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c] ?? c,
  );
}

// ---- painting ---------------------------------------------------------------------

// paint renders the view from one payload. Called on load and after every write, so there is
// exactly one path from state to pixels and nothing is ever updated in place.
// paintedRevision is the newest snapshot this page has drawn.
//
// Several producers hand paint() a whole-window snapshot and they do not queue against each other:
// the Conexiones queue, Sistema, idiomas, onboarding, and the reload after a permission grant. The
// order they arrive in is therefore not the order they were taken in, and the last to land would win
// — putting a superseded state back on screen. Go stamps each payload as it STARTS being built
// (SettingsPayload.Revision), which makes "this one is older" a fact rather than a guess.
let paintedRevision = 0;

// Returns whether the snapshot was applied. Callers use it to keep their hands off anything this
// function owns when it declines: if a newer payload is already on screen, that one is right.
function paint(p: SettingsPayload): boolean {
  // A snapshot that began before the one already drawn describes a world that has moved on.
  if (p.revision > 0 && p.revision < paintedRevision) {
    Events.Emit("ui:stale-payload", { got: p.revision, painted: paintedRevision });
    return false;
  }
  if (p.revision > paintedRevision) paintedRevision = p.revision;

  // The engine-check sentence is cleared by whatever paints next, and nothing else would clear it.
  // Every other status line belongs to an action that rewrites it; this one is written from an event,
  // outside run(), so no later action owns it. Choosing an engine from a Conexiones card puts its
  // feedback in the CARD's status and leaves this one alone — so the picker would read "macOS" with
  // "✓ Azure no está listo para dictar: se cambió a Whisper" still underneath it. Same failure the
  // revision arbitration exists for, one step later: there the payload was stale, here the sentence is.
  //
  // Safe for the paths that DO write here: run() calls paint() before its own say(), and the engine
  // check re-says its sentence right after the paint it triggers.
  const engineLine = $<HTMLElement>("engineStatus");
  if (engineLine) {
    engineLine.className = "status";
    engineLine.textContent = "";
  }

  const statusBySlot = new Map((p.keys ?? []).map((k) => [k.slot, k]));

  // The engine picker. Options come from Go rather than the hardcoded markup list, so an engine
  // the backend does not know cannot be selected — the markup offers six and Go is the authority
  // on which are real.
  const home = $<HTMLSelectElement>("homeEngine");
  if (home) {
    const providers = p.providers ?? [];
    // The readiness of each engine comes from the Conexiones rows — the same tested model the Ajustes
    // tab paints — so the picker and that list cannot disagree. They did: the picker labelled only the
    // UNPORTED engines and showed Azure and Grok as plain options while Conexiones called them "Sin
    // configurar", which in a picker reads as "ready to use".
    //
    // Declared out here because the hint below needs it too.
    const stateById = new Map((p.connections ?? []).map((c) => [c.id, c]));
    if (providers.length > 0) {
      // Unavailable engines are LISTED but disabled. Hiding them would make the app look like it
      // supports less than it will; leaving them selectable would let the user replace a working
      // engine with one that fails at the next dictation. Go refuses them too — this is the part
      // that stops the user getting there in the first place.
      //
      // Labels come from ENGINE_LABELS, captured before the first repaint: reading them back off
      // the rebuilt options re-appended the suffix every time.
      home.innerHTML = providers
        .map((prov) => {
          const label = escapeHtml(ENGINE_LABELS.get(prov.id) ?? prov.id);
          const state = stateById.get(prov.id)?.state;
          // Three different "cannot use this", kept distinct because the way out of each differs:
          // not ported YET (wait for a release), cannot run on THIS machine (nothing will fix it),
          // and not configured (add a key in Ajustes). All three are UNSELECTABLE — picking one
          // would replace a working engine with one that cannot dictate.
          //
          // THE EXCEPTION: the engine already stored stays selectable-looking even when it is not
          // usable. A <select> cannot display a value it has no option for, so removing or blanking
          // it would make the picker show a DIFFERENT engine than the one in effect — the original
          // keeps it for the same reason (pruneEngineOptions), e.g. settings copied from another Mac.
          const isStored = prov.id === p.provider;
          let suffix = "";
          let disabled = "";
          if (!prov.available) {
            suffix = " — no disponible aún";
            disabled = isStored ? "" : " disabled";
          } else if (state === "unsupported") {
            suffix = " — no disponible en este sistema";
            disabled = isStored ? "" : " disabled";
          } else if (state === "unconfigured") {
            suffix = " — sin configurar";
            disabled = isStored ? "" : " disabled";
          }
          return `<option value="${prov.id}"${disabled}>${label}${suffix}</option>`;
        })
        .join("");
    }
    home.value = p.provider;

    // The line under the picker, PORTED from renderEngineHint. Without it, choosing an engine that
    // needs a key looked like it worked: the picker changed, nothing complained, and the failure
    // surfaced at the next dictation. Derived from the ACTIVE engine's row rather than from key
    // presence, so "cannot run here at all" stays distinct from "needs configuring" — no amount of
    // configuring fixes the first, and saying so is the point.
    const hint = $<HTMLElement>("engineHint");
    if (hint) {
      const state = stateById.get(p.provider)?.state;
      if (state === "unsupported") {
        hint.textContent = "No disponible en este sistema";
        hint.className = "engine-hint warn";
      } else if (state === "unconfigured") {
        hint.textContent = "Este motor necesita configuración — ábrela en Ajustes";
        hint.className = "engine-hint warn";
      } else {
        hint.textContent = "";
        hint.className = "engine-hint";
      }
    }

    // What the picker ACTUALLY offers, option by option, on EVERY repaint. A <select> inside a Wails
    // webview cannot be opened from a script, so its contents were the one thing never checked — and
    // that is exactly where the picker and the Conexiones list had drifted apart.
    Events.Emit("ui:engine-options", {
      options: Array.from(
        document.querySelectorAll<HTMLOptionElement>("#homeEngine option"),
      )
        .map((o) => `${o.value}${o.disabled ? "[disabled]" : ""}=${(o.textContent ?? "").trim()}`)
        .join(" | "),
      hint: $<HTMLElement>("engineHint")?.textContent ?? "",
    });
  }

  // The Azure region dropdown is empty in the markup: Go owns the list.
  const region = $<HTMLSelectElement>("region");
  if (region) {
    const offered = p.azureRegions ?? [];
    // A stored region that is not in the curated list still has to be SELECTABLE. The backend
    // validates the shape rather than a membership list, so a perfectly valid region can be in the
    // settings and absent here — and assigning .value with no matching <option> silently selects
    // the empty placeholder, which then saves as "no region" on the next save.
    const listed = offered.some((r) => r.id === p.region);
    const extra =
      p.region !== "" && !listed
        ? `<option value="${escapeHtml(p.region)}">${escapeHtml(p.region)} (guardada)</option>`
        : "";
    region.innerHTML =
      `<option value="">Selecciona una región…</option>` +
      offered
        .map((r) => `<option value="${r.id}">${escapeHtml(r.name)}</option>`)
        .join("") +
      extra;
    region.value = p.region;
  }

  // The Conexiones model, computed in Go (store.ConnectionRows). What the page used to do instead
  // was guess a status from key presence, which collapsed three different situations —
  // "configured but not selected", "nothing to configure", "cannot run on this machine" — into one
  // label. The row carries its own state, kind line and text.
  const rows = new Map((p.connections ?? []).map((r) => [r.id, r]));
  // The states the model produced, so fidelity is checkable from the log rather than by eye.
  Events.Emit("ui:connections", {
    rows: (p.connections ?? []).map((r) => `${r.id}=${r.state}`).join(" "),
    hint: p.providerHint !== "",
  });
  const hint = $<HTMLElement>("providerHint");
  if (hint) hint.textContent = p.providerHint;

  // The Azure card's subservice picker. An option whose slot the app cannot read must not be
  // selectable: this form writes to KEY_SLOT_BY_PROVIDER["azure"], so choosing the unported
  // subservice and saving would put its key over the one actually in use.
  const azureService = $<HTMLSelectElement>("azureService");
  if (azureService) {
    for (const option of Array.from(azureService.options)) {
      const slot = AZURE_SERVICE_SLOT[option.value];
      const usable = slot ? (statusBySlot.get(slot)?.available ?? false) : true;
      option.disabled = !usable;
      if (!usable && !option.textContent?.includes("no disponible")) {
        option.textContent = `${option.textContent ?? option.value} — no disponible aún`;
      }
      // Never leave the unusable one selected, whatever the markup defaulted to.
      if (!usable && azureService.value === option.value)
        azureService.value = "speech";
    }
    // The Azure OpenAI fields belong to the unported subservice; hide them while it is selected
    // nowhere, so the form cannot collect values nothing will read.
    const openaiConfig = $<HTMLElement>("openaiConfig");
    if (openaiConfig) openaiConfig.hidden = azureService.value !== "openai";
    const speechConfig = $<HTMLElement>("speechConfig");
    if (speechConfig) speechConfig.hidden = azureService.value !== "speech";
  }

  for (const card of document.querySelectorAll<HTMLElement>(
    ".conn[data-provider]",
  )) {
    const provider = card.dataset.provider ?? "";
    const row = rows.get(provider);
    // A card the backend does not describe is treated as unsupported: the markup ships six, and Go
    // is the authority on which of them this machine can run.
    const state = row?.state ?? "unsupported";
    const active = state === "active";
    const available = state !== "unsupported";

    card.classList.toggle("is-active", active);
    card.classList.toggle("is-unsupported", !available);

    // The kind line — what the engine IS. For Azure it follows the selected sub-service.
    const kind = card.querySelector<HTMLElement>(".conn-kind");
    if (kind) kind.textContent = row?.kind ?? "";

    // The badge carries the STATE AS A CLASS, which is what colours its dot; the label is the
    // model's own wording. Setting only textContent, as this page did before, left every badge
    // styled the same regardless of whether the engine was ready.
    const badge = card.querySelector<HTMLElement>(".conn-state");
    if (badge) {
      badge.className = "conn-state " + state;
      badge.innerHTML = `<span class="dot"></span>${escapeHtml(row?.label ?? "")}`;
    }

    const slot = KEY_SLOT_BY_PROVIDER[provider];
    const key = slot ? statusBySlot.get(slot) : undefined;

    // "Usar este motor", by state, and the three outcomes are deliberately different:
    //
    //  · active / unsupported → HIDDEN, as the original does. A button that could never do anything
    //    here just occupies the row and invites the question of why it is dead.
    //  · unconfigured → VISIBLE BUT DISABLED. Selecting an engine that cannot dictate is how a
    //    working setup gets replaced by one that fails at the next dictation; shown-and-dead says
    //    "this is the button, once you finish configuring" in a way an absent one cannot.
    //  · connected / available → enabled. `available` is load-bearing: it is the state of Whisper and
    //    macOS, which take no credential at all — treating "no key" as "not configured" would make
    //    both local engines unselectable.
    for (const use of card.querySelectorAll<HTMLButtonElement>(".conn-use")) {
      use.style.display = active || !available ? "none" : "";
      const ready = state === "connected" || state === "available";
      use.disabled = !ready;
      use.title = ready ? "" : "Configura este motor antes de poder usarlo";
    }

    // The key field stays EMPTY even when a key is stored: the payload carries presence, never
    // the secret, so there is nothing to prefill. The label beside it says whether one is there.
    //
    // Found by class, not by id: each card's span has a DIFFERENT id (keyState, openaiKeyState,
    // grokKeyState, elevenKeyState), so looking up "#keyState" repainted Azure's card and silently
    // left the other three blank.
    const label = card.querySelector<HTMLElement>(".key-state");
    if (label) label.textContent = keyStateLabel(key?.status, key?.fromEnv);

    // Deleting is only meaningful for a key that is actually in the Keychain. An env-supplied one
    // has nothing to delete, and deleting the Keychain item behind it would remove something the
    // user cannot see while the slot still reads as configured.
    const save = card.querySelector<HTMLButtonElement>(".conn-save");
    if (save) save.disabled = !available;

    const del = card.querySelector<HTMLButtonElement>(".conn-delete");
    if (del) {
      const deletable = available && key?.status === "present" && !key.fromEnv;
      del.disabled = !deletable;
      del.title = key?.fromEnv
        ? "Definida por variable de entorno — quítala del entorno para dejar de usarla"
        : "";
    }
  }

  // The per-engine language controls, drawn last because they insert themselves into each row's
  // form. Their shape, options and copy all come from the payload — see frontend/src/language.ts.
  renderAllLanguages(p);
  paintSystem(p);
  paintOnboarding(p);
  return true;
}

// paintOwns reports whether paint() sets this control's enabled state.
//
// The three buttons inside a card are painted from the payload, so releasing them by hand after a
// write is not merely redundant: when the payload is DECLINED as stale, the hand-release would put
// back a button that the newer snapshot had disabled — handing the user a "Borrar clave" for a key
// that is already gone. Controls paint() does not own (the engine picker, "Probar conexión") have to
// be released by whoever disabled them.
function paintOwns(control: HTMLButtonElement | HTMLSelectElement | null): boolean {
  return (
    control !== null &&
    (control.classList.contains("conn-save") ||
      control.classList.contains("conn-delete") ||
      control.classList.contains("conn-use"))
  );
}

// The badge's wording is NOT decided here any more. It comes from store.ConnectionRows, which is the
// ported model — a label invented in the page collapsed "configured but not selected", "nothing to
// configure" and "cannot run here" into the same sentence.
//
// The three-way key state still gets its own wording, below, because that distinction lives in the
// FIELD rather than the badge: "the Keychain did not answer" sends the user somewhere completely
// different from "you never set a key", and telling them to retype a stored credential is worse
// than saying nothing.
function keyStateLabel(status?: string, fromEnv?: boolean): string {
  switch (status) {
    case "present":
      return fromEnv
        ? "(definida por variable de entorno — no se puede borrar desde aquí)"
        : "(guardada — escribe una nueva para reemplazarla)";
    case "absent":
      // Absent AND from the environment is a state of its own: the variable is in force, so it is
      // what dictation reads, and it holds nothing usable. "No configurada" would send the user to
      // paste a key that the variable would go on overriding — and that the backend refuses to save
      // for exactly that reason.
      return fromEnv
        ? "(la variable de entorno está definida pero vacía — quítala del entorno para usar una clave guardada)"
        : "(no configurada)";
    case "unreadable":
      return "(el Keychain no respondió — la app no está firmada con una identidad estable)";
    default:
      return "";
  }
}

// ---- acting ----------------------------------------------------------------------

// ---- who gets to speak, and what it looks like -------------------------------------

// cardEpochs arbitrates the STATUS LINE of one card.
//
// The four actions of a card share one `.status`, and the connection test does NOT go through the
// write queue — it changes nothing, and making a fifteen-second network call block a Guardar would be
// worse than the problem. So a slow test can finish after a newer action and describe a world that is
// two steps old. Each action takes the card's epoch when it starts and only writes the message if
// nobody has started another one since.
//
// It arbitrates the MESSAGE ONLY. Payloads are ordered by their own revision inside paint(): dropping
// a write's payload here would leave the badge, the key label and the delete button describing the
// state before the write that just succeeded.
const cardEpochs = new WeakMap<HTMLElement, number>();

function beginAction(card: HTMLElement | null): number {
  if (!card) return 0;
  const next = (cardEpochs.get(card) ?? 0) + 1;
  cardEpochs.set(card, next);
  // A new action supersedes whatever the last one complained about, so the red border goes with it.
  for (const field of card.querySelectorAll<HTMLElement>(".invalid")) {
    field.classList.remove("invalid");
  }
  return next;
}

function isCurrent(card: HTMLElement | null, epoch: number): boolean {
  return card === null || (cardEpochs.get(card) ?? 0) === epoch;
}

// say writes the one line the user actually reads.
//
// Success is STATED, never implied by silence: an empty status line is indistinguishable from a click
// that never arrived, and for "Borrar clave" that would mean a credential vanishing without a word.
// The busy text names the activity for the same reason — a bare ellipsis is on screen for as long as
// the round trip takes and reads as a flicker.
function say(status: HTMLElement | null, kind: "ok" | "err" | "busy", text: string): void {
  if (!status) return;
  status.className = kind === "busy" ? "status" : "status " + kind;
  status.textContent = kind === "ok" ? "✓ " + text : kind === "err" ? "✗ " + text : text;
}

// markInvalid puts the border on the input Go named. The page is told WHICH field, never asked to
// work it out from the message — that would be the same validation rule written twice.
function markInvalid(card: HTMLElement | null, provider: string, field: string): void {
  if (!card || field === "") return;
  const id =
    field === "key" ? KEY_INPUT_BY_PROVIDER[provider] : field === "region" ? "region" : "";
  const input = id ? $<HTMLElement>(id) : null;
  if (!input) return;
  input.classList.add("invalid");
  // Cleared as soon as the user acts on the complaint. A border that survives the correction is
  // worse than none: it goes on accusing someone who already did what was asked.
  const clear = () => input.classList.remove("invalid");
  input.addEventListener("input", clear, { once: true });
  input.addEventListener("change", clear, { once: true });
}

// writes is a one-at-a-time queue for every settings action.
//
// Disabling only the control that was clicked is not enough: the other handlers stay live, so two
// different cards can be saved at once. Their calls can then return out of order and the OLDER
// payload paints last, leaving the page showing state that has already been superseded — and the
// next save built from that stale form can put the superseded value back on disk. Serialising means
// the payload painted last is always the newest, with no revision bookkeeping to get wrong.
let writes: Promise<unknown> = Promise.resolve();

function serialize<T>(fn: () => Promise<T>): Promise<T> {
  // Chained off both outcomes, so one failure does not wedge the queue for the rest of the session.
  const next = writes.then(fn, fn);
  writes = next.then(
    () => undefined,
    () => undefined,
  );
  return next;
}

// run wraps every write.
//
// It ALWAYS repaints, success or failure, because every setter returns the freshly computed state
// alongside its error message. That is why the setters return a WriteResult instead of a Go error:
// Wails discards a bound method's result whenever it also returns an error, so a rejected write
// would leave the page with nothing authoritative to repaint from — showing the choice that was
// just refused. Repainting on failure is what snaps the picker back to the engine really in use.
//
// While a write is in flight the triggering control is disabled: two overlapping Keychain
// operations on one slot are exactly what the store's per-slot gate has to serialise, and a
// double-clicked Guardar is the easiest way to cause it.
async function run(
  label: string,
  status: HTMLElement | null,
  trigger: HTMLButtonElement | HTMLSelectElement | null,
  action: () => Promise<WriteResult>,
  opts: { card?: HTMLElement | null; provider?: string; busy?: string } = {},
): Promise<void> {
  const card = opts.card ?? null;
  const epoch = beginAction(card);
  // A named activity rather than "…". The ellipsis was on screen for as long as the round trip took
  // — often a few hundred milliseconds — and then vanished, so the user reported never seeing it.
  say(status, "busy", opts.busy ?? "Guardando…");
  const wasDisabled = trigger?.disabled ?? false;
  if (trigger) trigger.disabled = true;
  // The ENTIRE sequence goes in the queue — the call, the recovery load and the repaint. Wrapping
  // only the call let the queue advance the moment it rejected, so a later write could finish while
  // the failed one was still fetching its recovery payload, and that stale payload painted last.
  await serialize(async () => {
    try {
      const res = await action();
      // ALWAYS handed over, whatever the epoch says: this payload is the authority on the badge, the
      // key label and the buttons. paint() itself declines it if a newer snapshot already landed.
      const applied = paint(res.payload);
      // The busy state is released only for controls paint() does not own. For the card's own
      // buttons the payload decides — the one just applied, or the newer one that superseded it.
      if (trigger && !paintOwns(trigger)) trigger.disabled = false;
      if (!applied) Events.Emit("ui:stale-write", { action: label });
      if (isCurrent(card, epoch)) {
        if (res.error === "") {
          say(status, "ok", res.notice);
        } else {
          say(status, "err", res.error);
          markInvalid(card, opts.provider ?? "", res.field);
        }
      }
      Events.Emit("ui:action", {
        action: label,
        ok: res.error === "",
        error: res.error,
        notice: res.notice,
        field: res.field,
      });
    } catch (err) {
      // A thrown error here is a TRANSPORT failure, not a rejected write: the binding itself did not
      // complete, so there is no payload and the page's picture is now unknown. Re-read it.
      const msg = String(err instanceof Error ? err.message : err);
      if (isCurrent(card, epoch)) say(status, "err", msg);
      Events.Emit("ui:action", { action: label, ok: false, error: msg });
      try {
        // The payload is awaited BEFORE the control is released. Releasing first would leave it
        // actionable for the whole round trip at the one moment the page's state is explicitly
        // unknown — long enough to start a second write on top of the one that just failed.
        const payload = await Settings.Load();
        // Same arbitration as the success path: paint decides, and the card's own buttons are only
        // ever released by it. This recovery can take a while, so a newer snapshot may well have
        // landed meanwhile — releasing by hand would hand back a button that snapshot had disabled.
        paint(payload);
        if (trigger && !paintOwns(trigger)) trigger.disabled = false;
      } catch {
        // The backend is unreachable, so there is no authoritative state to paint from. Restore the
        // control to what it was rather than enabling it: guessing "enabled" would offer an action
        // whose precondition is now unknown.
        if (trigger) trigger.disabled = wasDisabled;
      }
    }
  });
}

// probe runs a connection test.
//
// Deliberately NOT run(): it is a read, so there is no write to serialise and nothing to roll the
// page back to. What it shares with run() is everything the user can see — the busy line, the epoch
// that decides who speaks, and repainting from whatever payload comes back.
async function probe(
  card: HTMLElement,
  status: HTMLElement | null,
  trigger: HTMLButtonElement,
  provider: string,
  slot: string,
  region: string,
  secret: string,
): Promise<void> {
  const epoch = beginAction(card);
  say(status, "busy", "Probando la conexión…");
  trigger.disabled = true;
  try {
    // Read-after-write: anything already queued has to land first, or an empty key field would be
    // tested against the credential the user has just replaced.
    await writes;
    const res = await Settings.TestConnection(slot, region, secret);
    // Not painted from the payload — no state disables it, by design — so it releases itself.
    trigger.disabled = false;
    // The probe reads the Keychain, so its payload can be the first place a write that timed out and
    // landed late becomes visible. paint() drops it if a newer snapshot got there first.
    // A probe writes nothing, so its repaint must leave the form exactly as it found it. paint() fills
    // the region select from what is STORED — right after a save, wrong here: an unsaved choice would
    // be swapped back silently and the next Guardar would store the key against a region that was
    // never the one tested.
    //
    // The value is read LIVE, immediately before the repaint, and not the one captured at the click:
    // a save that landed while the network was busy has already painted its own region, and putting
    // back the older selection would leave the form disagreeing with the store. The empty value is
    // restored too — clearing the selector is a choice like any other.
    const select = $<HTMLSelectElement>("region");
    const onScreen = select?.value;
    const applied = paint(res.payload);
    if (applied && select && onScreen !== undefined && select.value !== onScreen) {
      select.value = onScreen;
    }
    if (isCurrent(card, epoch)) {
      if (res.ok) {
        say(status, "ok", res.message);
      } else {
        say(status, "err", res.error);
        markInvalid(card, provider, res.field);
      }
    }
    Events.Emit("ui:probe", { provider, ok: res.ok, error: res.error, field: res.field });
  } catch (err) {
    const msg = String(err instanceof Error ? err.message : err);
    trigger.disabled = false;
    if (isCurrent(card, epoch)) say(status, "err", msg);
    Events.Emit("ui:probe", { provider, ok: false, error: msg });
  }
}

function wire(): void {
  const engineStatus = $<HTMLElement>("engineStatus");
  const home = $<HTMLSelectElement>("homeEngine");
  home?.addEventListener("change", (e) => {
    const value = (e.target as HTMLSelectElement).value;
    void run(
      `setProvider(${value})`,
      engineStatus,
      home,
      () => Settings.SetProvider(value),
      { busy: "Cambiando de motor…" },
    );
  });

  for (const card of document.querySelectorAll<HTMLElement>(
    ".conn[data-provider]",
  )) {
    const provider = card.dataset.provider ?? "";
    const slot = KEY_SLOT_BY_PROVIDER[provider];
    const status = card.querySelector<HTMLElement>(".status");

    // "Configurar" only folds the form open; no backend call.
    card
      .querySelector<HTMLButtonElement>(".conn-toggle")
      ?.addEventListener("click", () => {
        const form = card.querySelector<HTMLElement>(".conn-form");
        if (form) form.hidden = !form.hidden;
      });

    // "Probar conexión". It writes NOTHING, so it stays out of the write queue: a fifteen-second
    // network call in there would hold up a Guardar behind it. What it does do is WAIT for whatever
    // is already queued — otherwise a test fired right after a save would read the previous
    // credential from the Keychain and report that the new one fails.
    const test = card.querySelector<HTMLButtonElement>(".conn-test");
    test?.addEventListener("click", () => {
      if (!slot) return;
      // Captured at the click, before the wait. Read afterwards, they would be whatever the form
      // happens to hold when the queue drains rather than what the user pressed with.
      const input = $<HTMLInputElement>(KEY_INPUT_BY_PROVIDER[provider] ?? "");
      const secret = input?.value ?? "";
      const regionValue =
        provider === "azure" ? ($<HTMLSelectElement>("region")?.value ?? "") : "";
      void probe(card, status, test, provider, slot, regionValue, secret);
    });

    const use = card.querySelector<HTMLButtonElement>(".conn-use");
    use?.addEventListener("click", () => {
      void run(
        `setProvider(${provider})`,
        status,
        use,
        () => Settings.SetProvider(provider),
        { card, provider, busy: "Cambiando de motor…" },
      );
    });

    const save = card.querySelector<HTMLButtonElement>(".conn-save");
    save?.addEventListener("click", () => {
      if (!slot) return;
      const input = $<HTMLInputElement>(KEY_INPUT_BY_PROVIDER[provider] ?? "");
      // Snapshot the secret and clear the field IMMEDIATELY. Waiting for the round trip to finish
      // leaves it sitting in the DOM for as long as the Keychain takes — up to ten seconds on a
      // build where it does not answer — and left it there for ever when the write failed.
      const secret = input?.value ?? "";
      if (input) input.value = "";
      const regionValue =
        provider === "azure"
          ? ($<HTMLSelectElement>("region")?.value ?? "")
          : "";

      // ONE backend call, not three. Doing this as SetRegion-then-SetKey from here could commit
      // half of it: the region lands, the key write fails, and the user is left with a provider
      // that looks configured and cannot connect. SaveConnection validates both before writing
      // either.
      void run(
        `saveConnection(${provider})`,
        status,
        save,
        () => Settings.SaveConnection(slot, regionValue, secret),
        { card, provider, busy: "Guardando…" },
      );
    });

    const del = card.querySelector<HTMLButtonElement>(".conn-delete");
    del?.addEventListener("click", () => {
      if (!slot) return;
      void run(
        `deleteKey(${provider})`,
        status,
        del,
        () => Settings.DeleteKey(slot),
        { card, provider, busy: "Borrando la clave…" },
      );
    });
  }
}

// ---- start ------------------------------------------------------------------------

// Navigation and the record button are wired FIRST, before any backend call.
//
// They do not depend on the settings payload, and making them wait on it means a backend that fails
// to answer leaves the whole window dead — not just the part that needed the data. The user could
// not even reach About to read what went wrong.
wireNavigation();
buildWave();
wireRecordButton();
wireTabs();
wireHistory();
wireSystem();
wirePermissions();
wireOnboarding();
// Read once at load so the Permisos tab is already correct when opened, and again on every action.
void refreshPermissions();
// Acerca de is deliberately in this group, for the reason stated above: it is where a user goes to
// read what went wrong, so it must not depend on the settings payload arriving.
void paintAbout();
// The transcripts are on disk regardless of whether the settings payload arrives, so they are read
// on their own rather than behind it.
void refreshHistory();

// The engine may already be dictating (the trigger key or the tray, with this window closed), so
// ask rather than assume idle.
Dictation.Active().then(
  (active) => setDictating(active),
  () => {
    /* leave the label as the markup shipped it */
  },
);

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
    // Wrapped explicitly: a throw inside THIS callback is not caught by the rejection handler
    // below, so without the try a broken selector would leave the page silently inert while the
    // Go log still showed a healthy bootstrap. That exact failure mode — looks fine from Go,
    // does nothing — has already cost this port three debugging sessions.
    try {
      // The empty state's instruction has to name the trigger the user actually has configured, so
      // the history module is told before anything paints.
      setHistoryTriggerContext(payload.triggerKey, payload.mode);
      setHistoryLocale(payload.appLanguage);
      // A language save repaints the WHOLE page from the payload it returned, not just its own
      // control: changing Azure's sub-service moves which slot the row edits, so a control patching
      // itself in isolation would leave the rest of the row describing the other service.
      setLanguageSaveHandler(paint);
      setSystemSaveHandler(paint);
      setOnboardingSaveHandler(paint);
      // A granted microphone is what makes real device NAMES available, so the whole payload is
      // re-read after a grant rather than only the permissions list.
      setPermissionsChangeHandler(() => {
        void Settings.Load().then(paint, () => {});
      });
      paint(payload);
      wire();
      Events.Emit("ui:painted", { provider: payload.provider });
      // The tutorial opens ITSELF on a first launch, and only then. Placed after paint() so the
      // wizard mounts over a window that is already correct — its steps read the same payload, and a
      // wizard drawn before the page had one would show empty engine and preference panels.
      if (!payload.onboarded) openWizard();
    } catch (err) {
      Events.Emit("ui:bootstrap-failed", {
        error: `painting failed: ${err instanceof Error ? err.stack || err.message : String(err)}`,
      });
    }
  },
  (err: unknown) => {
    Events.Emit("ui:bootstrap-failed", { error: String(err) });
  },
);

// The engine moved because the app could not use the one that was selected.
//
// Announced rather than done quietly: something the user chose is no longer in effect, and finding
// that out by noticing a different name in the picker is worse than being told. Go stays the one
// place that computes state — the event carries the snapshot its sentence describes, and paint()
// drops it if a newer one has already landed.
// The check reports its own snapshot alongside its sentence, so the two cannot come apart.
type EngineCheckEvent = { payload?: SettingsPayload; notice?: string };

const engineNews = (kind: "ok" | "err") => (e: { data: unknown }) => {
  const arg = (Array.isArray(e.data) ? e.data[0] : e.data) as EngineCheckEvent | null;
  const notice = arg?.notice ?? "";
  const payload = arg?.payload;
  if (notice === "" || !payload) return;
  // The sentence is painted only if ITS OWN payload was the one applied. Fetching a fresh snapshot
  // here instead would let the two describe different moments: the page would draw whatever the user
  // did in between and then explain a decision about the state before it.
  if (paint(payload)) say($<HTMLElement>("engineStatus"), kind, notice);
};

// Two events, two tones. A tick belongs to a change that happened; "could not check your key" is not
// something that went well, and dressing it as success is how a warning gets read as noise.
Events.On("engine:changed", engineNews("ok"));
Events.On("engine:blocked", engineNews("err"));

// Dev affordance: drive one real write through the binding, on command from Go.
//
// It exists for the same reason LOQUI_DEBUG_DICTATE does — the real trigger cannot be scripted.
// Clicking a <select> inside a Wails webview from a shell script is not something that works, so
// without this the write half of the loop could only be checked by hand. This runs the SAME path
// the picker does, so what it proves is the real one: binding → service → Keychain/disk → repaint.
// Dev affordance: click a real sidebar item and report which view ended up visible.
//
// Same reason as the other debug hooks — a sidebar entry inside a Wails webview cannot be clicked
// from a shell script. This dispatches a genuine click so the handler under test is the one the
// user's mouse reaches, not a reimplementation of it.
// Accepts "<vista>", "<vista>:<pestaña>" or "<vista>:<pestaña>:<id>" — Ajustes' panels are tabs, and
// the sidebar click alone leaves Sistema and Permisos hidden, so without the second half they cannot
// be looked at at all. The third scrolls one control into view: the panel is taller than the window,
// so the rows past the fold can't be measured on a screenshot otherwise.
Events.On("debug:navigate", (e: { data: unknown }) => {
  const arg = Array.isArray(e.data) ? e.data[0] : e.data;
  const [want, wantTab, wantInto] = String(arg ?? "").split(":");
  const item = document.querySelector<HTMLElement>(
    `.nav-item[data-view="${want}"]`,
  );
  if (!item) {
    Events.Emit("ui:nav-probe", { requested: want, error: "no such nav item" });
    return;
  }
  item.click();
  if (wantTab) {
    const tab = document.querySelector<HTMLElement>(
      `.tab[data-tab="${wantTab}"]`,
    );
    if (!tab) {
      Events.Emit("ui:nav-probe", { requested: arg, error: "no such tab" });
      return;
    }
    tab.click();
  }
  if (wantInto) {
    document
      .getElementById(wantInto)
      ?.scrollIntoView({ block: "center", behavior: "instant" });
  }
  const visible = Array.from(
    document.querySelectorAll<HTMLElement>("section.view.active"),
  ).map((v) => v.dataset.view);
  const active = document.querySelector<HTMLElement>("section.view.active");
  Events.Emit("ui:nav-probe", {
    requested: want,
    visible,
    // Measured after the click. A screenshot cannot tell a view scrolled away from its own header
    // apart from a window sitting a few pixels off where you cropped — this session mistook the
    // second for the first, and it was scrollTop that settled it.
    activeScrollTop: active?.scrollTop ?? -1,
    focusedNow: document.activeElement?.id || document.activeElement?.tagName || "",
    navActive:
      document.querySelector<HTMLElement>(".nav-item.active")?.dataset.view,
    tabActive:
      document.querySelector<HTMLElement>(".tab.active")?.dataset.tab ?? "",
    connCards: document.querySelectorAll(".conn[data-provider]").length,
  });
});

// Dev affordance: click the Home record button and report what happened.
//
// Added after navigation shipped broken. The lesson it encodes: driving a binding from Go proves the
// plumbing, not that the control a human touches is connected to it. This clicks the real button.
Events.On("debug:record-click", () => {
  const button = $<HTMLButtonElement>("testDictate");
  if (!button) {
    Events.Emit("ui:record-probe", { error: "no record button in the DOM" });
    return;
  }
  const before =
    $<HTMLElement>("testDictate")?.querySelector(".btn-label")?.textContent;
  button.click();
  // After a tick, so the binding's promise has had a chance to settle.
  setTimeout(() => {
    Events.Emit("ui:record-probe", {
      labelBefore: before,
      labelAfter:
        $<HTMLElement>("testDictate")?.querySelector(".btn-label")?.textContent,
      dictating: document.documentElement.dataset.dictating,
      hint: $<HTMLElement>("ctaHint")?.textContent,
    });
  }, 1500);
});

// Dev affordance: drive the four buttons of a connection card, on command from Go.
//
// Same reason as the other debug hooks — a button inside a Wails webview cannot be clicked from a
// shell script, so without this the only way to check any of this is by hand. It dispatches REAL
// clicks, so what runs is the handler the user's mouse reaches.
//
// The grammar is "<provider>:<action>[:<argument>]", and several steps joined by "+" run WITHOUT
// waiting for each other — which is the only way to reproduce the overlapping-actions cases from
// outside.
//
// It never accepts a key from the environment. "badkey" types a fixed, obviously invalid sentinel:
// passing a real credential through an environment variable would put it in the process environment
// and in every log that captures it.
const DEBUG_BAD_KEY = "loqui-debug-clave-invalida";

function debugConnStep(step: string): string {
  const [provider, action, arg] = step.split(":");
  const card = document.querySelector<HTMLElement>(`.conn[data-provider="${provider}"]`);
  if (!card) return `no such card: ${provider}`;
  // The form is folded away until "Configurar" is pressed, and a click on a hidden button does
  // nothing at all — silently, which would read as the feature being broken.
  const form = card.querySelector<HTMLElement>(".conn-form");
  if (form?.hidden) card.querySelector<HTMLButtonElement>(".conn-toggle")?.click();

  const key = $<HTMLInputElement>(KEY_INPUT_BY_PROVIDER[provider] ?? "");
  const region = $<HTMLSelectElement>("region");
  switch (action) {
    case "test":
      if (arg === "badkey" && key) key.value = DEBUG_BAD_KEY;
      if (arg === "nokey" && key) key.value = "";
      card.querySelector<HTMLButtonElement>(".conn-test")?.click();
      return `test(${arg ?? "asis"})`;
    case "save-region":
      if (region && arg) region.value = arg;
      card.querySelector<HTMLButtonElement>(".conn-save")?.click();
      return `save-region(${arg ?? ""})`;
    case "set-region":
      // Chosen but NOT saved, which is the state a probe has to leave untouched: paint() fills this
      // select from what is stored, so restoring it is the only thing keeping an unsaved choice
      // alive across a test.
      if (region && arg) region.value = arg;
      return `set-region(${arg ?? ""})`;
    case "clear-region":
      if (region) region.value = "";
      return "clear-region";
    case "save":
      if (arg === "nokey" && key) key.value = "";
      card.querySelector<HTMLButtonElement>(".conn-save")?.click();
      return "save";
    case "use":
      card.querySelector<HTMLButtonElement>(".conn-use")?.click();
      return "use";
    case "delete":
      card.querySelector<HTMLButtonElement>(".conn-delete")?.click();
      return "delete";
    default:
      return `no such action: ${action}`;
  }
}

// reportCard is what a card looks like right now: the state a human would read off the screen.
function reportCard(provider: string): Record<string, unknown> {
  const card = document.querySelector<HTMLElement>(`.conn[data-provider="${provider}"]`);
  if (!card) return { provider, error: "no such card" };
  const button = (sel: string) => {
    const b = card.querySelector<HTMLButtonElement>(sel);
    if (!b) return "absent";
    return `${b.style.display === "none" ? "hidden" : "shown"}/${b.disabled ? "disabled" : "enabled"}`;
  };
  const status = card.querySelector<HTMLElement>(".status");
  // The HOME status line, not this card's: it is where the engine check speaks, and it is the one
  // line no action owns — so a sentence left stranded there is invisible to a per-card report. This
  // is the only way to see from outside whether the picker and the line beneath it agree.
  const engine = $<HTMLElement>("engineStatus");
  return {
    provider,
    badge: card.querySelector<HTMLElement>(".conn-state")?.textContent ?? "",
    badgeClass: card.querySelector<HTMLElement>(".conn-state")?.className ?? "",
    status: status?.textContent ?? "",
    statusClass: status?.className ?? "",
    engineStatus: engine?.textContent ?? "",
    homeEngine: $<HTMLSelectElement>("homeEngine")?.value ?? "",
    keyState: card.querySelector<HTMLElement>(".key-state")?.textContent ?? "",
    region: $<HTMLSelectElement>("region")?.value ?? "",
    test: button(".conn-test"),
    use: button(".conn-use"),
    delete: button(".conn-delete"),
    save: button(".conn-save"),
    invalid: Array.from(card.querySelectorAll<HTMLElement>(".invalid"))
      .map((el) => el.id)
      .join(","),
  };
}

Events.On("debug:conn-click", (e: { data: unknown }) => {
  const arg = Array.isArray(e.data) ? e.data[0] : e.data;
  const steps = String(arg ?? "").split("+");
  const provider = String(steps[0] ?? "").split(":")[0];
  // Only the first step names the card; the rest inherit it. Every chain acts on one card — that is
  // what makes the steps overlap in the first place — and repeating the provider in each one reads
  // as though they might not.
  const done = steps.map((step) => {
    const named = document.querySelector(`.conn[data-provider="${step.split(":")[0]}"]`) !== null;
    return debugConnStep(named ? step : `${provider}:${step}`);
  });
  Events.Emit("ui:conn-probe", { ran: done.join(" | "), card: reportCard(provider) });
});

// Report a card's state without touching it, for the before/after of a use case.
Events.On("debug:conn-report", (e: { data: unknown }) => {
  const arg = Array.isArray(e.data) ? e.data[0] : e.data;
  Events.Emit("ui:conn-report", { card: reportCard(String(arg ?? "azure")) });
});

Events.On("debug:exercise-write", (e: { data: unknown }) => {
  const arg = Array.isArray(e.data) ? e.data[0] : e.data;
  const provider = String(arg ?? "");
  void run(
    `debug setProvider(${provider})`,
    $<HTMLElement>("engineStatus"),
    null,
    () => Settings.SetProvider(provider),
  );
});
