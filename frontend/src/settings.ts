// Settings / app-shell renderer.
//
// PORT IN PROGRESS. The markup in index.html is the Electron page verbatim (1249 lines). Wired so
// far: the sidebar navigation, the Home record button, and the loop that makes the app CONFIGURABLE
// at all — the engine picker, the API key fields and the Azure region. Everything else is inert.
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
// Remaining for this view: the Azure subservice switch (speech vs openai), language pickers,
// trigger key, appearance, input device, onboarding, history and About. See
// docs/plans/loqui-go-port.md, phase 4.
import { Events } from "@wailsio/runtime";
import * as Settings from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/settingsservice.js";
import * as Dictation from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/dictationservice.js";
import type {
  SettingsPayload,
  WriteResult,
} from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/models.js";

console.info("[loqui] settings shell loaded (partial port: engine + keys)");

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
  const report = $<HTMLElement>("openReport");
  report?.addEventListener("click", () =>
    showView(report.dataset.view ?? "report"),
  );
}

// ---- the record button -------------------------------------------------------------

// setDictating reflects the engine's state in the Home button and on the root element.
function setDictating(active: boolean): void {
  document.documentElement.dataset.dictating = String(active);
  const label =
    $<HTMLElement>("testDictate")?.querySelector<HTMLElement>(".btn-label");
  if (label) label.textContent = active ? "Detener" : "Probar dictado";
}

// The engine is the authority on whether a dictation is running: it can start from the trigger key
// or the tray with this window never involved, and it can stop on its own (the idle guard).
Events.On("dictation:state", (e: { data: boolean | boolean[] }) => {
  const active = Array.isArray(e.data) ? e.data[0] : e.data;
  setDictating(!!active);
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
function paint(p: SettingsPayload): void {
  const statusBySlot = new Map((p.keys ?? []).map((k) => [k.slot, k]));

  // The engine picker. Options come from Go rather than the hardcoded markup list, so an engine
  // the backend does not know cannot be selected — the markup offers six and Go is the authority
  // on which are real.
  const home = $<HTMLSelectElement>("homeEngine");
  if (home) {
    const providers = p.providers ?? [];
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
          const suffix = prov.available ? "" : " — no disponible aún";
          const disabled = prov.available ? "" : " disabled";
          return `<option value="${prov.id}"${disabled}>${label}${suffix}</option>`;
        })
        .join("");
    }
    home.value = p.provider;
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

  const availability = new Map(
    (p.providers ?? []).map((prov) => [prov.id, prov.available]),
  );

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
    const active = provider === p.provider;
    // Unknown to the backend counts as unavailable: a card in the markup for an engine Go does not
    // list is not something to offer.
    const available = availability.get(provider) ?? false;
    card.classList.toggle("is-active", active);
    // The markup already styles .is-unsupported by hiding the Configurar button.
    card.classList.toggle("is-unsupported", !available);

    const slot = KEY_SLOT_BY_PROVIDER[provider];
    const key = slot ? statusBySlot.get(slot) : undefined;

    const state = card.querySelector<HTMLElement>(".conn-state");
    if (state)
      state.textContent = available
        ? connStateLabel(active, key)
        : "no disponible aún";

    // "Usar este motor" is pointless on the engine already in use, and must not be OFFERED for one
    // that cannot dictate. The backend refuses it either way; presenting a button whose outcome is
    // known to be an error is just a worse way to say the same thing.
    const use = card.querySelector<HTMLButtonElement>(".conn-use");
    if (use) use.disabled = active || !available;

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
}

// connStateLabel is what a card's badge says.
//
// An unreadable credential gets its OWN wording rather than "sin clave". The two send the user to
// completely different places — one means type your key in, the other means the signing identity
// is broken and typing it in will not help — and telling someone to retype a key that is already
// stored is worse than saying nothing.
function connStateLabel(
  active: boolean,
  k?: { status: string; fromEnv: boolean },
): string {
  if (!k) return active ? "en uso" : "";
  const prefix = active ? "en uso · " : "";
  switch (k.status) {
    case "present":
      return (
        prefix +
        (k.fromEnv ? "clave por variable de entorno" : "clave guardada")
      );
    case "absent":
      return prefix + "sin clave";
    default:
      return prefix + "no se pudo leer el Keychain";
  }
}

function keyStateLabel(status?: string, fromEnv?: boolean): string {
  switch (status) {
    case "present":
      return fromEnv
        ? "(definida por variable de entorno — no se puede borrar desde aquí)"
        : "(guardada — escribe una nueva para reemplazarla)";
    case "absent":
      return "(no configurada)";
    case "unreadable":
      return "(el Keychain no respondió — la app no está firmada con una identidad estable)";
    default:
      return "";
  }
}

// ---- acting ----------------------------------------------------------------------

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
): Promise<void> {
  if (status) status.textContent = "…";
  const wasDisabled = trigger?.disabled ?? false;
  if (trigger) trigger.disabled = true;
  // The ENTIRE sequence goes in the queue — the call, the recovery load and the repaint. Wrapping
  // only the call let the queue advance the moment it rejected, so a later write could finish while
  // the failed one was still fetching its recovery payload, and that stale payload painted last.
  await serialize(async () => {
    try {
      const res = await action();
      // Busy state released BEFORE painting, so paint() decides the final enabled state. The other
      // order silently undid it: paint() disables the now-active "Usar este motor" and the delete
      // button of a key that is gone, and re-enabling afterwards handed both back to the user.
      if (trigger) trigger.disabled = false;
      paint(res.payload);
      if (status) status.textContent = res.error;
      Events.Emit("ui:action", {
        action: label,
        ok: res.error === "",
        error: res.error,
      });
    } catch (err) {
      // A thrown error here is a TRANSPORT failure, not a rejected write: the binding itself did not
      // complete, so there is no payload and the page's picture is now unknown. Re-read it.
      const msg = String(err instanceof Error ? err.message : err);
      if (status) status.textContent = msg;
      Events.Emit("ui:action", { action: label, ok: false, error: msg });
      try {
        // The payload is awaited BEFORE the control is released. Releasing first would leave it
        // actionable for the whole round trip at the one moment the page's state is explicitly
        // unknown — long enough to start a second write on top of the one that just failed.
        const payload = await Settings.Load();
        if (trigger) trigger.disabled = false;
        paint(payload);
      } catch {
        // The backend is unreachable, so there is no authoritative state to paint from. Restore the
        // control to what it was rather than enabling it: guessing "enabled" would offer an action
        // whose precondition is now unknown.
        if (trigger) trigger.disabled = wasDisabled;
      }
    }
  });
}

function wire(): void {
  const engineStatus = $<HTMLElement>("engineStatus");
  const home = $<HTMLSelectElement>("homeEngine");
  home?.addEventListener("change", (e) => {
    const value = (e.target as HTMLSelectElement).value;
    void run(`setProvider(${value})`, engineStatus, home, () =>
      Settings.SetProvider(value),
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

    const use = card.querySelector<HTMLButtonElement>(".conn-use");
    use?.addEventListener("click", () => {
      void run(`setProvider(${provider})`, status, use, () =>
        Settings.SetProvider(provider),
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
      void run(`saveConnection(${provider})`, status, save, () =>
        Settings.SaveConnection(slot, regionValue, secret),
      );
    });

    const del = card.querySelector<HTMLButtonElement>(".conn-delete");
    del?.addEventListener("click", () => {
      if (!slot) return;
      void run(`deleteKey(${provider})`, status, del, () =>
        Settings.DeleteKey(slot),
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
wireRecordButton();

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
      paint(payload);
      wire();
      Events.Emit("ui:painted", { provider: payload.provider });
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
Events.On("debug:navigate", (e: { data: unknown }) => {
  const arg = Array.isArray(e.data) ? e.data[0] : e.data;
  const want = String(arg ?? "");
  const item = document.querySelector<HTMLElement>(
    `.nav-item[data-view="${want}"]`,
  );
  if (!item) {
    Events.Emit("ui:nav-probe", { requested: want, error: "no such nav item" });
    return;
  }
  item.click();
  const visible = Array.from(
    document.querySelectorAll<HTMLElement>("section.view.active"),
  ).map((v) => v.dataset.view);
  Events.Emit("ui:nav-probe", {
    requested: want,
    visible,
    navActive:
      document.querySelector<HTMLElement>(".nav-item.active")?.dataset.view,
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
