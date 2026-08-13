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
import { setText, t } from "./i18n.js";
import * as About from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/aboutservice.js";
import * as Updates from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/updateservice.js";
import type {
  AboutRow,
  UpdateStatus,
} from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/models.js";

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

let updateControlsWired = false;
let lastUpdateStatus: UpdateStatus | null = null;

function failedUpdateStatus(error: string): UpdateStatus {
  return {
    state: "error",
    currentVersion: lastUpdateStatus?.currentVersion ?? "",
    availableVersion: lastUpdateStatus?.availableVersion ?? "",
    name: lastUpdateStatus?.name ?? "",
    notes: lastUpdateStatus?.notes ?? "",
    artifact: lastUpdateStatus?.artifact ?? "",
    error,
    ready: false,
  };
}

function eventStatus(data: unknown): UpdateStatus | null {
  const value = Array.isArray(data) ? data[0] : data;
  if (!value || typeof value !== "object") return null;
  return value as UpdateStatus;
}

function updateStatusLabel(label: HTMLElement, state: string): void {
  switch (state) {
    case "checking":
      setText(label, "Buscando actualizaciones…");
      return;
    case "up-to-date":
      setText(label, "Loqui está actualizado");
      return;
    case "available":
      setText(label, "Hay una actualización disponible");
      return;
    case "installing":
      setText(label, "Descargando actualización…");
      return;
    case "ready":
      setText(label, "Actualización lista para reiniciar");
      return;
    case "restarting":
      setText(label, "Reiniciando Loqui…");
      return;
    case "error":
      setText(label, "No se pudo completar la actualización");
      return;
    case "unavailable":
      setText(label, "Las actualizaciones no están disponibles en esta compilación");
      return;
    default:
      setText(label, "Comprobar si hay actualizaciones");
  }
}

function renderUpdateStatus(status: UpdateStatus | null): void {
  lastUpdateStatus = status;
  const state = status?.state ?? "unavailable";
  const label = $<HTMLElement>("aboutUpdateStatus");
  if (label) {
    updateStatusLabel(label, state);
    label.className = "srow-label update-state " + state;
  }

  const detail = $<HTMLElement>("aboutUpdateDetail");
  if (detail) {
    detail.textContent = "";
    if (status?.error) {
      detail.textContent = t(status.error);
    } else if (status?.availableVersion) {
      const name = status.name ? ` — ${status.name}` : "";
      detail.textContent = `${status.availableVersion}${name}`;
    } else if (state === "checking" || state === "installing" || state === "restarting") {
      detail.textContent = t("Espera un momento…");
    }
  }

  const check = $<HTMLButtonElement>("aboutCheckUpdates");
  const install = $<HTMLButtonElement>("aboutInstallUpdate");
  const restart = $<HTMLButtonElement>("aboutRestartUpdate");
  if (check) check.disabled = state === "checking" || state === "installing" || state === "restarting";
  if (install) install.hidden = state !== "available";
  if (restart) restart.hidden = state !== "ready";

  Events.Emit("ui:updates", {
    state,
    availableVersion: status?.availableVersion ?? "",
    ready: status?.ready === true,
  });
}

function wireUpdateControls(): void {
  if (updateControlsWired) return;
  updateControlsWired = true;
  const check = $<HTMLButtonElement>("aboutCheckUpdates");
  const install = $<HTMLButtonElement>("aboutInstallUpdate");
  const restart = $<HTMLButtonElement>("aboutRestartUpdate");

  check?.addEventListener("click", () => {
    check.disabled = true;
    void Updates.Check().then(
      (result) => renderUpdateStatus(result.status),
      (err) => {
        renderUpdateStatus(failedUpdateStatus(String(err)));
      },
    );
  });

  install?.addEventListener("click", () => {
    if (!window.confirm(t("¿Quieres descargar e instalar esta actualización?"))) return;
    install.disabled = true;
    void Updates.Install().then(
      (result) => renderUpdateStatus(result.status),
      (err) => {
        renderUpdateStatus(failedUpdateStatus(String(err)));
      },
    );
  });

  restart?.addEventListener("click", () => {
    if (!window.confirm(t("¿Quieres reiniciar Loqui para aplicar la actualización?"))) return;
    restart.disabled = true;
    void Updates.Restart().then(
      (result) => renderUpdateStatus(result.status),
      (err) => {
        renderUpdateStatus(failedUpdateStatus(String(err)));
      },
    );
  });
}

for (const event of [
  "updates:checking",
  "updates:up-to-date",
  "updates:available",
  "updates:installing",
  "updates:ready",
  "updates:error",
]) {
  Events.On(event, (e: { data: unknown }) => {
    const status = eventStatus(e.data);
    if (status) renderUpdateStatus(status);
  });
}

export async function paintAbout(): Promise<void> {
  try {
    wireUpdateControls();
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

  try {
    wireUpdateControls();
    renderUpdateStatus(await Updates.Status());
  } catch (err) {
    renderUpdateStatus(failedUpdateStatus(String(err)));
  }
}
