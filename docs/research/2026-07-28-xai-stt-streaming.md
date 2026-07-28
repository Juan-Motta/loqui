# xAI (Grok) Speech-to-Text streaming — verificación de la API

- **Fecha:** 2026-07-28
- **Para:** el proveedor `internal/stt/grok`, fase 3 del port.
- **Motivo:** el código de Electron (`../loqui/src/shared/grokStt.ts`) dice "validated
  against docs.x.ai (2026)". Antes de portarlo hay que confirmar que sigue vigente — y
  resultó que **una de las seis suposiciones estaba mal**, con consecuencia de pérdida de
  texto.

## Fuente autoritativa

`https://docs.x.ai/stt-streaming.ws.json` — el **esquema legible por máquina** del endpoint.
Es más preciso que la página renderizada, que en al menos un punto se contradice consigo
misma (ver "error" abajo). Dos trucos útiles para el futuro:

- añadir `.md` a cualquier ruta de `docs.x.ai` devuelve el markdown fuente;
- los endpoints WebSocket publican su esquema en la raíz (`/stt-streaming.ws.json`,
  `/tts-streaming.ws.json`).

## Las seis suposiciones de Electron

| # | Suposición | Veredicto |
| --- | --- | --- |
| 1 | Endpoint `wss://api.x.ai/v1/stt` | ✅ confirmado |
| 2 | Configuración por query params, sin mensaje de setup | ✅ confirmado, literal: "Configuration is done via URL query parameters — no setup message required." Los cuatro params son **opcionales** (`sample_rate` 16000, `encoding` pcm, `interim_results` false, `language` "") |
| 3 | Auth por header `Authorization: Bearer <key>` en el handshake | ✅ confirmado; `required: true` en el esquema. No hay auth por query param ni por subprotocolo |
| 4 | Frames binarios crudos PCM16 LE, sin base64; 16 kHz mono nativo | ✅ confirmado, literal: "Audio is sent as raw binary frames (no base64 encoding)". 16 kHz es "the model's native rate and avoids resampling on the server" |
| 5 | `{"type":"audio.done"}` para cerrar, `{"type":"Finalize"}` para forzar final | ✅ confirmado. `finalize` acepta ambas capitalizaciones (`enum: ["finalize","Finalize"]`); `audio.done` es `const`, sólo esa cadena exacta |
| 6 | Eventos `transcript.created` / `.partial` / `.done` / `error` | ⚠️ **casi** — ver la corrección, es la importante |

## Las correcciones que importan

### 1. El final NO se distingue por el nombre del evento (el bug que casi se portó)

`transcript.partial` trae `type, text, words, is_final, speech_final, start, duration`.
Los tres estados:

| `is_final` | `speech_final` | significado |
| --- | --- | --- |
| `false` | — | hipótesis interina |
| `true` | `false` | final de trozo (~3 s) |
| `true` | `true` | **final de enunciado** |

`parseGrokEvent` de Electron mapea todo `transcript.partial` a `{kind:"partial"}` y sólo
`transcript.done` a final. En una sesión de varias frases eso **descarta los finales de
enunciado**. No se debe portar ese parseo tal cual.

### 2. La carga de `error` es PLANA

`{"type":"error","message":"..."}`, campos requeridos `["type","message"]`. **No existe** la
forma anidada `error.message` que el código de Electron busca primero — esa es de
`wss://api.x.ai/v1/responses` (Responses API), otro endpoint.

⚠️ **No importar el límite de 25 minutos** ni el sobre `{"type":"error","status":400,"error":{...}}`:
son de la Responses API. Context7 los devuelve pegados a los docs de STT, lo que invita
exactamente a esa confusión.

### 3. La documentación se contradice sobre si `error` es terminal

- La guía dice: "Connection stays open."
- La referencia y el esquema dicen: la mayoría de errores (fallos de pipeline, timeouts de
  stream) **cierran** la conexión; sólo los errores de parseo de un mensaje del cliente la
  dejan viva.

Vale más la referencia. Tratar `error` como terminal.

### 4. Otros detalles

- `transcript.created` trae un `id` requerido (UUID de sesión) — útil para correlacionar logs.
- `transcript.done` trae `type, text, words, duration`, y "Connection closes after this event".
- **No hay `model`**: STT es un modelo único, facturado por hora ($0.10/h REST, $0.20/h
  streaming). La suposición de Electron de no mandar `model` es correcta. (La Voice Agent
  API, `wss://api.x.ai/v1/realtime`, sí toma `model` — no confundirlas.)
- **No hay campo de idioma detectado** en streaming: ni en `.partial` ni en `.done`. En REST
  existe pero está desactivado ("Currently empty — language detection is not yet enabled").
  ⇒ nuestro `Event.Language` se queda vacío para Grok.
- **`language` no es una pista de reconocimiento, es de formato.** Literal: "The model
  transcribes speech in any of these languages regardless of the `language` parameter —
  setting it enables formatting of numbers, currencies, and units into their written form."
  25 códigos; casi todos ISO-639-1 pero `fil` tiene tres letras ⇒ tratar como cadena libre,
  no validar como ISO-639-1 estricto.
- **Tamaño de frame / duración máxima de sesión: no verificable** — nada documentado para
  streaming (el límite de 500 MB es sólo de REST). Único consejo: "Send 100 ms audio chunks
  (3,200 bytes at 16 kHz PCM16)". "Stream timeouts" se menciona como causa de error sin
  umbral.
- **Fallo de autenticación: no verificable del todo.** La tabla de errores lista
  `401 Unauthorized — API key is missing or invalid`, pero está organizada por código HTTP,
  o sea que describe el handshake. **No hay close codes documentados** para `/v1/stt`. Como
  la auth va en un header, una key mala casi seguro rompe el upgrade con 401 antes de
  cualquier frame — pero los docs no lo dicen. Comprobar empíricamente.

## Segunda ronda: ¿qué texto trae cada evento?

La pregunta decisiva para el mapeo, porque el controlador **acumula** cada `Final`
(`internal/session/controller.go:293`): si dos eventos traen texto solapado, se pega dos veces.

**Los docs no lo dicen explícitamente para ningún evento — "no verificable".** Pero los
payloads de ejemplo del propio esquema resuelven la parte práctica.

### `transcript.done.text` — puede venir VACÍO

La descripción del campo es inútil (`"Final transcript text."`), y las fuentes se contradicen:

- A favor de "sólo la cola": la guía dice que `audio.done` le pide al servidor "flush the
  **remaining** transcript"; la referencia dice que primero emite los eventos finales y
  *después* el `transcript.done` (redundante si repitiera todo).
- A favor de "toda la sesión": los dos ejemplos de código oficiales lo imprimen como
  `Full transcript: {event['text']}`, y el campo homónimo en REST sí está documentado como
  "Full transcript text".

**El artefacto decisivo** — el ejemplo oficial de `transcript.done`:

```json
{ "type": "transcript.done", "text": "", "words": [], "duration": 6.43 }
```

`text` vacío tras **6.43 segundos** de audio. Una transcripción de sesión completa no puede
estar vacía; un flush de cola sí.

> **Consecuencia para el port:** el mapeo de Electron (finales **sólo** desde
> `transcript.done`) puede entregar **cadena vacía** y perder el dictado entero. No es una
> mejora opcional: es un bug.

### `transcript.partial.text` — no acumula a lo largo de la sesión

- `start` es **relativo a la sesión**: "seconds from stream start". `duration` es la ventana
  que cubre *este* resultado. Los `words` llevan `start`/`end` también relativos a la sesión.
- Evidencia negativa fuerte contra lo acumulativo: estos mismos docs dicen "cumulative"
  cuando lo quieren decir — el evento de la Voice Agent API
  `conversation.item.input_audio_transcription.updated` está documentado como "the
  **cumulative transcript so far** … this is different from a transcript delta". Los docs de
  STT streaming nunca usan esa palabra. Mismos autores, misma familia de páginas: la omisión
  significa algo.
- **Reinicio por enunciado: no verificable.** No hay **ningún** ejemplo multi-enunciado en los
  docs; todos los `start` valorados son `0.0`.

### El peligro real: solape DENTRO de un enunciado (sin resolver)

- La tabla de estados de la guía dice que `is_final=true, speech_final=true` es el
  "**complete stitched utterance**" — o sea que **re-emite** el texto que ya entregaron los
  finales de trozo de ese enunciado.
- El esquema dice que `text` es "Transcript text for this **chunk**" — o sea incremental.

Si "stitched" es lo correcto y uno acumula todo `is_final=true`, se duplica cada enunciado de
más de ~3 s.

### `transcript.done` es terminal, y no está garantizado que lo preceda un final

- Sólo llega tras `audio.done`, y "the connection closes after this event". Confirmado en las
  tres fuentes.
- **`finalize` NO lo produce**: "The session stays open so you can continue streaming audio" —
  `finalize` produce un `transcript.partial` con `speech_final`, nunca un `done`. ⇒ para
  push-to-talk, `audio.done` solo es suficiente; mandar `Finalize` antes es redundante.
- Un `error` puede cerrar la conexión **sin** ningún `transcript.done` ⇒ no esperar el `done`
  como único terminador; hace falta el timeout.
- Con `interim_results=false` y audio más corto que la ventana de ~3 s, el único evento con
  texto podría **ser** el `transcript.done`. Hay que soportar los dos extremos.

### La secuencia más completa que existe

No hay ninguna captura real multi-evento en los docs. Estos son los únicos payloads con
valores, ensamblados de tres secciones distintas:

```json
{"type":"transcript.created","id":"83f2f6fd-1cd1-4747-bc52-cebddc961c32"}

{"type":"transcript.partial","text":"The balance is $167,983.15.",
 "words":[{"text":"The","start":0.24,"end":0.48,"confidence":0.95},
          {"text":"balance","start":0.48,"end":0.96,"confidence":0.92},
          {"text":"is","start":0.96,"end":1.12,"confidence":0.98},
          {"text":"$167,983.15.","start":1.12,"end":3.2,"confidence":0.89}],
 "is_final":true,"speech_final":false,"start":0.0,"duration":3.2}

{"type":"transcript.partial","text":"I will buy two of those, please.","words":[...],
 "is_final":true,"speech_final":true,"start":0.0,"duration":2.4,
 "end_of_turn_confidence":0.983}

{"type":"transcript.done","text":"","words":[],"duration":6.43}
```

El SDK oficial de Python **no tiene** helper de STT streaming, así que no hay implementación
de referencia de la que copiar la semántica.

### La salida: marca de agua temporal (correcta bajo TODAS las interpretaciones)

No hay que elegir interpretación. `start`/`duration` son relativos a la sesión y los `words`
llevan tiempos absolutos ⇒ eso da una marca de agua monótona:

- `committedUpTo := 0.0`
- Los `is_final=false` **no entran** al buffer (sólo vista previa en vivo).
- En cada `transcript.partial` con `is_final=true` (de trozo **o** de enunciado):
  - `start+duration <= committedUpTo` → solape total, **descartar** (esto es lo que mata el
    doble pegado del enunciado "stitched");
  - `start < committedUpTo` → solape parcial, quedarse sólo con los `words` cuyo
    `start >= committedUpTo`;
  - si no → todo;
  - y luego `committedUpTo = max(committedUpTo, start+duration)`.
- En `transcript.done`: la misma regla por tiempos de palabra. Si fuera de sesión completa, lo
  anterior a la marca se descarta y sólo entra la cola; si fuera sólo la cola, entra normal;
  si viene vacío, no entra nada. **Las tres interpretaciones caen bien.**

Dos restricciones a codificar: `transcript.done` **no** trae `start` de primer nivel (sus
requeridos son `type, text, words, duration`), así que se deduplica por tiempos de palabra; y
si `words` viniera vacío con `text` no vacío, cae a una comparación de cadenas — si el buffer
es prefijo/subcadena de `done.text`, reemplazar el buffer por `done.text`, si no, añadir.
`word.confidence` está "Omitted when 0" ⇒ puntero/opcional, no un float requerido.

### Experimento de cinco minutos que zanja las tres preguntas

Dictar dos frases con una pausa clara, `interim_results=true`, registrar cada evento literal, y
mirar: (a) ¿el `start` del evento `speech_final=true` es igual al del primer final de trozo de
ese enunciado (stitched) o continúa desde su fin (incremental)?; (b) ¿el `start` del segundo
enunciado sigue subiendo (relativo a la sesión, como está documentado) o se reinicia a ~0?;
(c) ¿`done.text` viene vacío, con el último enunciado, o con todo? La lógica de marca de agua
es correcta en cualquier caso, así que esto confirma, no desbloquea.

## Verificado contra el servicio real (2026-07-28)

Lo único que se pudo comprobar sin cuenta de pago: **el handshake con una key inválida**. Y
**los docs están mal**.

La tabla de errores de xAI dice `401 Unauthorized — API key is missing or invalid`. La realidad:

```
$ curl -i -H "Authorization: Bearer xai-...-000000" \
    -H "Connection: Upgrade" -H "Upgrade: websocket" \
    -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
    "https://api.x.ai/v1/stt?encoding=pcm&sample_rate=16000&interim_results=true&language=es"

HTTP/2 400
content-type: application/json

{"code":"Client specified an invalid argument",
 "error":"Incorrect API key provided. You can obtain an API key from https://console.x.ai."}
```

**HTTP 400, no 401.** Consecuencias para el cliente:

1. Mapear sólo por status daría `BadRequest` con el mensaje "xAI rechazó la petición (status
   400)" — que manda al usuario a auditar su configuración cuando lo que pasa es que la key
   está mal. Hay que **leer el cuerpo** para distinguirlo (`internal/stt/grok/errors.go`,
   `handshakeFailure`).
2. Eso **no** reintroduce el bug de clasificar por prosa: la decisión de reintento sale del
   **código**, y tanto `AuthenticationFailure` como `BadRequest` son no-reintentables, así que
   leer el cuerpo cambia **el mensaje**, nunca el comportamiento. Es el invariante a mantener.
3. Sigue **sin verificar** si un `error` post-conexión puede traer un fallo de auth. Con la key
   mala nunca se llega a abrir el socket, así que la suposición "la auth falla en el handshake"
   se confirma; lo que no se puede confirmar es qué más viaja por el evento `error`.

Un resultado colateral: la respuesta trae un campo `code` con forma de constante
(`"Client specified an invalid argument"`) que **no** está documentado en ninguna parte de
`docs.x.ai`. No conviene depender de él.

## Capacidades que Electron no usaba

- `smart_turn` (detección de fin de turno por ML) + `smart_turn_timeout`. Para dictado
  evitaría cortar al usuario a mitad de una cifra. No se usa ahora; queda anotado.
- `keyterm` (repetible, máx. 100 × 50 chars) para sesgar nombres propios.
- `vad_threshold`: el default de streaming es `0.08`, el de REST `0.5`.

## Qué implica para el cliente Go

1. El transporte porta intacto: endpoint, query params, header de auth, frames binarios.
2. Ramificar en `is_final`/`speech_final`, **no** en el nombre del evento.
3. Asumir que `error` mata el socket.
4. El único código estructurado fiable viene del handshake: `coder/websocket` devuelve el
   `*http.Response` en el error de `Dial`, así que de ahí sale el 401/429. Después de
   conectar sólo hay un `message` en prosa ⇒ un cubo `ServiceError` en vez de adivinar.

## Fuentes

- https://docs.x.ai/stt-streaming.ws.json (esquema, autoritativo)
- https://docs.x.ai/developers/model-capabilities/audio/speech-to-text (guía)
- https://docs.x.ai/developers/rest-api-reference/inference/voice (referencia)
- https://docs.x.ai/developers/models (precios)
- https://docs.x.ai/developers/rate-limits
- https://docs.x.ai/developers/advanced-api-usage/websocket-mode — **Responses API, NO STT**
