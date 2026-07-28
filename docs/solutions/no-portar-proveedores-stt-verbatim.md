# No portar el parseo de eventos de un proveedor STT verbatim

- **Fecha:** 2026-07-28
- **Encontrado al:** portar el proveedor Grok (xAI) de Electron a Go
- **Aplica a:** los dos proveedores que faltan (**elevenlabs**, **openai**), y a cualquier
  revisión futura de Azure

## Síntoma

Ninguno visible. El código de Electron (`../loqui/src/shared/grokStt.ts`) llevaba un comentario
que decía *"Validated against docs.x.ai (Speech to Text, 2026)"*, tenía tests, y estaba en
producción. Portarlo parecía mecánico.

## Causa raíz

Dos bugs distintos, los dos de **pérdida silenciosa de texto**, y ninguno detectable sin leer el
esquema del servicio:

1. **El final no se distingue por el nombre del evento.** `parseGrokEvent` mapeaba todo
   `transcript.partial` a "interino" y tomaba el final sólo de `transcript.done`. El esquema
   distingue con dos banderas (`is_final`, `speech_final`). Ignorarlas descarta los finales de
   enunciado de una sesión de varias frases.
2. **El ejemplo oficial de `transcript.done` trae `text: ""`** tras 6.43 s de audio. Si el único
   final sale de ahí, el dictado se entrega **vacío**.

Y un tercero, del servicio y no del código: **los docs de xAI mienten sobre el fallo de auth.**
Su tabla dice `401`; el servicio real devuelve **400** con
`{"error":"Incorrect API key provided..."}`.

## Arreglo

- Leer el **esquema legible por máquina**, no la página renderizada: los endpoints WebSocket de
  xAI lo publican en la raíz (`docs.x.ai/stt-streaming.ws.json`). Añadir `.md` a cualquier ruta de
  `docs.x.ai` devuelve el markdown fuente. La página se contradice consigo misma en al menos dos
  puntos donde el esquema es claro.
- El proveedor **ensambla** el transcripto y emite **un** final al salir, en vez de emitirlos
  progresivamente: el controlador de sesión sólo **acumula** (`internal/session/controller.go:293`),
  así que un proveedor que emite en trozos no puede retirar nada, y estos servicios sí se retractan.
- Dos reglas de reemplazo según lo que el protocolo dice de cada evento — un final de trozo es
  incremental, un final de enunciado (`speech_final`) es autoritativo para su tramo declarado.
  Ver `internal/stt/grok/timeline.go`.
- Comprobar el fallo de auth **contra el servicio real** con una key inválida. Cuesta un minuto,
  no cuesta dinero, y fue lo único de la ruta de nube verificable sin cuenta.

## Cómo se verificó

`internal/stt/grok` con 71 tests (`-race`), contra un servidor WebSocket **real** local
(`httptest` + `websocket.Accept`, no un mock de la librería), más el handshake contra `api.x.ai`.
Evidencia en `docs/e2e/reports/2026-07-28-grok-stt.md`; la verificación de la API, con lo que los
docs **no** dicen, en `docs/research/2026-07-28-xai-stt-streaming.md`.

## Lo reutilizable

1. **Un comentario que dice "validado contra los docs" no es evidencia.** Ese comentario era
   correcto *y* el código estaba mal: describía el transporte, no la semántica de los eventos.
2. **Buscar el esquema legible por máquina antes de creerle a la prosa.** Los tres hallazgos
   salieron de ahí o del servicio real, ninguno de la guía.
3. **En un proveedor de dictado, el bug caro es el silencioso**: texto perdido o duplicado, no una
   excepción. Los tests que valen son los de secuencias de eventos con solape, retractación y
   terminadores vacíos — no "¿parsea este JSON?".
4. **Cuatro rondas de revisión cruzada encontraron algo real cada una, todas en el mismo sitio**
   (el ensamblado del transcripto). Cuando un componente concentra los hallazgos, seguir picando
   ahí en vez de declararlo terminado.
5. **Un caso que parece irresoluble puede tener señal en el protocolo.** Se dio por "mejor
   esfuerzo" la retractación de palabras hasta que la revisión señaló que `speech_final` es
   exactamente esa señal, y estaba siendo descartada en el decode.
