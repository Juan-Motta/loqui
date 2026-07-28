# Continuity — session handoff

> The first thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it current and SMALL; refresh it with the `checkpoint` skill before closing a session.

- **Focus:** port de **Loqui** (Electron/TS, en `../loqui`) a **Go + Wails v3**, sólo macOS
  arm64. Fases 0-2 completas; fase 3 (proveedores) casi — falta openai y elevenlabs.
- **Next step:** implementar **ElevenLabs** en `internal/stt/elevenlabs/`, portando
  `../loqui/src/shared/elevenLabs.ts`. Sale del mismo molde que Grok (WebSocket, auth por
  header `xi-api-key`, pero JSON con base64 en vez de frames binarios), así que **es el momento
  de extraer el ciclo de vida del socket** de `internal/stt/grok/provider.go` a un paquete
  compartido — con dos implementaciones reales delante, no deducido de una. OpenAI realtime
  **no** encaja en ese molde (mensaje de setup, otro ciclo de vida): paquete aparte.
- **Blockers:**
  1. **Firma ad-hoc de desarrollo.** Ya implicada en tres fallos: el Keychain no responde (de
     ahí las escotillas `LOQUI_*_KEY`), los permisos se revocan en cada rebuild, y probablemente
     el motor de Apple. Requiere una decisión: certificado self-signed fijo vs tu Developer ID.
  2. **Keys de nube.** La de Azure de `loqui` está marcada como expuesta; de xAI no hay ninguna.
     Sin ellas no se verifica transcripción real por Azure ni por Grok (el resto de esas rutas sí
     está probado).
- **Deuda conocida, con dueño:** **dos bugs preexistentes en `internal/session` +
  `internal/app`** que afectan a Azure **hoy**, encontrados por la revisión cruzada de Grok y
  verificados en el código. Van en su propio cambio; están al final de
  `docs/plans/grok-stt-provider.md` con `file:line`:
  1. el presupuesto de reintentos no acota nada si la conexión llega a abrir
     (`controller.go:278` resetea `reconnectAttempt` en cada `Started`) ⇒ bucle de gasto contra
     un servicio que factura por hora;
  2. la reconexión filtra la captura anterior (`controller.go:359` llama a `StartEngine` sin
     `StopEngine`; `dictation.go:115` y `:359` sobreescriben los únicos handles).
- **Active workflow:** ninguno — `.workflow/state.md` quedó completo para Grok. Trabajo directo
  en la rama `port/foundation` (12+ commits sobre `main`).
- **Updated:** 2026-07-28

## Handoff notes

1. **La app dicta.** Verificado con **whisper local** (sin red, sin cuenta, sin clave): el
   dictado quedó en `~/Library/Application Support/LoquiGo/history.jsonl`, con los varios `final`
   de whisper unidos en **un solo mensaje**. Reproducir:
   `./scripts/task.sh package && open --env LOQUI_DEBUG_DICTATE=10 bin/loqui.app`, hablar, y
   mirar el historial. Para que además **pegue** en el cursor falta conceder Accesibilidad.
2. **Leer el README antes de correr cualquier cosa.** Dos trampas de entorno que rompen un clon
   nuevo y ninguna es obvia: `go` a secas no compila (los flags de cgo del Speech SDK salen del
   entorno → `./scripts/go.sh`), y `wails3` no está en el PATH en macOS (→ `./scripts/task.sh`).
3. **`docs/plans/loqui-go-port.md` tiene el diseño, el mapa módulo-por-módulo, las lecciones y
   los riesgos** — leerlo antes de tocar arquitectura.
4. **Cuando algo se queda quieto, volcar goroutines.** `GOTRACEBACK=all` + `kill -QUIT <pid>`
   encontró los tres cuelgues de este port. Ninguno dejó una línea de log.
5. **No portar los proveedores de Electron verbatim — hay que leer el esquema del servicio.**
   El de Grok tenía un bug de pérdida total del texto (toma el final sólo de `transcript.done`,
   que puede venir vacío). Y los docs de xAI mienten sobre el fallo de auth: devuelve 400, no
   401. Las dos cosas sólo se vieron comparando contra
   `docs.x.ai/stt-streaming.ws.json` y contra el servicio real. Ver
   `docs/research/2026-07-28-xai-stt-streaming.md`.

## Estado del código

Once paquetes de tests en verde con `-race` (`./scripts/task.sh test`), `vet` y `gofmt` limpios.

- `main.go`, `wiring.go` — app Wails: 2 ventanas, tray, hotkey `fn`, permisos.
- `internal/session` — el controlador de dictado (decisiones puras, suite portada de Electron).
- `internal/stt` — contrato, **sin dependencias de red**; `stt/azure` (llega al 401 real),
  `stt/helper` (whisper ✅, Apple ⛔ — riesgo 5 del plan), `stt/grok` (✅ 64 tests; el ciclo de
  vida del WebSocket vive aquí hasta que ElevenLabs justifique extraerlo).
- `internal/{audio,inject,store,history,hotkey,permissions,macos,assets}` — captura, paste con
  `NSPasteboard.changeCount`, settings + Keychain, historial, protocolo `fn`, TCC, glue AppKit.
- `frontend/` — `index.html` es Ajustes (el HTML de Electron **verbatim**), `overlay.html` el
  pill. **`src/settings.ts` es un stub**: la UI se ve completa pero no responde a nada. Es la
  fase 4, y son 1828 líneas más el diseño del payload de bootstrap que Go le entrega.
- `cmd/stt-probe` — dictado desde la CLI, `-provider azure|grok`, para aislar fallos sin el app.
