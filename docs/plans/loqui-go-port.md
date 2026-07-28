# Plan — Port de Loqui (Electron/TS) a Go + Wails v3

- **Fecha:** 2026-07-27
- **Origen:** `~/Desktop/personal/projects/loqui` — Electron + TypeScript, ~13.4k LOC,
  517 tests, 3 helpers nativos, DMG firmado y notarizado.
- **Objetivo:** port 1:1 de funcionalidad. Sólo macOS (arm64) en esta fase; Windows
  queda como puerta abierta, no como requisito.
- **Investigación base:** `docs/research/2026-07-27-azure-speech-go-macos.md`

## Decisiones tomadas

| Decisión | Elegido | Por qué |
| --- | --- | --- |
| Framework | **Wails v3 alpha** | v2 no tiene systray ni multi-ventana; Loqui necesita ambos. v3 marca estables las APIs de app/ventanas/menús/eventos/servicios. |
| Frontend | **Reutilizar verbatim** | La UI actual es HTML+CSS+TS vanilla sin framework, y Wails también renderiza en un webview. El HTML se copia tal cual; sólo cambia la capa de IPC. |
| Lógica pura | **Toda a Go** | En Electron main y renderer compartían TS. Aquí no comparten lenguaje, así que las reglas viven en Go una sola vez y la UI sólo pinta. En Loqui ya hubo dos bugs por reglas duplicadas/derivadas (`sessionPolicy` clasificando por prosa traducida, i18n rompiendo una decisión no-UI). |
| Orden | **Azure Speech primero** | Era el único riesgo sin equivalente Go. Resuelto: ver la investigación. |
| Bundle id | **`com.jualopezmo.loquigo`** | NO reutilizar `com.jualopezmo.loqui`: macOS ata los permisos de Accesibilidad e Input Monitoring a bundle id + firma, así que dos binarios firmados distinto con el mismo id se pelean el mismo registro TCC. Tomar el id original será un paso de migración explícito cuando el port reemplace a Electron. |

## Cambio arquitectónico central: se cae la ventana `engine`

Electron tenía **3 renderers**. El `engine` existía por dos razones que en Go
desaparecen: hospedar el SDK JS de Azure Speech, y ser la única ventana con permiso
de micrófono (`getUserMedia`).

En el port **Go captura el audio** (malgo/miniaudio → CoreAudio) y **Go maneja todos
los proveedores**. Consecuencias:

- Quedan **2 ventanas**: `settings` y `overlay`. Ambas con los permisos del webview
  explícitamente denegados: no necesitan ninguno.
- **Un solo camino de audio**: PCM16 16 kHz mono desde Go hacia cualquier proveedor
  (push stream para Azure, WebSocket para Grok/ElevenLabs/OpenAI, stdin para los
  helpers nativos). En Electron había 3 topologías distintas de captura.
- Se elimina la dependencia de `getUserMedia` en WKWebView, que es terreno dudoso.
- Desaparece toda la superficie IPC `engine:*` y su guardia por ventana
  (`ipcGuard`/`ENGINE_CHANNELS`): ya no hay un renderer que maneje secretos.
- El medidor de nivel (`meter.ts`, AnalyserNode) se vuelve RMS en Go.

## Mapa de módulos: Electron → Go

### Lógica pura → paquetes Go con tests (el corazón del port)

| Electron (`src/shared/`) | Go | Nota |
| --- | --- | --- |
| `sessionController.ts`, `dictationState.ts`, `sessionTracker.ts`, `sessionPolicy.ts` | `internal/session/` | El controlador con `desired` vs `actual`, generaciones, backoff. Se porta con su suite completa. |
| `overlayState.ts` | `internal/session/overlay.go` | El reducer se va a Go; el frontend recibe `{status,error}` ya calculado. **Hecho** en el lado del frontend. |
| `settings.ts`, `azureConfig.ts`, `azureOpenAi.ts`, `openaiRealtime.ts`, `grokStt.ts`, `elevenLabs.ts` | `internal/settings/`, `internal/stt/<provider>/` | Validación, normalización de región, endpoint v2, construcción de URLs/payloads. |
| `languageSlots.ts`, `languageCatalog.ts`, `secretSlots.ts` | `internal/settings/` | Idiomas por slot de proveedor + migración del `languages` global legado. |
| `triggerKey.ts` | `internal/hotkey/` | Ojo: los acceleradores dejan de ser de Electron. Ver "Riesgos". |
| `history.ts`, `historyFilter.ts` | `internal/store/` | |
| `logFile.ts` | `internal/store/` | Formato, redacción y retención. |
| `modelSpec.ts` | `internal/model/` | Aritmética de descarga + sha256. |
| `audioPcm.ts` | `internal/audio/` | `downsample`, `floatTo16BitPCM`. |
| `permissions.ts`, `mediaPermission.ts` | `internal/permissions/` | |
| `connectionStatus.ts` | `internal/ui/` | Modelo de disponibilidad por proveedor. |
| `helperExit.ts`, `sttHelperProtocol.ts`, `globeProtocol.ts` | `internal/stt/helper/`, `internal/hotkey/` | Protocolos de línea de los helpers. |
| `i18n/` | `internal/i18n/` | Catálogos es/en. La UI recibe el catálogo resuelto al arrancar. |
| `preRollBuffer.ts`, `pasteQueue.ts` | `internal/audio/`, `internal/inject/` | |

### I/O y glue

| Electron (`src/main/`) | Go | Cambio real |
| --- | --- | --- |
| `main.ts` | `main.go` + `internal/app/` | **Esqueleto hecho.** |
| `configStore.ts` (safeStorage) | `internal/store/keychain_darwin.go` | `safeStorage` no existe. Va al **Keychain de macOS** directo por cgo (Security.framework), un slot por proveedor. Con timeout: sin firma estable la lectura cuelga (ver riesgo 1). |
| `historyStore.ts`, `logStore.ts`, `modelStore.ts`, `deviceState.ts` | `internal/store/` | `app.getPath("userData")` → `~/Library/Application Support/LoquiGo`. **No `Loqui`**: macOS es case-insensitive, así que ese nombre es el MISMO directorio que el `loqui` de Electron. |
| `tokenService.ts`, `azureProbe.ts` | `internal/stt/azure/` | HTTP plano. |
| `injection.ts` | `internal/inject/` | **Mejora sobre el original.** Electron dejó documentado que el restore del portapapeles necesitaba `NSPasteboard.changeCount` y no lo tenía. En Go con cgo **sí se puede**, y el PRD ya lo pedía (R6). Y el pegado puede ser `CGEventPost` en vez de `osascript`. |
| `focusGuard.ts` | `internal/inject/focus.go` | Igual: la lectura AX puede ser AXUIElement por cgo en vez de AppleScript. |
| `hotkey.ts` | `internal/hotkey/` | El helper Swift se conserva; sólo cambia quien lo lanza. |
| `streamingStt.ts` | **por proveedor**, no compartido todavía | `ws` → `coder/websocket`. **Corregido 2026-07-28:** este mapeo decía `internal/stt/stream.go`, o sea dentro del paquete del **contrato**, lo que metería una dependencia de red en el paquete que importan `session`, `app` y los proveedores locales. Y extraerlo ya era prematuro: el `SttAdapter` de Electron **contiene** el bug que Grok tuvo que arreglar (cierra ante cualquier final tras el finalize, lo que trunca). El ciclo de vida vive en `internal/stt/grok/provider.go`; se extraerá a un paquete propio cuando ElevenLabs dé la segunda implementación real. Ver `docs/plans/grok-stt-provider.md`. |
| `windowOptions.ts`, `ipcGuard.ts` | `main.go` | `ipcGuard` desaparece: no hay canales genéricos, Wails expone métodos tipados. |
| `preload/` | — | Desaparece. Los bindings de Wails son la superficie. |

### Se porta sin tocar

Los tres helpers nativos son procesos aparte que hablan un protocolo de líneas, así
que son independientes del lenguaje del host. Copiados ya a `helpers/`:

- `macos-globe-listener.swift` — la única forma de detectar `fn` down **y up**
- `macos-stt.swift` — Apple SpeechAnalyzer (macOS 26+)
- `whisper-stt.cpp` — whisper.cpp local

## Fases

- **Fase 0 — cimientos.** ✅ **HECHO.** Spike de Azure verificado; scaffold Wails v3;
  2 ventanas; tray con icono template/activo; single instance; shim AppKit para el
  overlay no-activante (verificado: `layer=25`, en pantalla, sin robar foco);
  `patch-plists.sh` para las usage descriptions.
- **Fase 1 — Azure Speech real.** ✅ **HECHA**, salvo la transcripción real (necesita key).
  Proveedor en `internal/stt/azure`, `scripts/vendor-speech-sdk.sh` con sha256 fijado,
  captura con malgo verificada, `tokenService` con tests, `cmd/stt-probe`.
- **Fase 2 — sesión y entrega.** ✅ **HECHA** (bloqueada por firma, ver riesgos).
  `internal/session` completo con tests, hotkey `fn`, inyección con `changeCount` real,
  focus guard por AX, historial, settings + Keychain, todo cableado. Falta el atajo global
  no-`fn` (ver riesgo 2) y la primera transcripción real (necesita firma estable + key).
- **Fase 3 — el resto de proveedores.** PARCIAL.
  - ✅ `internal/stt/helper` — un proveedor para los dos motores locales (mismo protocolo de
    líneas JSON), con tests. `internal/permissions` — micrófono + reconocimiento de voz.
  - ✅ **whisper: funciona.** Primer transcript real verificado 2026-07-28.
  - ⛔ **macos (Apple SpeechAnalyzer): bloqueado, causa desconocida.** Ver riesgo 6.
  - ✅ **grok (xAI): implementado** 2026-07-28, `internal/stt/grok`. Falta la transcripción real
    (necesita una key de xAI); todo lo demás está verificado contra un servidor WebSocket local,
    y el rechazo del handshake contra el servicio real. **No se portó el parseo de eventos
    verbatim**: el de Electron pierde el texto (ver `docs/plans/grok-stt-provider.md`).
  - ⬜ Faltan **openai, elevenlabs**. ElevenLabs sale del mismo molde que Grok y es cuando toca
    extraer el ciclo de vida del WebSocket a un paquete compartido; OpenAI realtime **no** encaja
    en ese molde (necesita un mensaje de setup y tiene otro ciclo de vida).
- **Fase 4 — la UI.** Portar `settings.ts` (1828 líneas) contra un payload de
  bootstrap calculado en Go; i18n; onboarding; historial; permisos; About; logs.
- **Fase 5 — empaquetado.** Firma, entitlements, la dylib de Azure en
  `Contents/Frameworks` con `@rpath`, notarización, DMG.

## Lecciones del port (no re-introducir)

- **El mutex que Go necesita crea reentrada que JavaScript no tenía.** El
  `SessionController` de Electron llamaba a `io.startEngine()` desde dentro de un método y
  recibía el fallo del proveedor sincrónicamente de vuelta en `engineEvent()`. Sin lock eso
  funciona; con lock es deadlock. **Regla: decidir bajo el lock, ejecutar efectos fuera.**
- **macOS tiene filesystem case-insensitive.** `Loqui` y `loqui` son el mismo directorio
  (verificado por inode), así que "le puse mayúscula para separarlo" no separa nada.
- **Los cuelgues del port no dejan log.** Los dos bugs anteriores se encontraron con
  `GOTRACEBACK=all` + `kill -QUIT`. Cuando algo se queda quieto, volcar goroutines es el
  primer paso, no el último.
- **`BackgroundType` no hace NADA en macOS.** Aparece cero veces en el código darwin de
  Wails; la transparencia y la translucidez salen de `Mac.Backdrop`. El overlay quedó
  dibujado sobre un rectángulo blanco opaco por esto, y **se veía perfectamente configurado
  desde Go** — sólo los píxeles discrepaban. De ahí `macos.WindowOpacity`, que lee el estado
  real del `NSWindow`: cuando la config y la pantalla pueden discrepar, hay que leer la
  pantalla.
- **Las rutas relativas no sirven en un `.app`.** Un app lanzado desde Finder tiene el cwd en
  `/`, así que `helpers/bin/...` no resuelve a nada. Todo lookup de recurso pasa por
  `app.HelperPath`, que mira primero dentro del bundle. Es el mismo error que los `-I`
  relativos de cgo, en otro disfraz.
- **Un watchdog de silencio se arma al PARAR, no desde la última salida.** Un helper no
  imprime nada mientras escucha, así que medir desde su última línea gasta toda la gracia
  antes de que el usuario suelte la tecla, y mata el flush que la gracia protegía.
- **Los tests con procesos hijos necesitan esperas generosas.** Arrancar `sh` bajo un binario
  de test enlazado con cgo puede tardar cientos de ms; un drain de 200 ms hizo fallar cuatro
  tests contra código que funcionaba.

## Riesgos abiertos

1. **Firma ad-hoc en desarrollo — CONFIRMADO Y PEOR DE LO ESPERADO.** No sólo revoca
   Accesibilidad e Input Monitoring en cada build: **cuelga el Keychain**.
   `SecItemCopyMatching` nunca retorna cuando macOS no reconoce el binario, porque quiere
   pedir autorización y el prompt no se puede presentar. `GetKey` ahora tiene timeout de
   3 s (`ErrKeychainTimeout`) para que falle diagnosticable en vez de congelarse, pero eso
   no lo resuelve: **sin identidad estable la app no puede leer su propia clave, así que no
   puede dictar.** Es el siguiente paso del proyecto, no un detalle de comodidad.
2. **`triggerKey` ya no puede hablar de acceleradores de Electron.** El formato
   (`"CommandOrControl+Shift+D"`) es de Electron y no hay `globalShortcut` en Go. Habrá
   que elegir librería (`golang.design/x/hotkey`) o registrar un `NSEvent` global
   monitor por cgo, y mapear el formato guardado. Los settings ya persistidos deben
   seguir cargando.
3. **Wails v3 es alpha.** La API puede moverse entre alphas. Fijar la versión en
   `go.mod` y subirla deliberadamente.
4. **Sin cross-compilación.** cgo obligatorio (Azure, AppKit, malgo) → el build de
   macOS se hace en macOS. Ya era así por los helpers Swift.
5. **El motor de Apple está bloqueado y no hay causa identificada.** Standalone llega a
   `started`; lanzado desde el `.app` se detiene justo antes, tras elegir `es-CL`. No falla
   —no emite `canceled`, no escribe a stderr— está bloqueado en un `await`. Micrófono y
   reconocimiento de voz concedidos y no cambia. Los `await` candidatos son
   `SpeechTranscriber.installedLocales`, `AssetInventory.assetInstallationRequest` y
   `SpeechAnalyzer.bestAvailableAudioFormat`. **Siguiente paso: instrumentar el Swift con
   prints entre cada await, o reintentar con firma estable. No inventar la causa.**
6. **Dos warts de whisper, ninguno del pipeline.** `language` guarda el ajuste (`"auto"`) en
   vez del idioma detectado, porque el helper no lo reporta aunque whisper.cpp lo sabe — el
   proyecto Electron guarda lo mismo. Y el helper abre el **dispositivo por defecto de SDL**,
   ignorando `inputDeviceId`, así que el selector de micrófono no le aplica.
7. **La key de Azure está expuesta y pendiente de regenerar** (viene de `loqui`).
   La transcripción real no se puede verificar hasta tener una nueva.
