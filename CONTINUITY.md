# Continuity — session handoff

> The first thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it current; refresh it with the `checkpoint` skill before closing a session.

- **Focus:** Port de **Loqui** (Electron/TS, `../loqui`) a **Go + Wails v3**, sólo macOS arm64.
- **Next step:** Fase 2 — portar `internal/session/` (sessionController + dictationState +
  sessionTracker + sessionPolicy) con su suite de tests, y cablearlo a
  `internal/stt/azure` + `internal/audio` en `main.go`.
- **Blockers:** para verificar transcripción real hace falta una **key de Azure Speech
  nueva** (la de `loqui` está marcada como expuesta). El código compila y conecta sin ella.
- **Active workflow:** none (trabajo directo en la rama `port/foundation`).
- **Updated:** 2026-07-27

## Handoff notes

**Fase 0 cerrada y verificada ejecutando.** Lo que está probado, no inferido:

1. **Azure Speech funciona en Go/macOS arm64.** El xcframework de
   `aka.ms/csspeech/macosbinary` **sí trae los 117 headers `speechapi_c_*.h`** (las
   issues de GitHub que dicen lo contrario describen un problema de documentación).
   El spike compila con cgo, carga la dylib, acepta push stream 16 kHz + LID continuo,
   abre sesión contra el endpoint v2 y devuelve `errorCode=AuthenticationFailure` — el
   mismo código estructurado del que depende la política de reconexión. Detalle en
   `docs/research/2026-07-27-azure-speech-go-macos.md`.
2. **El overlay no-activante funciona.** Wails no expone `showInactive()`, así que hay
   un shim AppKit (`internal/macos/window_darwin.go`) que llama
   `orderFrontRegardless`. Verificado con CoreGraphics: la ventana está en pantalla,
   `layer=25` (status), 215×59, y el frontmost app siguió siendo Finder. **Esto no es
   cosmético:** si Loqui roba el foco, el paste va a la ventana equivocada y el
   `focusGuard` lo descarta como "cambió de app".
3. **La ventana `engine` de Electron no existe en el port.** Go captura el audio y
   maneja todos los proveedores, así que sólo quedan `settings` y `overlay`. Esto borra
   toda la superficie IPC `engine:*` y la dependencia de `getUserMedia` en WKWebView.

**Trampa ya resuelta, no volver a pisarla:** `wails3 task common:update:build-assets`
regenera los `Info.plist` desde `build/config.yml` y borra las usage descriptions.
Sin `NSMicrophoneUsageDescription` macOS **mata el proceso** en vez de mostrar un
prompt. Por eso `scripts/patch-plists.sh` las reinyecta y el Taskfile de darwin lo
llama antes de armar el bundle. No editar los plist a mano.

**Riesgo que va a doler en fase 2:** `wails3 task run` firma ad-hoc y la firma cambia
en cada build, así que macOS revoca Accesibilidad e Input Monitoring en cada
iteración. Con Electron no pasaba (el binario de dev era el de Electron, firma
estable). Decidir una identidad de firma estable para dev antes de tocar hotkey/paste.

## Fase 1 — hecha y verificada

El proveedor Azure y la captura de audio existen y funcionan:

- `scripts/vendor-speech-sdk.sh` descarga el xcframework 1.51.1 con sha256 fijado,
  verifica que traiga los headers C y lo deja en `third_party/speech-sdk/`. Idempotente.
  **Ojo: el directorio NO puede llamarse `vendor/`** — Go entra en modo vendoring y
  rompe el build entero con "inconsistent vendoring".
- Los flags de cgo viven en `build/darwin/Taskfile.yml` (y en las tareas `test`/`vet`),
  con rutas ABSOLUTAS: cgo corre el compilador de C con el directorio del *paquete* como
  cwd, así que un `-I` relativo resolvería dentro del module cache.
- **Dos rpaths a propósito:** `@executable_path/../Frameworks` para el bundle y la ruta
  de desarrollo para `bin/loqui`. Verificado con `DYLD_PRINT_LIBRARIES`: el `.app`
  empaquetado carga la dylib **desde dentro del bundle**.
- `internal/audio/capture.go` — malgo/CoreAudio a 16 kHz/16-bit/mono. **Miniaudio hace el
  resampleo**, igual que Chromium en Electron; no se usa el `Downsample` ingenuo de
  `pcm.go` (que en Electron casi nunca se ejecutaba, justamente porque Chromium honraba
  la tasa pedida — apoyarse en él aquí sería confiar en código que producción nunca corrió).
  Verificado: 125 KB en 4.0 s = 16000x2x4 exacto.
- `cmd/stt-probe` — sucesor del spike, ya en el repo. `-list` enumera micrófonos,
  `-mic-only` mide nivel sin tocar la red (separa "micrófono mudo" de "key rechazada").
- Tests: `internal/audio`, `internal/settings`, `internal/stt/azure` verdes.

**Lo único que sigue sin verificar es la transcripción real**, que necesita la key:

```bash
SPEECH_KEY=... SPEECH_REGION=eastus go run ./cmd/stt-probe -seconds 20
```

## Estado del código

- `main.go` — 2 ventanas + tray + single instance. Corre. El item "Dictar (prueba)" del
  tray hoy sólo togglea el overlay (es lo que ejercita el shim).
- `frontend/` — `settings.html` y `overlay.html` son el HTML de Electron **verbatim**.
  `src/overlay.ts` está portado y funcional. **`src/settings.ts` es un stub**: las 1828
  líneas del original se portan en fase 4, contra un payload de bootstrap de Go.
- `helpers/` — los 3 helpers nativos copiados sin cambios (Swift/C++). Aún sin compilar
  ni lanzar desde Go.
- `internal/` — `assets`, `macos`, `audio` (captura + PCM/nivel), `settings`
  (azureConfig portado), `stt` (contrato de proveedor), `stt/azure` (recognizer + tokens).
  **Todavía nada de esto está cableado a `main.go`**: eso es la fase 2.
- Plan completo con el mapa módulo por módulo: `docs/plans/loqui-go-port.md`.

## Comandos

```bash
wails3 task test       # tests (inyecta los flags de cgo del Speech SDK)
wails3 task vet
wails3 task build      # compila (frontend + go)
wails3 task package    # arma bin/loqui.app y firma ad-hoc
wails3 task dev        # hot reload
LOQUI_DEBUG_OVERLAY=1 ./bin/loqui.app/Contents/MacOS/loqui   # muestra el pill a los 2s
```
