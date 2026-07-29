# Continuity — session handoff

> The first thing to read on a new session (auto-loaded via `CLAUDE.md` / `AGENTS.md`).
> Keep it current and SMALL; refresh it with the `checkpoint` skill before closing a session.

- **Focus:** port de **Loqui** (Electron/TS, en `../loqui`) a **Go + Wails v3**, sólo macOS arm64.
  Fases 0-3 hechas salvo dos proveedores. **Fase 4 (la UI) casi cerrada:** la app se navega, se
  configura y se usa. El usuario la probó y dice que "se ve bien todo"; quedan detalles menores de
  UI que él pulirá.

- **Next step:** **portar la fila del modelo de whisper**, lo único que falta del encargo de
  fidelidad (el trabajo de la sesión anterior ya está mergeado en `main`, hasta `14a3241`). Empezar
     por el test rojo de `modelSpec` en Go: portar `../loqui/src/shared/modelSpec.ts` a
     `internal/store/model.go` (nombre del archivo, tamaño esperado, URL de descarga) con tests, y
     sólo después el servicio de descarga con progreso y el DOM de `renderModelInto` en `#modelRow`.
     Es **load-bearing**: sin `ggml-small.bin` whisper no arranca, y hoy sólo existe bajarlo con
     `./scripts/build-whisper-stt.sh`.

- **Blockers:**
  1. **Sin remoto.** `git remote -v` vacío: no hay copia fuera de esta máquina. El usuario dijo que
     lo configura después. Cuando toque: el module path dice `github.com/Juan-Motta/loqui-go` pero
     `gh` está autenticado como `Juan-Andres-LM`, y crear el repo publicaría el código → hace falta
     elegir owner + público/privado.
  2. **Firma ad-hoc.** Implicada en tres síntomas: el Keychain no responde (de ahí las escotillas
     `LOQUI_*_KEY`), los permisos se revocan en cada rebuild, y probablemente el motor de Apple.
     Arreglarla haría desaparecer también el residuo declarado del Keychain. Decisión pendiente:
     self-signed fijo vs Developer ID.
  3. **Keys de nube.** La de Azure está marcada como expuesta; de xAI no hay ninguna. Sin ellas no
     se verifica transcripción real por esas rutas.

- **Deuda, sin dueño: el frontend no comprueba tipos.** `typescript@^4.9.3` contra un `tsconfig.json`
  con opciones de TS5, así que `tsc` no puede leer la config y vite borra los tipos sin validarlos.
  Ya se escribieron ~1500 líneas de TS sin red. **Subir typescript antes de escribir más.**

- **Deuda conocida, con dueño:** dos bugs preexistentes en `internal/session` que afectan a Azure
  **hoy** — el presupuesto de reintentos no acota nada si la conexión abre (bucle de gasto contra un
  servicio que factura por hora) y la reconexión filtra la captura anterior. Con `file:line` al final
  de `docs/plans/grok-stt-provider.md`. Van en su propio cambio.

- **Active workflow:** ninguno. El último cerrado (los setters de Ajustes) está en
  `.workflow/state.md` — **gitignored**, así que un clon nuevo no lo tiene.
- **Updated:** 2026-07-29

## Handoff notes

1. **La UI funciona y está portada FIEL al maquetado original.** `frontend/index.html` sigue siendo
   el markup de Electron casi verbatim, y la CSS es la suya — por eso **lo que la página emite tiene
   que coincidir con las clases que esa CSS espera**. Un primer intento inventó `.hist-item`/
   `.hist-meta` y produjo filas sin estilo. Portado ya, con sus clases: Historial (`.hrow`, expandir,
   copiar, estados vacíos), Conexiones (`.conn-state` con el estado COMO CLASE, que es lo que colorea
   el punto), idiomas (chips/select según capacidad), Sistema (atajo, apariencia, modo, dispositivo) y
   Permisos (`.prow` con estado de tres vías).

   Los módulos TS están partidos por vista: `settings.ts` (shell + conexiones), `history.ts`,
   `language.ts`, `system.ts`, `permissions.ts`. Las **reglas** viven todas en Go
   (`internal/store/{connection,language,language_catalog,trigger}.go`,
   `internal/app/permission_rows.go`) con tests; la página no decide nada.

2. **Tres cosas que "sólo persistir" NO cubre, y que ya morderon una vez cada una.** Cualquier
   ajuste nuevo tiene que comprobarlas:
   - **El modo** se lee una vez al construir el motor → hay que empujarlo al controlador vivo
     (`LiveHooks.ModeChanged`).
   - **El atajo** vive en un proceso hijo lanzado al arrancar → hay que reiniciar el listener
     (`LiveHooks.TriggerChanged`), o el nuevo queda guardado y el viejo sigue funcionando.
   - **La apariencia** la aplica Wails una sola vez y no expone cómo cambiarla → cgo en
     `internal/macos/appearance_darwin.go`.

   Y los hooks se pasan en el **constructor**, no por métodos: Wails bindea todos los métodos
   exportados de un servicio al webview.

3. **Para probar la UI sin ratón** — un `<select>` dentro de un webview de Wails no se puede clicar
   desde un script, así que hay sondas por variable de entorno, todas gateadas:
   `LOQUI_DEBUG_NAVIGATE=<vista>`, `LOQUI_DEBUG_RECORD_CLICK=1`, `LOQUI_DEBUG_SET_PROVIDER=<motor>`,
   `LOQUI_DEBUG_APPEARANCE=<modo>`, `LOQUI_DEBUG_HISTORY_EVENT=1`, `LOQUI_DEBUG_OVERLAY=1`,
   `LOQUI_DEBUG_DICTATE=<segundos>`. Cada una reporta al log de Go (`UI-NAV`, `CONN`, `LANG`, `SYS`,
   `PERMS`, `HIST-SHAPE`…), **nunca texto de transcripciones**. `./scripts/capture-overlay.sh` captura
   la píldora a resolución nativa.

4. **Un test en verde no prueba que pruebe algo.** En esta sesión **cuatro** tests propios no
   probaban lo que su nombre decía, y los cuatro los encontró **mutar el código de producción**, no
   la suite: uno metía el secreto en un seam que la función nunca llamaba, uno comprobaba sólo "no
   vacío" y bendijo un default incorrecto, uno aceptaba cualquier error donde dos comprobaciones se
   solapaban, y uno afirmaba sobre un código presente en las dos listas que debía distinguir.
   **Verificar cada test nuevo rompiendo a propósito lo que dice cubrir.**

5. **Lo que sigue inerte** (medido, no de memoria): la fila del modelo (`#modelRow`), "Probar
   conexión" de Azure (`#test`), el `#save` de Sistema — que puede ser redundante por diseño, porque
   aquí cada control ya persiste al cambiar —, `#engineHint`, los campos de subservicios sin portar
   (`azureOpenAiResource`, `azureOpenAiDeployment`, `openaiModel`), los enlaces del pie
   (`#openDonate`, `#openTutorial`), las vistas **About** y **reporte**, y los 17 elementos `wiz*`
   del **onboarding**.

6. **Leer el README antes de correr nada.** Dos trampas de entorno: `go` a secas no compila (los
   flags de cgo del Speech SDK salen del entorno → `./scripts/go.sh`) y `wails3` no está en el PATH
   (→ `./scripts/task.sh`). Al añadir campos o métodos a un servicio hay que **regenerar bindings**:
   `./scripts/task.sh common:generate:bindings` (la tarea `generate:bindings` a secas no existe;
   `package` ya la corre).

## Estado del código

Trece paquetes de tests en verde con `-race -count=1` (`./scripts/task.sh test`), `vet` y `gofmt`
limpios. Cinco servicios Wails: `Settings`, `History`, `Clipboard`, `Dictation`, `Permissions`.

- `main.go`, `wiring.go` — app Wails: 2 ventanas, tray, hotkey `fn`, permisos, y los `LiveHooks` que
  conectan los ajustes con el motor y el listener en marcha. El **store se abre en `main`** y se
  comparte con el motor.
- `internal/app` — el payload de Ajustes (`bootstrap.go`), los setters (`settings_write.go`), y los
  servicios de historial, portapapeles, dictado y permisos. Todos los setters devuelven
  `WriteResult{payload, error}` y **no** un error de Go: Wails descarta el resultado de un método que
  también devuelve error, y la página necesita el payload precisamente cuando falla.
- `internal/store` — persistencia **y** las reglas portadas de los módulos puros de Electron:
  conexiones, capacidad de idioma, catálogo, atajo. `UpdateSettings` es transaccional; nunca
  Load-then-Save.
- `internal/session` — el controlador de dictado (decisiones puras, suite portada de Electron).
- `internal/stt` — contrato sin red. `azure` (llega al 401 real), `helper` (whisper ✅ y ahora
  **reporta niveles de micrófono**, Apple ⛔), `grok` (✅ 71 tests).
- `internal/{audio,inject,history,hotkey,permissions,macos,assets,settings}` — captura, paste,
  historial + filtro, protocolo `fn`, TCC, glue AppKit, validación de región.
- `frontend/` — `index.html` markup de Electron casi verbatim (se le añadieron botones de borrar
  clave y una línea de estado); cinco módulos TS por vista; `overlay.html` la píldora.
- `cmd/stt-probe` — dictado desde la CLI, para aislar fallos sin el app.

## Proveedores: qué falta

- ⬜ **elevenlabs** — mismo molde que Grok (WebSocket, header `xi-api-key`, JSON con base64 en vez de
  frames binarios). Momento de **extraer el ciclo de vida del socket** de `internal/stt/grok`, con dos
  implementaciones reales delante y no deducido de una.
- ⬜ **openai realtime** — **no** encaja en ese molde (mensaje de setup, otro ciclo de vida): paquete
  aparte.
