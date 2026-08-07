// The Permisos tab.
//
// PORTED FROM renderPerms, class names included (.prow / .ptile / .pmid / .pname / .pdesc / .pchip).
// The rows, their states, labels and which action each button performs all come from Go — see
// internal/app/permission_rows.go. This file draws and reports.
//
// RE-READ ON EVERY ACTION, not painted once from a snapshot. Permissions change from OUTSIDE the app:
// the user grants Accessibility in System Settings and comes back, and a value baked in at load would
// be stale exactly when it matters.
import * as Permissions from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/permissionsservice.js";
import type { PermissionsPage } from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/models.js";
import { Events } from "@wailsio/runtime";
import { t } from "./i18n.js";

const $ = <T extends HTMLElement>(id: string) =>
  document.getElementById(id) as T | null;

function esc(s: string): string {
  return s.replace(
    /[&<>"]/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c] ?? c,
  );
}

const svg = (paths: string): string =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
    stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;

const ARROW_OUT = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
  stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M7 7h10v10"/><path d="M7 17 17 7"/></svg>`;

// The icons, verbatim from the original.
const PERM_ICONS: Record<string, string> = {
  microphone: `<path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><path d="M12 19v3"/>`,
  accessibility: `<circle cx="16" cy="4" r="1"/><path d="m18 19 1-7-6 1"/><path d="m5 8 3-3 5.5 3-2.36 3.5"/><path d="M4.24 14.5a5 5 0 0 0 6.88 6"/><path d="M13.76 17.5a5 5 0 0 0-6.88-6"/>`,
  inputMonitoring: `<rect width="20" height="14" x="2" y="5" rx="2"/><path d="M6 9h.01M10 9h.01M14 9h.01M18 9h.01M8 13h.01M12 13h.01M16 13h.01M7 17h10"/>`,
  speechRecognition: `<path d="M2 10v4"/><path d="M6 6v12"/><path d="M10 3v18"/><path d="M14 8v8"/><path d="M18 5v14"/><path d="M22 10v4"/>`,
};

const CHIP_ICONS: Record<string, string> = {
  granted: `<path d="M20 6 9 17l-5-5"/>`,
  missing: `<circle cx="12" cy="12" r="10"/><path d="M12 8v4"/><path d="M12 16h.01"/>`,
  unknown: `<circle cx="12" cy="12" r="10"/><path d="M9.5 9a2.5 2.5 0 1 1 3.5 2.3c-.9.5-1 1-1 1.7"/><path d="M12 17h.01"/>`,
};

// onChanged lets the rest of the page know a grant may have moved: the device list only gets real
// microphone LABELS once the mic is granted, so it has to be re-read after a successful request.
type OnChanged = () => void;
let onChanged: OnChanged = () => {};
export function setPermissionsChangeHandler(fn: OnChanged): void {
  onChanged = fn;
}

// Where the rows are painted. The onboarding wizard shows the SAME rows in its own container, and
// they must not be a second copy of this DOM: the states travel as class names the stylesheet owns,
// and a duplicate drifts the moment either side is touched. So the container is a parameter.
let mounts: string[] = ["perms"];

/** Also paint the rows into `id` from now on — used by the tutorial's Permisos step. */
export function addPermissionsMount(id: string): void {
  if (!mounts.includes(id)) mounts = [...mounts, id];
}

function paint(page: PermissionsPage): void {
  for (const id of mounts) paintInto(id, page);
}

function paintInto(mountId: string, page: PermissionsPage): void {
  const box = $<HTMLElement>(mountId);
  if (!box) return;
  const rows = page.rows ?? [];

  box.innerHTML = "";
  for (const r of rows) {
    const row = document.createElement("div");
    // The STATE travels as a class, which is what colours the row and its chip. Setting only text
    // would leave every row looking identical regardless of whether it blocks dictation.
    row.className = "prow " + r.state;

    const tile = document.createElement("span");
    tile.className = "ptile";
    tile.innerHTML = svg(PERM_ICONS[r.id] ?? "");
    row.appendChild(tile);

    const mid = document.createElement("div");
    mid.className = "pmid";
    mid.innerHTML = `<div class="pname">${esc(r.name)}</div><div class="pdesc">${esc(r.desc)}</div>`;
    row.appendChild(mid);

    const chip = document.createElement("span");
    chip.className = "pchip";
    chip.innerHTML = svg(CHIP_ICONS[r.state] ?? "") + esc(r.label);
    // The one state the app cannot read — say WHY rather than imply a problem.
    if (r.state === "unknown") {
      chip.title = t("macOS no permite consultar este permiso desde la app");
    }
    row.appendChild(chip);

    const btn = document.createElement("button");
    const grantable = r.action === "request";
    // Only a REQUIRED and missing permission gets the primary button: a recommended one should not
    // shout as loudly as something that actually blocks dictation.
    btn.className =
      "btn small" + (r.state === "missing" && r.required ? " primary" : "");
    btn.innerHTML =
      esc(grantable ? "Conceder acceso" : "Abrir Ajustes") + ARROW_OUT;
    btn.onclick = () => {
      btn.disabled = true;
      const call = grantable
        ? Permissions.Request(r.id)
        : Permissions.Open(r.id);
      void call.then(
        (next) => {
          paint(next);
          // The microphone grant is what makes real device NAMES available, so the input picker is
          // stale until this happens.
          onChanged();
        },
        () => {
          btn.disabled = false;
        },
      );
    };
    row.appendChild(btn);

    box.appendChild(row);
  }

  // "Volver a comprobar" is the only way to notice a grant changed in System Settings while this
  // window stayed open.
  const foot = document.createElement("div");
  foot.className = "prow";
  const recheck = document.createElement("button");
  recheck.className = "btn small";
  recheck.textContent = "Volver a comprobar";
  recheck.onclick = () => void refreshPermissions();
  foot.append(recheck);
  box.appendChild(foot);

  const status = $<HTMLElement>("permsStatus");
  if (status) {
    const missing = page.missing ?? [];
    status.className = "status " + (page.allReady ? "ok" : "err");
    status.textContent = page.allReady
      ? t("✓ Todos los permisos requeridos están concedidos")
      : "Faltan permisos requeridos: " + missing.join(", ");
  }

  Events.Emit("ui:perms", {
    rows: rows.map((r) => `${r.id}=${r.state}/${r.action}`).join(" "),
    allReady: page.allReady,
  });
}

export async function refreshPermissions(): Promise<void> {
  try {
    paint(await Permissions.List());
  } catch (err) {
    const status = $<HTMLElement>("permsStatus");
    if (status) {
      status.className = "status err";
      status.textContent = t("No se pudieron leer los permisos: ") + String(err);
    }
  }
}

export function wirePermissions(): void {
  // Read when the tab is opened, because that is when the answer matters and when it is most likely
  // to have changed since the window was loaded.
  for (const tab of document.querySelectorAll<HTMLElement>(
    '.tab[data-tab="permisos"]',
  )) {
    tab.addEventListener("click", () => void refreshPermissions());
  }
}
