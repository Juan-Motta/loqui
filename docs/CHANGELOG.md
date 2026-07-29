# Changelog

Notable changes to this project, newest first — one short entry (or small block) per
shipped change. Written at ship time (the `finish-branch` skill records an entry before the
ship commit). See `shared/rules/docs-layout.md`.

## Fidelidad al maquetado original, y la UI que no respondía — 2026-07-29

- **La navegación del sidebar no estaba cableada**, y como todos los controles de Ajustes viven dentro
  de esa vista, la app no respondía a nada. Cableadas también las pestañas de Ajustes, el botón de
  grabar y los enlaces del pie.
- **Historial** portado fiel: filas `.hrow` con expandir y copiar, el chevron sólo donde el texto está
  cortado, estados vacíos con sus dos variantes, y actividad reciente con tiempo relativo. La CSS
  heredada espera esas clases; un primer intento inventó otras y las filas salían sin estilo.
- **Conexiones** con el modelo real portado (`connectionStatus.ts`): cinco estados, Azure como dos
  servicios con dos keys y dos campos requeridos, y `unsupported` por plataforma/OS/helper.
- **Selectores de idioma** por capacidad: chips con una-locale-por-idioma-base para Azure, locales
  completos para Apple, base + "Detección automática" para los de hint opcional.
- **Pestaña Sistema**: atajo con captura de teclas, apariencia (que necesitó cgo porque Wails sólo la
  aplica al construir la ventana), modo, dispositivo e idioma de interfaz.
- **Pestaña Permisos** con estado de tres vías: lo que macOS no deja consultar es "sin verificar", no
  "falta".
- **Los medidores de audio no medían nada con los motores locales** — los helpers abren el micrófono
  ellos mismos, así que Go nunca veía niveles. Ahora whisper los reporta. Y el pulso de reposo, que
  parecía habla continua, se sustituyó por una línea plana: tres estados distinguibles y ninguno que
  afirme audio inexistente.
- **La píldora**: sombra recortada contra el borde de la ventana, luego un halo demasiado fuerte sobre
  fondo claro, y el borde de medio píxel fuera.


## La app se configura desde la interfaz (fase 4) — 2026-07-28

- Setters en el servicio de Ajustes (`SetProvider`, `SetRegion`, `SetKey`, `DeleteKey`,
  `SaveConnection`) y el DOM del selector de motor, los campos de key y el dropdown de regiones.
  Cierra el lazo: hasta ahora había que editar `settings.json` a mano para probar un proveedor.
- Los setters devuelven `WriteResult{payload, error}` y **no** un error de Go: Wails descarta el
  resultado de un método que además devuelve error, y la página necesita el payload justo cuando la
  escritura falla.
- **Keychain:** la escritura y el borrado ya están acotados (colgaban la ventana), el reemplazo usa
  `SecItemUpdate` en vez de delete-then-add (perdía la key vieja si el add fallaba) y las
  operaciones se serializan por slot.
- **Guardar ya no borra ajustes:** `Settings` es un subconjunto declarado del modelo y Ajustes
  reescribe el archivo entero, así que la escritura fusiona sobre el JSON crudo. Un `settings.json`
  con `null` hacía panic en todas las escrituras.
- **Azure:** elegir el subservicio OpenAI y guardar sobrescribía la credencial de Speech, y un
  guardado de sólo región podía mover el endpoint en vivo. Ambas cerradas en backend y UI.
- Los motores no portados ya no son seleccionables, con un test de contrato que falla si la lista de
  disponibles y el switch de `buildProvider` divergen.
- `logging.go` redacta los argumentos de los bindings: Wails los loguea y uno recibe una API key.


## Payload de bootstrap de Ajustes (fase 4) — 2026-07-28

- `Settings.Load()`, un servicio Wails que devuelve en **una** llamada todo lo que la página de
  Ajustes necesita para pintarse. Hasta ahora la app **no se podía configurar por la interfaz**:
  había que editar `settings.json` a mano y pasar las keys por variable de entorno.
- La presencia de keys pasa a **tres estados** (`store.KeyStatus`: present / absent /
  unreadable). `HasKey` colapsaba `ErrKeychainTimeout` en `false`, así que en un build ad-hoc un
  slot que sí tenía key se reportaba vacío — y eso manda al usuario a reescribir una credencial
  que ya estaba ahí.
- Los slots resueltos por `LOQUI_*_KEY` no se consultan y los demás se leen en paralelo: en serie
  eran 15s (5 × 3s de timeout) con la página en blanco.
- Idiomas normalizados por slot (`store.AllLanguageSlots`, `store.LanguagesIn`). Cuatro slots de
  nube caían al último recurso `en-US`, que fija un motor de nube a inglés en vez de dejarlo
  autodetectar.
- `.task/` deja de estar trackeado: es caché de Taskfile.


## Unreleased

### Proveedor Grok (xAI) STT — fase 3 del port

- **`internal/stt/grok`**: proveedor de dictado por streaming contra `wss://api.x.ai/v1/stt`,
  sobre el contrato `stt.Provider`. Frames PCM16 binarios, auth por header, `audio.done` para
  cerrar. Cableado en `buildProvider` y disponible en `cmd/stt-probe -provider grok`.
- **No se portó el parseo de eventos de Electron verbatim, porque pierde el texto.**
  `grokStt.ts` toma el final sólo de `transcript.done`, e ignora las banderas `is_final` /
  `speech_final`. El ejemplo oficial de `transcript.done` trae `text: ""` tras 6.43 s de audio
  ⇒ ese mapeo puede entregar un dictado **vacío**. El proveedor ahora ensambla una línea de
  tiempo de palabras y emite **un** final al terminar, lo que además absorbe los re-envíos
  "stitched" y las correcciones del servidor sin duplicar. Dos reglas de reemplazo, según lo que
  el protocolo dice de cada evento: un final de trozo es **incremental** (por palabra), mientras
  que un final de enunciado (`speech_final`) y `transcript.done` son **autoritativos** para su
  tramo — que es lo que permite que una retractación borre de verdad.
- **Los docs de xAI están mal sobre el fallo de auth**: una key inválida devuelve **400**, no el
  401 documentado. Se lee el cuerpo del rechazo para reportarlo como problema de key en vez de
  "petición inválida". Verificado contra el servicio real.
- **Escotilla de Keychain por proveedor**: `keyReaderFor` era Azure-only, así que una key de otro
  proveedor se ignoraba en silencio. Ahora hay una variable por slot (`LOQUI_GROK_KEY`, …) y una
  no satisface la lectura de otra.
- `store.DefaultSettings()` gana el slot de idioma `grok` (`auto`), y `store.NewAt` para que los
  tests de otros paquetes no escriban en `~/Library/Application Support`.
- Diseño, alternativas y las cuatro rondas de revisión: `docs/plans/grok-stt-provider.md`.
  Verificación de la API: `docs/research/2026-07-28-xai-stt-streaming.md`. La lección
  transferible a los dos proveedores que faltan:
  `docs/solutions/no-portar-proveedores-stt-verbatim.md`.
- **Pendiente**: transcripción real (necesita una key de xAI). Y la revisión destapó **dos bugs
  preexistentes** en `internal/session` + `internal/app` que afectan a Azure hoy — presupuesto de
  reintentos que no acota si la conexión llega a abrir, y fuga de la captura en la reconexión.
  Documentados al final del plan; van en su propio cambio.
