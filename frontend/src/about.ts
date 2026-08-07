// The Acerca de view: version, machine and file locations.
//
// PORTED FROM the Electron renderAbout, class names included (.arow / .ak / .av) — the stylesheet in
// index.html is the original's, so inventing names here would produce unstyled rows, which is exactly
// how the first attempt at Historial went wrong.
//
// This module DECIDES NOTHING. What each row says, when a build counts as development, and what an
// unanswered question looks like all live in internal/app/about.go, where they are tested. Here the
// rows arrive already resolved and are only turned into DOM.
import { Events } from "@wailsio/runtime";
import { t } from "./i18n.js";
import * as About from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/aboutservice.js";
import type { AboutRow } from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/models.js";

const $ = <T extends HTMLElement>(id: string) =>
  document.getElementById(id) as T | null;

// Built with textContent rather than innerHTML: these values are file paths and a locale, and one of
// them comes from the user's own home directory — a name containing "<" would otherwise break the
// markup around it.
function renderRows(box: HTMLElement | null, rows: AboutRow[] | null): void {
  if (!box) return;
  box.innerHTML = "";
  for (const row of rows ?? []) {
    const line = document.createElement("div");
    line.className = "arow";
    const key = document.createElement("span");
    key.className = "ak";
    key.textContent = row.key;
    const value = document.createElement("span");
    value.className = "av";
    value.textContent = row.value;
    line.append(key, value);
    box.appendChild(line);
  }
}

export async function paintAbout(): Promise<void> {
  try {
    const info = await About.Info();
    const version = $<HTMLElement>("aboutVersion");
    if (version) version.textContent = info.versionLabel;
    renderRows($<HTMLElement>("aboutSystem"), info.system);
    renderRows($<HTMLElement>("aboutPaths"), info.paths);
    Events.Emit("ui:about", {
      version: info.versionLabel,
      systemRows: (info.system ?? []).length,
      pathRows: (info.paths ?? []).length,
    });
  } catch (err) {
    // The view has to say something: this is where a user goes to read what went wrong, so a
    // silent empty panel is the one outcome that defeats its purpose.
    const version = $<HTMLElement>("aboutVersion");
    if (version) version.textContent = t("No se pudo leer la información de la app");
    Events.Emit("ui:about", { error: String(err) });
  }
}
