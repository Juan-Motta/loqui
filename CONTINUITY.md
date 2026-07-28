# Continuity — session handoff

> The first thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it current and SMALL; refresh it with the `checkpoint` skill before closing a session.

- **Focus:** port de **Loqui** (Electron/TS, en `../loqui`) a **Go + Wails v3**, sólo macOS
  arm64. Fases 0-3 hechas salvo dos proveedores. **Ahora toca la fase 4: la UI.**

- **Next step:** escribir el test rojo del **payload de bootstrap** en `internal/app`, y
  implementarlo. Es la costura de la que cuelga toda la fase 4, y hoy no existe: `main.go` y
  `wiring.go` no exponen **ningún** binding de ajustes (verificado, el grep sale vacío).

  Concretamente: una función que devuelva de una sola vez lo que la página de Ajustes necesita
  para pintarse — motor activo, idiomas por slot, qué slots tienen key
  (`store.KeyPresence`, booleanos, **nunca** la key), estado de los tres permisos
  (`permissions.Microphone/SpeechRecognition/Accessibility`), dispositivos de entrada
  (`audio.ListInputDevices`), apariencia e idioma de la app. Todo eso ya existe en Go por
  separado; lo que falta es el struct, el test y el binding.

  Empezar por ahí y no por el DOM tiene una razón práctica: **hoy no hay forma de configurar la
  app por la interfaz**, así que para probar cualquier proveedor hay que editar `settings.json` a
  mano y pasar la key por env var. La primera vista que valga la pena cablear después del payload
  es **Ajustes → selector de motor + campo de key**, porque cierra ese lazo completo (Go calcula →
  UI pinta → usuario actúa → Go persiste en Keychain con `store.SetKey` → UI repinta).

- **Blockers:**
  1. **El trabajo de Grok está commiteado sólo en local (`c922b51`).** Este repo **no tiene
     remoto** (`git remote -v` vacío), así que no hay push ni PR. No se arregla adivinando: el
     module path dice `github.com/Juan-Motta/loqui-go` pero `gh` está autenticado como
     `Juan-Andres-LM`, y crear el repo publicaría el código. Necesita decisión del usuario:
     owner + público/privado.
  2. **Firma ad-hoc de desarrollo.** Implicada en tres fallos: el Keychain no responde (de ahí
     las escotillas `LOQUI_*_KEY`), los permisos se revocan en cada rebuild, y probablemente el
     motor de Apple. Decisión pendiente: certificado self-signed fijo vs Developer ID.
  3. **Keys de nube.** La de Azure de `loqui` está marcada como expuesta; de xAI no hay ninguna.
     Sin ellas no se verifica transcripción real por Azure ni por Grok (el resto de esas rutas sí
     está probado).

- **Deuda conocida, con dueño:** **dos bugs preexistentes** en `internal/session` +
  `internal/app` que afectan a Azure **hoy**, encontrados por la revisión cruzada de Grok y
  verificados en el código. Van en su propio cambio; están al final de
  `docs/plans/grok-stt-provider.md` con `file:line`:
  1. el presupuesto de reintentos no acota nada si la conexión llega a abrir
     (`controller.go:278` resetea `reconnectAttempt` en cada `Started`) ⇒ bucle de gasto contra un
     servicio que factura por hora;
  2. la reconexión filtra la captura anterior (`controller.go:359` llama a `StartEngine` sin
     `StopEngine`; `dictation.go:115` y `:359` sobreescriben los únicos handles).

- **Active workflow:** ninguno. El de Grok se cerró; su registro está en `.workflow/state.md`
  (**gitignored**, así que un clon nuevo no lo tiene — el relato durable está en
  `docs/plans/grok-stt-provider.md`).
- **Updated:** 2026-07-28

## Handoff notes

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
   (lógica pura, va con tests, no toca DOM): **i18n**, **languageCatalog**, **langSlot /
   langCapability / validateLanguagesFor** (existe `store.LanguagesFor` pero no la validación por
   capacidad), **connectionStatus**, **historyFilter**, **triggerKey**, y **modelSpec + descarga
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

- `main.go`, `wiring.go` — app Wails: 2 ventanas, tray, hotkey `fn`, permisos. **Sin bindings de
  ajustes** — eso es la fase 4.
- `internal/session` — el controlador de dictado (decisiones puras, suite portada de Electron).
- `internal/stt` — contrato, **sin dependencias de red**. `stt/azure` (llega al 401 real),
  `stt/helper` (whisper ✅, Apple ⛔ — riesgo 5 del plan), `stt/grok` (✅ **71 tests**; el ciclo de
  vida del WebSocket vive aquí hasta que ElevenLabs justifique extraerlo a un paquete propio).
- `internal/{audio,inject,store,history,hotkey,permissions,macos,assets,settings}` — captura,
  paste con `NSPasteboard.changeCount`, settings + Keychain, historial, protocolo `fn`, TCC, glue
  AppKit, validación de región/candidatos.
- `frontend/` — `index.html` es Ajustes (markup de Electron **verbatim**), `overlay.html` el pill.
  **`overlay.ts` funciona**; **`settings.ts` es un stub**.
- `cmd/stt-probe` — dictado desde la CLI, `-provider azure|grok`, para aislar fallos sin el app.

## Proveedores: qué falta

- ⬜ **elevenlabs** — mismo molde que Grok (WebSocket, header `xi-api-key`, pero JSON con base64
  en vez de frames binarios). Es el momento de **extraer el ciclo de vida del socket** de
  `internal/stt/grok/provider.go`, con dos implementaciones reales delante y no deducido de una.
- ⬜ **openai realtime** — **no** encaja en ese molde (mensaje de setup, otro ciclo de vida):
  paquete aparte.
