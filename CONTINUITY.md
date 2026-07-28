# Continuity — session handoff

> The first thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it current and SMALL; refresh it with the `checkpoint` skill before closing a session.

- **Focus:** port de **Loqui** (Electron/TS, en `../loqui`) a **Go + Wails v3**, sólo macOS
  arm64. Fases 0-3 hechas salvo dos proveedores; **el port está mergeado en `main`**. Ahora
  **fase 4: la UI**, con la primera costura ya puesta.

- **Next step:** seguir portando la vista de Ajustes contra la misma costura. El lazo ya está
  cerrado para **motor + key + región**; lo que falta en esta pantalla son los **selectores de
  idioma por slot** (el payload ya trae `languageBySlot` con los siete slots y sus defaults; falta
  el setter y la **validación por capacidad** — cuántos idiomas acepta cada motor), y después
  **tecla de activación**, **apariencia**, **dispositivo de entrada** y el **modo** hold/toggle.

  Dos cosas que hay que saber antes de tocarlo:
  1. **El modo se lee UNA vez, al construir el motor** (`app.NewDictation` hace
     `session.Mode(st.LoadSettings().Mode)`), así que un `SetMode` que sólo persista **no tendrá
     efecto** hasta reiniciar. Hay que decidir si el controlador relee o si se le notifica.
  2. **Todo setter nuevo va por `store.UpdateSettings`**, nunca Load-then-Save: son dos secciones
     críticas distintas y Wails despacha cada llamada en su propia goroutine. Hay un test de 50
     rondas que lo caza.

  Después de Ajustes: **historial**, **About** y el **onboarding**. Y el que es load-bearing:
  **modelSpec + descarga del modelo de whisper**, porque sin modelo no arranca y hoy no hay UI
  para bajarlo.

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

- **Deuda, sin dueño: el frontend no comprueba tipos.** `typescript@^4.9.3` contra un
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

- **Active workflow:** ninguno. Los dos cambios de la fase 4 se cerraron: el payload en `bd17cd8` y
  los setters en `74d077a`. El registro está en `.workflow/state.md` (**gitignored**, así que un clon
  nuevo no lo tiene).
- **Updated:** 2026-07-28

## Handoff notes

0. **La app YA se configura desde la interfaz.** El lazo completo funciona para motor, key y región:
   `Settings.Load()` da el estado, los setters lo cambian y devuelven el payload repintado.
   Verificado en la app empaquetada. Al añadir campos o métodos: `internal/app/bootstrap.go` y
   `internal/app/settings_write.go`, y **regenerar bindings** con
   `./scripts/task.sh common:generate:bindings` (la tarea `generate:bindings` a secas no existe;
   `package` ya la corre).

   **Reglas que el ciclo de revisión dejó, y que valen para cada setter nuevo:**
   - Devolver `WriteResult`, **no** un error de Go: Wails descarta el resultado de un método que
     también devuelve error, y la página necesita el payload precisamente cuando falla.
   - Escribir por `store.UpdateSettings`, nunca Load-then-Save.
   - Un rechazo no cambia nada, y se valida **todo** antes de escribir **algo**.
   - Estados ilegibles (`unreadable`) y no disponibles (`available: false`) se pintan distintos de
     "vacío": adivinar es el bug que este proyecto ya cometió con los permisos.

0b. **Para probar la escritura sin ratón:** `LOQUI_DEBUG_SET_PROVIDER=grok` pide a la página que
   haga un `SetProvider` real por el binding. Un `<select>` dentro de un webview de Wails no se
   puede clicar desde un script, así que sin esto la mitad de escritura sólo se comprueba a mano.

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
- `internal/app/settings_write.go` — los setters. `WriteResult` en vez de error de Go (ver nota 0),
  validación antes de cualquier escritura, y `SaveConnection` para que región+key no se commiteen a
  medias.
- `logging.go` — redacta los argumentos de los bindings en el log de Wails: uno de ellos recibe una
  API key, y activar su log de debug la imprimiría.
- `internal/session` — el controlador de dictado (decisiones puras, suite portada de Electron).
- `internal/stt` — contrato, **sin dependencias de red**. `stt/azure` (llega al 401 real),
  `stt/helper` (whisper ✅, Apple ⛔ — riesgo 5 del plan), `stt/grok` (✅ **71 tests**; el ciclo de
  vida del WebSocket vive aquí hasta que ElevenLabs justifique extraerlo a un paquete propio).
- `internal/{audio,inject,store,history,hotkey,permissions,macos,assets,settings}` — captura,
  paste con `NSPasteboard.changeCount`, settings + Keychain, historial, protocolo `fn`, TCC, glue
  AppKit, validación de región/candidatos.
- `frontend/` — `index.html` es Ajustes (markup de Electron casi verbatim: se le añadieron los
  botones de borrar clave y una línea de estado para el selector), `overlay.html` el pill.
  **`overlay.ts` funciona**; **`settings.ts` cablea motor, keys y región** — el resto de la página
  sigue inerte. No decide nada: lee el payload, pinta, manda la acción y repinta.
- `cmd/stt-probe` — dictado desde la CLI, `-provider azure|grok`, para aislar fallos sin el app.

## Proveedores: qué falta

- ⬜ **elevenlabs** — mismo molde que Grok (WebSocket, header `xi-api-key`, pero JSON con base64
  en vez de frames binarios). Es el momento de **extraer el ciclo de vida del socket** de
  `internal/stt/grok/provider.go`, con dos implementaciones reales delante y no deducido de una.
- ⬜ **openai realtime** — **no** encaja en ese molde (mensaje de setup, otro ciclo de vida):
  paquete aparte.
