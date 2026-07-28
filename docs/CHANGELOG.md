# Changelog

Notable changes to this project, newest first — one short entry (or small block) per
shipped change. Written at ship time (the `finish-branch` skill records an entry before the
ship commit). See `shared/rules/docs-layout.md`.

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
