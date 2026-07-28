# Continuity — session handoff

> The first thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it current and SMALL; refresh it with the `checkpoint` skill before closing a session.

- **Focus:** port de **Loqui** (Electron/TS, en `../loqui`) a **Go + Wails v3**, sólo macOS
  arm64. Fases 0-3 hechas salvo dos proveedores; **el port está mergeado en `main`**. Ahora
  **fase 4: la UI**, con la primera costura ya puesta.

- **Next step:** cablear la primera vista real contra el payload: **Ajustes → selector de motor
  + campo de key**. El payload ya existe y llega al webview (`Settings.Load()`), así que falta el
  otro sentido: los **setters** en `SettingsService` — `SetProvider`, `SetKey` (a Keychain con
  `store.SetKey`), `DeleteKey`, `SetRegion` — y el DOM que los llama y repinta. Eso cierra el lazo
  completo (Go calcula → UI pinta → usuario actúa → Go persiste → UI repinta) y es lo que hace que
  la app se pueda **configurar sin editar `settings.json` a mano**, que es el bloqueo práctico
  para probar cualquier proveedor.

  Ojo con dos cosas al escribir los setters:
  1. **`SetKey` escribe en el Keychain, que en un build ad-hoc no responde.** La lectura ya está
     acotada a 3s; la escritura **no** tiene ese timeout (`store.SetKey` llama al cgo directo).
     Un guardado que cuelgue la UI es peor que uno que falle.
  2. **El estado `unreadable` ya existe en el payload** — la UI tiene que pintarlo distinto de
     "sin key", no colapsarlo. Ese es justo el bug que se arregló al crear el payload.

- **Blockers:**
  1. **Todo está commiteado sólo en local.** Este repo **no tiene remoto** (`git remote -v`
     vacío), así que no hay push ni PR. El usuario dijo explícitamente que **lo configura
     después**, así que no es una decisión pendiente que bloquee el trabajo — pero sigue siendo
     cierto que no hay copia fuera de esta máquina. Cuando toque: el module path dice
     `github.com/Juan-Motta/loqui-go` y `gh` está autenticado como `Juan-Andres-LM`, y crear el
     repo publicaría el código, así que hay que elegir owner + público/privado.
  2. **Firma ad-hoc de desarrollo.** Implicada en tres fallos: el Keychain no responde (de ahí
     las escotillas `LOQUI_*_KEY`), los permisos se revocan en cada rebuild, y probablemente el
     motor de Apple. Decisión pendiente: certificado self-signed fijo vs Developer ID.
  3. **Keys de nube.** La de Azure de `loqui` está marcada como expuesta; de xAI no hay ninguna.
     Sin ellas no se verifica transcripción real por Azure ni por Grok (el resto de esas rutas sí
     está probado).

- **Deuda nueva, sin dueño: el frontend no comprueba tipos.** `typescript@^4.9.3` contra un
  `tsconfig.json` con opciones de TS5 (`verbatimModuleSyntax`, `moduleResolution: bundler`): `tsc`
  no puede leer la config y vite/esbuild borra los tipos sin validarlos. Encontrado por la
  revisión de codex y verificado. La fase 4 va a escribir mucho TS contra DTOs generados
  (nullable), así que **conviene subir typescript antes** de portar `settings.ts` en serio.

- **Deuda conocida, con dueño:** **dos bugs preexistentes** en `internal/session` +
  `internal/app` que afectan a Azure **hoy**, encontrados por la revisión cruzada de Grok y
  verificados en el código. Van en su propio cambio; están al final de
  `docs/plans/grok-stt-provider.md` con `file:line`:
  1. el presupuesto de reintentos no acota nada si la conexión llega a abrir
     (`controller.go:278` resetea `reconnectAttempt` en cada `Started`) ⇒ bucle de gasto contra un
     servicio que factura por hora;
  2. la reconexión filtra la captura anterior (`controller.go:359` llama a `StartEngine` sin
     `StopEngine`; `dictation.go:115` y `:359` sobreescriben los únicos handles).

- **Active workflow:** ninguno. El del payload de bootstrap se cerró en `bd17cd8`; su registro
  está en `.workflow/state.md` (**gitignored**, así que un clon nuevo no lo tiene).
- **Updated:** 2026-07-28

## Handoff notes

0. **Ya hay una costura de configuración, y funciona.** `Settings.Load()` es un servicio Wails
   que devuelve el estado completo de Ajustes en una llamada; verificado en la app empaquetada,
   no sólo en tests. Lo que **sigue faltando** es el otro sentido (setters) y el DOM. Al añadir
   campos al payload: `internal/app/bootstrap.go`, y **regenerar bindings** con
   `./scripts/task.sh common:generate:bindings` (la tarea `generate:bindings` a secas no existe;
   `package` ya la corre).

1. **La app dicta pero no se configura.** El dictado está verificado de punta a punta con
   **whisper local** (hotkey `fn` → captura → transcripción → `history.jsonl`, los varios `final`
   unidos en **un** mensaje). Pero `frontend/index.html` son **1249 líneas del markup de Electron
   verbatim** y `frontend/src/settings.ts` es un **stub de 35 líneas** (el original: **1828**): la
   ventana se ve completa y **no responde a nada**. Reproducir el dictado:
   `./scripts/task.sh package && open --env LOQUI_DEBUG_DICTATE=10 bin/loqui.app`. Para que además
   **pegue** en el cursor falta conceder Accesibilidad.

2. **La fase 4 no es traducir 1828 líneas, es rediseñar de dónde salen los datos.** El
   `settings.ts` de Electron importaba **diez módulos puros compartidos** porque allí main y
   renderer compartían lenguaje. Aquí esas reglas viven en Go, como única fuente de verdad. Lo que
   ya está en Go: settings, historial, Keychain, permisos, dispositivos. Lo que **falta portar**
   (lógica pura, va con tests, no toca DOM): **i18n**, **languageCatalog**,
   **langCapability / validateLanguagesFor** (los slots ya están: `store.AllLanguageSlots` +
   `store.LanguagesIn`, con default por slot; falta la **validación por capacidad**),
   **connectionStatus**, **historyFilter**, **triggerKey**, y **modelSpec + descarga
   del modelo de whisper** — este último es load-bearing, porque sin modelo whisper no arranca y
   hoy no hay UI para bajarlo. La superficie que consumía la UI de Electron son **32 métodos**
   (`grep -o "window\.loqui\.[a-zA-Z]*" ../loqui/src/settings/settings.ts | sort -u`), y las
   vistas son cinco: `inicio`, `ajustes`, `historial`, `about`, `report`.

3. **Leer el README antes de correr cualquier cosa.** Dos trampas de entorno que rompen un clon
   nuevo y ninguna es obvia: `go` a secas no compila (los flags de cgo del Speech SDK salen del
   entorno → `./scripts/go.sh`), y `wails3` no está en el PATH en macOS (→ `./scripts/task.sh`).

4. **No portar los proveedores de Electron verbatim — leer el esquema del servicio.** El de Grok
   tenía un bug de pérdida total del texto y los docs de xAI mienten sobre el fallo de auth
   (devuelve 400, no 401). La lección completa, transferible a los dos proveedores que faltan,
   está en `docs/solutions/no-portar-proveedores-stt-verbatim.md`.

5. **Cuando algo se queda quieto, volcar goroutines.** `GOTRACEBACK=all` + `kill -QUIT <pid>`
   encontró los tres cuelgues de este port. Ninguno dejó una línea de log.

## Estado del código

Once paquetes de tests en verde con `-race` (`./scripts/task.sh test`), `vet` y `gofmt` limpios.

- `main.go`, `wiring.go` — app Wails: 2 ventanas, tray, hotkey `fn`, permisos. El **store se abre
  en `main`** y se comparte con el motor (el servicio se registra como opción de construcción, y
  dos `Store` sobre el mismo directorio intercalarían escrituras).
- `internal/app/bootstrap.go` — el payload de Ajustes + `SettingsService` (`Settings.Load()`).
  Seams para Keychain / TCC / hardware, porque los tres son intesteables en su sitio.
- `internal/session` — el controlador de dictado (decisiones puras, suite portada de Electron).
- `internal/stt` — contrato, **sin dependencias de red**. `stt/azure` (llega al 401 real),
  `stt/helper` (whisper ✅, Apple ⛔ — riesgo 5 del plan), `stt/grok` (✅ **71 tests**; el ciclo de
  vida del WebSocket vive aquí hasta que ElevenLabs justifique extraerlo a un paquete propio).
- `internal/{audio,inject,store,history,hotkey,permissions,macos,assets,settings}` — captura,
  paste con `NSPasteboard.changeCount`, settings + Keychain, historial, protocolo `fn`, TCC, glue
  AppKit, validación de región/candidatos.
- `frontend/` — `index.html` es Ajustes (markup de Electron **verbatim**), `overlay.html` el pill.
  **`overlay.ts` funciona**; **`settings.ts` sigue siendo un stub**, pero ya pide el payload y
  reporta lo que recibe por el log de Go (`BOOTSTRAP`).
- `cmd/stt-probe` — dictado desde la CLI, `-provider azure|grok`, para aislar fallos sin el app.

## Proveedores: qué falta

- ⬜ **elevenlabs** — mismo molde que Grok (WebSocket, header `xi-api-key`, pero JSON con base64
  en vez de frames binarios). Es el momento de **extraer el ciclo de vida del socket** de
  `internal/stt/grok/provider.go`, con dos implementaciones reales delante y no deducido de una.
- ⬜ **openai realtime** — **no** encaja en ese molde (mensaje de setup, otro ciclo de vida):
  paquete aparte.
