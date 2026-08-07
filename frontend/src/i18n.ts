// Translating the markup.
//
// The catalogue comes from Go — one table, so it cannot drift from what the services themselves
// emit. This module only APPLIES it: it never decides what a string means.
//
// KEYS ARE THE SPANISH SOURCE STRINGS, which is what makes the whole thing safe: a string with no
// entry is simply left as it was, and reads as ordinary Spanish rather than as a hole or a leaked
// key name.

import { Events } from "@wailsio/runtime";
import * as Settings from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/settingsservice.js";

// THE ORIGINAL SPANISH HAS TO BE STASHED ON THE FIRST PASS.
//
// After one translation the element's text is English, so it is no longer its own key: a second
// pass would look up "Connections", miss, and leave it — and switching back to Spanish would be
// impossible. `data-i18n-src` keeps the key that was authored into the markup.
// getAttribute/setAttribute rather than `dataset`, and this is not a style choice: a DOMStringMap
// key containing a hyphen followed by a lowercase letter throws SyntaxError. `dataset["i18nSrc-
// placeholder"]` does exactly that, which took down the entire first paint — the page reported
// "painting failed" and rendered nothing at all.
const SRC_PREFIX = "data-i18n-src";

function stash(el: HTMLElement, name: string, current: string): string {
  const existing = el.getAttribute(name);
  if (existing !== null) return existing;
  el.setAttribute(name, current);
  return current;
}

function sourceText(el: HTMLElement): string {
  return stash(el, SRC_PREFIX, (el.textContent ?? "").trim());
}

function sourceAttr(el: HTMLElement, attr: string): string {
  return stash(el, `${SRC_PREFIX}-${attr}`, el.getAttribute(attr) ?? "");
}

let catalog: Record<string, string | undefined> = {};
let locale = "es";

/** The language in effect, for anything that formats rather than translates (dates). */
export function currentLocale(): string {
  return locale;
}

// setText writes a NEW Spanish source string into a marked element and shows it translated.
//
// Required for every marked node whose text changes at runtime, and getting this wrong is worse than
// not translating at all. The stash below records the first text as that element's key forever, so a
// node that later becomes a different sentence would keep being translated as the OLD one: #wizNext
// turns "Continuar" into "Empezar" on the last step and would have read "Continue"; the record
// button alternates "Probar dictado" and "Detener" and could cache either, depending on which
// promise resolved first at startup. Found in design review.
export function setText(el: HTMLElement | null | undefined, spanish: string): void {
  if (!el) return;
  el.setAttribute("data-i18n-src", spanish);
  el.textContent = catalog[spanish] ?? spanish;
}

/** Translate one Spanish source string. Falls back to the string itself — see the header. */
export function t(key: string): string {
  return catalog[key] ?? key;
}

// applyTranslations walks the marked elements and attributes and rewrites them from the catalogue.
//
// Called after EVERY paint, not once: paint() rebuilds parts of the page (the engine picker's
// options, the connection rows, the language chips) and freshly written markup would otherwise stay
// Spanish while the rest of the page is English.
export function applyTranslations(root: ParentNode = document): void {
  for (const el of Array.from(root.querySelectorAll<HTMLElement>("[data-i18n]"))) {
    const key = sourceText(el);
    if (key === "") continue;
    const translated = catalog[key];
    // Only WRITE when there is something to change: touching textContent on every paint would fight
    // with anything else that owns this node, and would blow away child elements if one ever
    // appears inside a marked node.
    if (translated !== undefined && el.textContent !== translated) {
      el.textContent = translated;
    } else if (translated === undefined && el.textContent !== key) {
      // Back to Spanish, or a string that has no translation: restore what was authored.
      el.textContent = key;
    }
  }
  for (const el of Array.from(root.querySelectorAll<HTMLElement>("[data-i18n-attr]"))) {
    for (const attr of (el.getAttribute("data-i18n-attr") ?? "").split(",")) {
      const name = attr.trim();
      if (name === "") continue;
      const key = sourceAttr(el, name);
      if (key === "") continue;
      el.setAttribute(name, catalog[key] ?? key);
    }
  }
}

// loadTranslations fetches the table for the language in effect and applies it.
//
// Awaited by the caller before the first paint where possible, so the user does not see the page in
// Spanish for a frame and then watch it change under them.
// A generation counter, for the same reason the payload has a revision: two language changes in
// quick succession produce two in-flight fetches, and the one that ANSWERS last is not necessarily
// the one that was asked last. Applying it would leave the static markup in one language and the
// Go-rendered rows in the other. Found in design review.
let generation = 0;

export async function loadTranslations(): Promise<void> {
  const mine = ++generation;
  const res = await Settings.Translations();
  if (mine !== generation) return; // a newer request is already on its way; this answer is stale
  locale = res.locale ?? "es";
  catalog = (res.catalog ?? {}) as Record<string, string | undefined>;
  applyTranslations();
  report();
}

// What actually happened, for the log and for the E2E. Counts and one sample, never the catalogue:
// the point is to answer "did the page really translate itself" from outside, and today there is no
// other way to see it — every card report echoes strings that come from Go, not from the markup.
function report(): void {
  const marked = document.querySelectorAll("[data-i18n]").length;
  let changed = 0;
  for (const el of Array.from(document.querySelectorAll<HTMLElement>("[data-i18n]"))) {
    const key = el.getAttribute("data-i18n-src");
    if (key !== null && (el.textContent ?? "") !== key) changed++;
  }
  Events.Emit("ui:i18n", {
    locale,
    entries: Object.keys(catalog).length,
    marked,
    translated: changed,
    sample: document.querySelector("[data-i18n]")?.textContent ?? "",
  });
}
