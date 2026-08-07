// The Historial view and the Home activity card.
//
// PORTED FROM THE ELECTRON RENDERER, markup and class names included. The stylesheet in index.html
// is that build's verbatim, so it styles `.hrow` / `.hrow-line` / `.hrow-text` / `.hrow-when` and
// `.empty-state`; a first pass at this invented `.hist-item` / `.hist-meta` / `.hist-text` instead
// and produced rows with no styling at all. What the page emits has to match what the CSS expects.
//
// It lives in its own module rather than inside settings.ts — the Electron original was one
// 1828-line file — because the split changes nothing about the DOM, which is what fidelity means
// here.
//
// WHAT IS NOT PORTED and why: the filtering. The original ran `filterHistory` in the renderer; here
// it runs in Go (internal/history.Filter, tested) and the page re-queries instead of holding the
// list in memory. That is the port's whole premise — the rules live on one side — and it is why
// there is no `historyItems` array in this file.
import { Events } from "@wailsio/runtime";
import { t } from "./i18n.js";
import * as History from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/historyservice.js";
import * as Clipboard from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/clipboardservice.js";
import type { HistoryEntry } from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/models.js";

const $ = <T extends HTMLElement>(id: string) =>
  document.getElementById(id) as T | null;

function esc(s: string): string {
  return s.replace(
    /[&<>"]/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c] ?? c,
  );
}

// ---- icons, verbatim from the original ---------------------------------------------

const CHEVRON_DOWN = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
  stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg>`;

const COPY_ICON = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
  stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="14" height="14" x="8" y="8" rx="2"/>
  <path d="M16 4a2 2 0 0 0-2-2h-8a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2"/></svg>`;

const EMPTY_WAVE =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round">' +
  '<path d="M3 12h1.5M7 8.5v7M10.5 5v14M14 8v8M17.5 10.5v3M21 12h-1.5"/></svg>';

const EMPTY_SEARCH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" ' +
  'stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-4.2-4.2"/></svg>';

// ---- time formatting ---------------------------------------------------------------

// The interface language decides the format, not the OS: an English UI showing "25/7/2026" is the
// same bug as an English UI showing "ahora". Carried over from the original's intlLocale().
let appLocale = "es";

// REPAINTS when the language actually moves, and that is not a nicety — it is the fix for a bug the
// user reported: "hace 23h sigue apareciendo" on an English interface.
//
// Two orderings break without it, and they are the same bug from both ends. At startup the history
// is wired and painted before the settings payload lands, so the first render formats in Spanish and
// nothing ever redraws it. And when the user switches language later, these rows are not part of
// what paint() rebuilds, so they would keep the old language until the next dictation.
export function setHistoryLocale(locale: string): void {
  const next = locale || "es";
  if (next === appLocale) return;
  appLocale = next;
  void refreshHistory();
}
function intlLocale(): string {
  return appLocale === "en" ? "en-US" : "es-ES";
}

// relTime is what the Home card shows: recent transcripts read better as "hace 5 min" than as a
// timestamp. The Historial itself uses the absolute date, because there you are looking things up.
//
// Intl.RelativeTimeFormat RATHER THAN CATALOGUE ENTRIES, and the reason is that these strings are
// not really copy — they are a number and a unit, which every language pluralises and orders its own
// way. A catalogue would need "hace {n} min", "hace {n} h" and their plurals per language, and would
// go stale the moment a third language arrived.
//
// `narrow` is what keeps the Spanish EXACTLY as it was — "hace 5 min", "hace 23 h", "hace 3 d",
// byte for byte — while English comes out as "5m ago", "23h ago". Checked against the runtime, not
// assumed: the compact form matters because this sits in a narrow card.
function relTime(at: number): string {
  const mins = Math.floor((Date.now() - at) / 60000);
  // Under a minute is the one case Intl words differently from this app ("este minuto"), so it keeps
  // its own wording and goes through the catalogue like any other sentence.
  if (mins < 1) return t("ahora");
  const rel = new Intl.RelativeTimeFormat(intlLocale(), {
    numeric: "auto",
    style: "narrow",
  });
  if (mins < 60) return rel.format(-mins, "minute");
  const h = Math.floor(mins / 60);
  if (h < 24) return rel.format(-h, "hour");
  const d = Math.floor(h / 24);
  if (d < 7) return rel.format(-d, "day");
  return new Date(at).toLocaleDateString(intlLocale());
}

// ---- empty states -----------------------------------------------------------------

// The trigger and mode the empty state needs, kept in step by paintFromPayload.
let triggerKey = "";
let mode = "hold";
export function setHistoryTriggerContext(trigger: string, m: string): void {
  triggerKey = trigger;
  mode = m;
}

function formatTrigger(trigger: string): string {
  if (trigger === "") return "Sin atajo";
  if (trigger === "fn") return "fn (Globe)";
  const parts = trigger.split("+");
  const key = parts.pop() ?? "";
  const symbols: Record<string, string> = {
    CommandOrControl: "⌘",
    Command: "⌘",
    Control: "⌃",
    Alt: "⌥",
    Option: "⌥",
    Shift: "⇧",
  };
  return parts.map((m) => symbols[m] ?? m + "+").join("") + key;
}

// TWO DISTINCT CASES on purpose, as in the original: "nothing recorded yet" has to teach the user
// how to dictate, while "your filter matched nothing" must NOT — telling someone to hold fn when
// they simply mistyped a search is noise.
function emptyStateHTML(kind: "none" | "nomatch"): string {
  if (kind === "nomatch") {
    return (
      '<div class="empty-state">' +
      `<span class="empty-icon">${EMPTY_SEARCH}</span>` +
      `<div class="empty-title">Sin resultados</div>` +
      `<div class="empty-desc">Prueba con otra búsqueda o cambia el filtro de fecha.</div>` +
      "</div>"
    );
  }
  const key = `<b>${esc(formatTrigger(triggerKey))}</b>`;
  // The instruction must match the trigger actually configured: hold only works with fn, and with
  // no shortcut at all there is no key to press.
  const desc =
    triggerKey === ""
      ? t("Usa el ícono de la barra de menús o “Probar dictado” para crear la primera.")
      : mode === "toggle"
        ? `Pulsa ${key} y habla para crear la primera.`
        : `Mantén ${key} y habla para crear la primera.`;
  return (
    '<div class="empty-state">' +
    `<span class="empty-icon">${EMPTY_WAVE}</span>` +
    `<div class="empty-title">Aún no hay transcripciones</div>` +
    `<div class="empty-desc">${desc}</div>` +
    "</div>"
  );
}

// ---- the Historial list -----------------------------------------------------------

// historyRow builds one row, element by element as the original did — not from an HTML string.
// That is not a style preference: the row carries click handlers and its text is set with
// textContent, so a transcript containing markup cannot become markup.
function historyRow(it: HistoryEntry): HTMLElement {
  const row = document.createElement("div");
  row.className = "hrow";

  const line = document.createElement("div");
  line.className = "hrow-line";

  if (it.language) {
    const lang = document.createElement("span");
    lang.className = "hist-lang";
    lang.textContent = it.language;
    line.appendChild(lang);
  }

  const text = document.createElement("div");
  text.className = "hrow-text";
  text.textContent = it.text || "";
  line.appendChild(text);

  const when = document.createElement("span");
  when.className = "hrow-when";
  when.textContent = it.at ? new Date(it.at).toLocaleString(intlLocale()) : "";
  line.appendChild(when);

  // The expand toggle. Kept as a hidden PLACEHOLDER when the text already fits, so every row's
  // copy button still lines up in the same column — a toggle that appears and disappears would
  // make the column ragged.
  const expand = document.createElement("button");
  expand.className = "icon-btn placeholder";
  expand.innerHTML = CHEVRON_DOWN;
  expand.title = "Ver completo";
  expand.setAttribute("aria-expanded", "false");
  expand.onclick = () => {
    const open = row.classList.toggle("open");
    expand.setAttribute("aria-expanded", String(open));
    expand.title = open ? "Ver menos" : "Ver completo";
    const svg = expand.querySelector("svg");
    if (svg) svg.style.transform = open ? "rotate(180deg)" : "";
  };
  line.appendChild(expand);

  const copy = document.createElement("button");
  copy.className = "btn small copy-btn";
  copy.innerHTML = COPY_ICON + `<span>copiar</span>`;
  copy.onclick = async () => {
    const label = copy.querySelector("span");
    try {
      const failure = await Clipboard.Copy(it.text || "");
      if (failure) throw new Error(failure);
      if (label) label.textContent = "copiado";
    } catch (e) {
      // Say what failed and what to do instead of a bare "error": the text is still on screen, so
      // selecting it by hand always works as a fallback.
      if (label) label.textContent = t("no se copió");
      copy.title = `No se pudo copiar: ${e instanceof Error ? e.message : String(e)}. Puedes seleccionar el texto y copiarlo a mano.`;
      copy.classList.add("copy-failed");
    }
    setTimeout(() => {
      if (label) label.textContent = "copiar";
      copy.classList.remove("copy-failed");
    }, 2600);
  };
  line.appendChild(copy);

  row.appendChild(line);
  return row;
}

// Show the expand toggle only on rows whose text is actually clipped. Needs layout, so it runs
// after the rows are in the DOM — and again on resize, since a wider window can un-clip a row and a
// narrower one clip it.
function markTruncatedRows(box: HTMLElement): void {
  for (const row of box.querySelectorAll<HTMLElement>(".hrow")) {
    const text = row.querySelector<HTMLElement>(".hrow-text");
    const btn = row.querySelector<HTMLElement>(".icon-btn");
    if (!text || !btn) continue;
    if (row.classList.contains("open")) continue; // expanded: keep the toggle
    const clipped = text.scrollWidth > text.clientWidth + 1;
    btn.classList.toggle("placeholder", !clipped);
  }
}

// listCap matches the original: a hundred rows is already more than anyone scrolls, and building
// every row of a long history costs layout on a view that is opened constantly.
const listCap = 100;

function paintHistory(page: {
  entries: HistoryEntry[] | null;
  total: number;
}): void {
  const box = $<HTMLElement>("historyBox");
  if (!box) return;
  const entries = page.entries ?? [];
  if (entries.length === 0) {
    // Which empty state depends on whether a filter is on — Total is what tells them apart, and it
    // comes from the backend for exactly this.
    const filtering =
      ($<HTMLInputElement>("histSearch")?.value ?? "").trim() !== "" ||
      ($<HTMLSelectElement>("histRange")?.value ?? "all") !== "all";
    box.innerHTML = emptyStateHTML(filtering ? "nomatch" : "none");
    Events.Emit("ui:history", { shown: 0, total: page.total });
    return;
  }
  box.innerHTML = "";
  for (const it of entries.slice(0, listCap)) box.appendChild(historyRow(it));
  markTruncatedRows(box);
  reportShape("ui:hist-shape", box, ".hrow");
  Events.Emit("ui:history", { shown: entries.length, total: page.total });
}

// reportShape announces the CLASS NAMES a rendered row uses, never its text.
//
// It exists because fidelity here means "the DOM matches what the stylesheet expects", and that is
// checkable — a first pass at this emitted invented class names and produced unstyled rows that
// looked broken. Class names only: the log must never carry a transcript.
function reportShape(
  event: string,
  box: HTMLElement,
  rowSelector: string,
): void {
  const row = box.querySelector<HTMLElement>(rowSelector);
  const classes = row
    ? Array.from(row.querySelectorAll<HTMLElement>("*"))
        .map((el) => el.className)
        .filter((c) => typeof c === "string" && c !== "")
    : [];
  Events.Emit(event, {
    rows: box.querySelectorAll(rowSelector).length,
    // NOT translated: this is diagnostic output, and a debug report that changes wording with the
    // interface language is harder to grep, not friendlier.
    rowClass: row?.className ?? "(none)",
    childClasses: classes.join(" | "),
    // The relative timestamp as rendered. A time, never transcript text — and the only way to see
    // from outside that the language reached this formatter.
    when: row?.querySelector(".hist-when,.hrow-when")?.textContent ?? "",
  });
}

function paintRecent(page: { entries: HistoryEntry[] | null }): void {
  const box = $<HTMLElement>("homeRecent");
  if (!box) return;
  const entries = page.entries ?? [];
  box.innerHTML = entries.length
    ? entries
        .map((it) => {
          const lang = it.language
            ? `<span class="hist-lang">${esc(it.language)}</span>`
            : "";
          const text = `<span class="grow">${esc(String(it.text || "").slice(0, 140))}</span>`;
          const when = `<span class="hist-when">${esc(relTime(it.at))}</span>`;
          return `<div class="hist-item">${lang}${text}${when}</div>`;
        })
        .join("")
    : emptyStateHTML("none");
  reportShape("ui:recent-shape", box, ".hist-item");
}

// refresh re-reads both lists from the CURRENT filter controls, so a transcript landing while a
// search is typed is not silently ignored.
export async function refreshHistory(): Promise<void> {
  const query = $<HTMLInputElement>("histSearch")?.value ?? "";
  const range = $<HTMLSelectElement>("histRange")?.value ?? "all";
  try {
    paintHistory(await History.List(query, range));
    paintRecent(await History.Recent());
  } catch (err) {
    const box = $<HTMLElement>("historyBox");
    if (box)
      box.innerHTML = `<div class="empty-state"><div class="empty-title">No se pudo leer el historial</div><div class="empty-desc">${esc(String(err))}</div></div>`;
  }
}

export function wireHistory(): void {
  // Filtering is a re-query, not a client-side pass over a cached list: the rules live in Go and
  // the page must not grow a second opinion about what "Hoy" means.
  $<HTMLInputElement>("histSearch")?.addEventListener("input", () => {
    void refreshHistory();
  });
  $<HTMLSelectElement>("histRange")?.addEventListener("change", () => {
    void refreshHistory();
  });

  const menuButton = $<HTMLButtonElement>("histMenuBtn");
  const menu = $<HTMLElement>("histMenu");
  menuButton?.addEventListener("click", () => {
    if (!menu) return;
    menu.hidden = !menu.hidden;
    menuButton.setAttribute("aria-expanded", String(!menu.hidden));
  });

  $<HTMLButtonElement>("clearHistory")?.addEventListener("click", () => {
    if (menu) menu.hidden = true;
    menuButton?.setAttribute("aria-expanded", "false");
    void History.Clear().then(
      () => void refreshHistory(),
      (err: unknown) =>
        Events.Emit("ui:action", {
          action: "history.clear",
          ok: false,
          error: String(err),
        }),
    );
  });

  // The engine announces every stored transcript, so the list stays live while the window is open —
  // the case that made the history look lost: dictating with Historial already on screen.
  Events.On("history:changed", () => {
    void refreshHistory();
  });

  // A wider window can un-clip a row, so the toggles are re-evaluated on resize.
  const box = $<HTMLElement>("historyBox");
  if (box) new ResizeObserver(() => markTruncatedRows(box)).observe(box);
}
