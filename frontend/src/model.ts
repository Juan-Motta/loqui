// The whisper model row.
//
// PORTED FROM THE ELECTRON RENDERER, and the class names are the contract: the stylesheet in
// index.html is that build's verbatim and already styles `.model-line`, `.model-state.ready/.warn`,
// `.model-bar > span` and `.model-sub`. Emitting anything else produces an unstyled row — the same
// mistake that once invented `.hist-item` for the history and shipped rows with no styling at all.
//
// THE PAGE DECIDES NOTHING HERE. The sentence and the class that colours it both come from Go
// (ModelStatus.stateText / .stateClass), because "is this model usable" is the same question whisper
// itself asks and there must be one answer to it.

import { Events } from "@wailsio/runtime";
import { t } from "./i18n.js";
import * as Model from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/modelservice.js";
import type { ModelStatus } from "../bindings/github.com/Juan-Motta/loqui-go/internal/app/models.js";

const $$ = <T extends HTMLElement>(sel: string) =>
  Array.from(document.querySelectorAll<T>(sel));

// Progress lives here, not in the status: it arrives as events, many per second, and refetching the
// status for each one would hammer the disk for a number the event already carries.
let progress: { percent: number; text: string } | null = null;

function escapeHtml(s: string): string {
  return s.replace(
    /[&<>"]/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c] ?? c,
  );
}

// renderInto draws the row into ONE container. There are two — the Conexiones card and the tutorial —
// and both must show the same thing, which is why the original drew into every `.model-box` rather
// than into an id.
function renderInto(box: HTMLElement, st: ModelStatus): void {
  const downloading = st.downloading;
  const ready = st.verdict?.ok === true || st.bundled;
  // Resume, not Download, when there is a partial file: "Download" there reads as starting over, and
  // starting over is what resuming exists to avoid.
  const getLabel =
    st.verdict?.problem === "incomplete" ? t("Reanudar descarga") : t("Descargar modelo");

  box.innerHTML =
    '<div class="model-line">' +
    `<span class="model-state ${escapeHtml(st.stateClass ?? "")}"></span>` +
    (ready || downloading ? "" : '<button class="btn small model-get"></button>') +
    (downloading ? '<button class="btn small model-stop"></button>' : "") +
    // Delete is offered ONLY for a model this app downloaded. A bundled copy came with the build and
    // cannot be fetched back into that location, so removing it would leave the user with neither the
    // file nor an explanation.
    (st.verdict?.ok === true && !st.bundled
      ? '<button class="btn small model-del"></button>'
      : "") +
    "</div>" +
    (downloading
      ? '<div class="model-bar"><span></span></div><div class="model-sub"></div>'
      : "");

  const state = box.querySelector<HTMLElement>(".model-state");
  if (state) state.textContent = st.stateText ?? "";

  const get = box.querySelector<HTMLButtonElement>(".model-get");
  if (get) {
    get.textContent = getLabel;
    get.onclick = () => void startDownload();
  }
  const stop = box.querySelector<HTMLButtonElement>(".model-stop");
  if (stop) {
    stop.textContent = t("Cancelar");
    stop.onclick = () => void Model.Cancel().then(applyResult, () => {});
  }
  const del = box.querySelector<HTMLButtonElement>(".model-del");
  if (del) {
    del.textContent = t("Eliminar");
    del.onclick = () => void Model.Remove().then(applyResult, () => {});
  }

  if (downloading && progress) {
    const bar = box.querySelector<HTMLElement>(".model-bar > span");
    if (bar) bar.style.width = `${progress.percent}%`;
    const sub = box.querySelector<HTMLElement>(".model-sub");
    if (sub) sub.textContent = progress.text;
  }
}

let lastStatus: ModelStatus | null = null;

function paint(st: ModelStatus): void {
  lastStatus = st;
  for (const box of $$<HTMLElement>(".model-box")) renderInto(box, st);
  // Reported for the E2E, and it carries no user data: a verdict, a class and whether a download is
  // running. The BYTES are a size, not activity — the file is the same for everyone.
  Events.Emit("ui:model-row", {
    boxes: $$(".model-box").length,
    problem: st.verdict?.problem ?? "",
    ok: st.verdict?.ok === true,
    bundled: st.bundled,
    downloading: st.downloading,
    stateClass: st.stateClass,
    buttons: ["model-get", "model-stop", "model-del"]
      .map((c) => `${c}=${document.querySelector("." + c) ? "shown" : "absent"}`)
      .join(" "),
  });
}

function applyResult(res: { ok: boolean; error: string; status: ModelStatus }): void {
  progress = null;
  paint(res.status);
  if (!res.ok && res.error) Events.Emit("ui:model-error", { error: res.error });
}

async function startDownload(): Promise<void> {
  // Painted as downloading BEFORE the call, so the button changes the instant it is pressed. The
  // call blocks for minutes; waiting for it to answer before showing anything would look dead.
  if (lastStatus) paint({ ...lastStatus, downloading: true });
  try {
    applyResult(await Model.Download());
  } catch (err) {
    progress = null;
    Events.Emit("ui:model-error", { error: String(err) });
    await refreshModelRow();
  }
}

/** Re-reads the status and repaints. Called on load, after a language change, and after any action. */
export async function refreshModelRow(): Promise<void> {
  try {
    paint(await Model.Status());
  } catch (err) {
    Events.Emit("ui:model-error", { error: String(err) });
  }
}

Events.On("model:progress", (e: { data: unknown }) => {
  const arg = (Array.isArray(e.data) ? e.data[0] : e.data) as
    | { percent?: number; text?: string }
    | null;
  if (!arg) return;
  progress = { percent: arg.percent ?? 0, text: arg.text ?? "" };
  // Only the bar and its caption are touched, not the whole row: rebuilding the markup several times
  // a second would fight with the button the user is trying to press.
  for (const box of $$<HTMLElement>(".model-box")) {
    const bar = box.querySelector<HTMLElement>(".model-bar > span");
    if (bar) bar.style.width = `${progress.percent}%`;
    const sub = box.querySelector<HTMLElement>(".model-sub");
    if (sub) sub.textContent = progress.text;
  }
});

// Drives the row's own buttons from outside, for the E2E that performs a REAL 465 MB download.
//
// It clicks the actual button rather than calling the binding: a probe that bypassed the row would
// verify the downloader and prove nothing about the control the user presses. Same rule as the
// connection-card affordance.
Events.On("debug:model-click", (e: { data: unknown }) => {
  const arg = Array.isArray(e.data) ? e.data[0] : e.data;
  const which = String(arg ?? "download");
  const sel = { download: ".model-get", cancel: ".model-stop", remove: ".model-del" }[which];
  const btn = sel ? document.querySelector<HTMLButtonElement>(sel) : null;
  Events.Emit("ui:model-click", { asked: which, found: btn !== null });
  btn?.click();
});
