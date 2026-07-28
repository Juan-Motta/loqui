# Continuity — session handoff

> The first thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it current and SMALL; refresh it with the `checkpoint` skill before closing a session.

- **Focus:** port de **Loqui** (Electron/TS, en `../loqui`) a **Go + Wails v3**, sólo macOS
  arm64. Fases 0-2 completas; fase 3 (proveedores) parcial.
- **Next step:** implementar el proveedor **Grok** en `internal/stt/grok/`, portando
  `../loqui/src/shared/grokStt.ts` (URL + parseo de eventos) y
  `../loqui/src/main/streamingStt.ts` (la sesión WebSocket) sobre el contrato `stt.Provider`,
  con tests. Es el primero de los tres cloud que faltan y el más simple: frames PCM16
  binarios, header `Authorization: Bearer`, y un `{"type":"audio.done"}` para cerrar. Después
  ElevenLabs (mismo molde, JSON con base64) y OpenAI realtime.
- **Blockers:**
  1. **Firma ad-hoc de desarrollo.** Ya está implicada en tres fallos: el Keychain no
     responde (de ahí la escotilla `LOQUI_AZURE_KEY`), los permisos se revocan en cada
     rebuild, y probablemente el motor de Apple. Requiere una decisión del usuario:
     certificado self-signed fijo vs su Developer ID.
  2. **Key de Azure nueva** — la de `loqui` está marcada como expuesta. Sin ella no se puede
     verificar transcripción real por Azure (el resto de esa ruta sí está probado).
- **Active workflow:** none — trabajo directo en la rama `port/foundation`.
- **Updated:** 2026-07-28

## Handoff notes

1. **La app dicta.** Verificado hoy con **whisper local** (sin red, sin cuenta, sin clave): un
   dictado dejó un registro en `~/Library/Application Support/LoquiGo/history.jsonl`, con los
   varios `final` de whisper unidos en **un solo mensaje**. Reproducir:
   `./scripts/task.sh package && open --env LOQUI_DEBUG_DICTATE=10 bin/loqui.app`, hablar, y
   mirar el historial. Para que además **pegue** en el cursor falta conceder Accesibilidad.
2. **Leer el README antes de correr cualquier cosa.** Hay dos trampas de entorno que rompen un
   clon nuevo y ninguna es obvia: `go` a secas no compila (los flags de cgo del Speech SDK
   salen del entorno → `./scripts/go.sh`), y `wails3` no está en el PATH en macOS
   (→ `./scripts/task.sh`).
3. **El diseño, el mapa módulo-por-módulo, las lecciones y los riesgos abiertos están en
   `docs/plans/loqui-go-port.md`** — leerlo antes de tocar arquitectura. Incluye por qué
   desapareció la ventana `engine`, por qué el bundle id NO es el de Electron, y cinco errores
   que este port ya cometió y conviene no repetir: reentrada bajo mutex, filesystem
   case-insensitive, `BackgroundType` que no aplica en macOS, rutas relativas dentro de un
   `.app`, y el watchdog de silencio. El spike que desbloqueó Azure está en
   `docs/research/2026-07-27-azure-speech-go-macos.md`.
4. **Cuando algo se queda quieto, volcar goroutines.** `GOTRACEBACK=all` + `kill -QUIT <pid>`
   encontró los tres cuelgues de este port (deadlock del controlador, Keychain,
   `SecItemCopyMatching`). Ninguno dejó una línea de log.

## Estado del código

Nueve paquetes de tests en verde (`./scripts/task.sh test`), `vet` y `gofmt` limpios.

- `main.go`, `wiring.go` — app Wails: 2 ventanas, tray, hotkey `fn`, permisos.
- `internal/session` — el controlador de dictado (decisiones puras, suite portada de Electron).
- `internal/stt` — contrato; `stt/azure` (llega al 401 real), `stt/helper` (whisper ✅, Apple ⛔
  — ver riesgo 5 del plan).
- `internal/{audio,inject,store,history,hotkey,permissions,macos,assets}` — captura,
  paste con `NSPasteboard.changeCount`, settings + Keychain, historial, protocolo `fn`, TCC,
  glue de AppKit.
- `frontend/` — `index.html` es Ajustes (el HTML de Electron **verbatim**), `overlay.html` el
  pill. **`src/settings.ts` es un stub**: la UI se ve completa pero no responde a nada. Es la
  fase 4, y son 1828 líneas más el diseño del payload de bootstrap que Go le entrega.
- `cmd/stt-probe` — dictado desde la CLI, para aislar fallos sin el app.
