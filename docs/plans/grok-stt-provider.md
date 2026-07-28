# Plan — proveedor Grok (xAI) STT

- **Fecha:** 2026-07-28
- **Fase:** 3 del port (`docs/plans/loqui-go-port.md`)
- **Rama:** `port/foundation`
- **Investigación previa:** `docs/research/2026-07-28-xai-stt-streaming.md` (obligatoria —
  invalidó una de las seis suposiciones del código de Electron)
- **Revisión de diseño:** codex `gpt-5.6-sol` xhigh × 2 iteraciones, las dos REWORK. Ésta es
  la v3. El registro de qué cambió en cada ronda está al final.
- **Decisión de alcance del usuario (2026-07-28):** Grok enfocado; los dos bugs preexistentes
  del controlador se documentan y van en su propio cambio.

## Objetivo

Que `provider: "grok"` dicte de verdad: un proveedor que cumple `stt.Provider`
(`internal/stt/stt.go:65`), alimentado por la captura única de audio del host, hablando el
WebSocket de xAI con auth por header.

## No-objetivos

- ElevenLabs y OpenAI realtime.
- UI de Ajustes (fase 4). Verificación por `cmd/stt-probe` y `settings.json` a mano.
- `smart_turn`, `keyterm`, `vad_threshold`.
- **Los dos bugs preexistentes del controlador** (ver el final). Decisión del usuario. Este
  plan se limita a **no abrir ninguna ruta nueva** hacia ellos.

## El hallazgo que cambia el port

`parseGrokEvent` (`../loqui/src/shared/grokStt.ts:38`) mapea **todo** `transcript.partial` a
interino y **sólo** `transcript.done` a final. Tres cosas están mal:

1. El esquema distingue el final con `is_final`/`speech_final`, no con el nombre del evento.
2. **El ejemplo oficial de `transcript.done` trae `text: ""` tras 6.43 s de audio** ⇒ si el
   único final sale de ahí, el dictado se entrega **vacío**.
3. La guía llama al evento `speech_final=true` el "complete **stitched** utterance" ⇒ re-emite
   texto ya enviado. Acumularlo todo duplicaría cada frase de más de ~3 s.

El servidor además puede **corregir** texto ya enviado. Y los docs no fijan el alcance de
ningún texto ("no verificable").

## La línea de tiempo (el corazón del cambio)

El controlador sólo **acumula** (`controller.go:293`, `history.go:40`), así que el proveedor
tiene que entregar el texto ya resuelto: **un solo `Final`** al terminar, ensamblado de una
línea de tiempo que el proveedor mantiene.

**Unidad de la línea de tiempo: la palabra, no el segmento.** La v2 guardaba segmentos de
texto y reemplazaba todo segmento que solapara el intervalo del evento nuevo. La revisión lo
tumbó con contraejemplos correctos: un segmento con palabras en `[0,1]` y `[4,5]` tiene envoltura
`[0,5]`, así que un evento nuevo en `[2,3]` lo borraba entero y perdía dos palabras legítimas.
Lo mismo un `done` que solapa parcialmente dos segmentos.

Diseño v3:

- estado: `words []word`, `word = {start, end float64; text string}`, ordenado por
  `(start, end)`;
- **intervalos semiabiertos `[start, end)`**. La API usa palabras adyacentes donde una acaba
  exactamente cuando empieza la siguiente (`"The" 0.24→0.48`, `"balance" 0.48→0.96`), así que
  con intervalos cerrados cada palabra borraría a su vecina;
- `is_final=false` → `Partial` con el texto crudo. **No** toca la línea de tiempo;
- `is_final=true` (de trozo **o** de enunciado) → de las palabras del evento se toma el
  intervalo `[min(start), max(end))` y se **eliminan sólo las palabras existentes que solapan
  ese intervalo** (`w.start < evEnd && w.end > evStart`), luego se insertan las nuevas. Sólo
  desaparece lo que el servidor está reemplazando de verdad;
- desempate con marcas iguales: orden estable por `(start, end, orden de llegada)`, para que
  dos palabras con el mismo `start` no bailen entre ejecuciones;
- un evento `is_final=true` **sin** `words` pero con `text` → se usa `[start, start+duration)`
  como intervalo y el texto entero como una "palabra" de ese tramo. Es la única opción sin
  datos por palabra, y mantiene la regla de reemplazo;
- al terminar, por **cualquier** ruta: unir los textos en orden temporal → un `Final`.

### `transcript.done` sin `words` (limitación explícita)

`done` no trae `start` de primer nivel. Si además llega con `words: []`, no hay **ninguna**
evidencia posicional. La revisión demostró que ninguna regla de comparación de cadenas es
correcta: con lo ensamblado `"I agree"`, un `done` de cola `"I agree again"` y un `done`
corregido `"I disagree"` son indistinguibles, y prefijo acierta en uno y falla en el otro.

Regla v3, deliberadamente conservadora:

- si la línea de tiempo **está vacía** → usar `done.text` (es el caso corto y real: audio más
  breve que la ventana de ~3 s, donde `done` es el único evento con texto);
- si la línea de tiempo **tiene algo** → **ignorar** `done.text` y quedarse con lo ensamblado,
  **registrándolo** en el log.

Justificación: la línea de tiempo está construida con evidencia por palabra; un `done` sin
`words` no aporta ninguna. Esto **nunca duplica** — el fallo posible es perderse una corrección
final, que es un fallo menor y visible, no la duplicación silenciosa. Es **mejor esfuerzo
declarado**, no una garantía; el experimento de la investigación lo resuelve con datos.

### Orden de salida: `Final → [Canceled] → Stopped`

La v2 decía `[Canceled] → Final → Stopped`. **Está mal, y la revisión lo trazó exactamente.**
En la ruta reintentable, `handleCancelLocked` hace `tracker.Bump()` (`controller.go:350`) y a
partir de ahí `Accepts(gen)` exige `gen == t.gen` (`tracker.go:57`), así que el `Final` que
llegara después con el gen viejo se **descarta** (`controller.go:287`) y el `Stopped` también.
Se perdería el dictado entero justo en el caso en que hay algo que salvar.

Con `Final` **primero**: se acepta y se acumula con el gen aún vigente; si el cancel es
reintentable, `c.parts` sobrevive al `Bump` y se entrega cuando la sesión acabe de verdad; si
es terminal, `flushLocked` (`controller.go:343`) lo entrega en el momento. Correcto en ambas.

**Verificado leyendo `tracker.go:56-58` y `controller.go:286-289, 333-363`.**

### `Finalize` no se manda

`finalize` mantiene la sesión abierta y **no** produce `transcript.done`; `audio.done` sí, y
hace el flush. Pero no se puede esperar el `done` como único terminador (un `error` cierra sin
emitirlo) ⇒ timeouts.

## Enfoque: todo en `internal/stt/grok`

La v1 quería extraer un `internal/stt/wsstream` genérico desde ya, apoyándose en que
`StreamingSttSession` + `SttAdapter` era un diseño probado en producción. La revisión lo
desarmó: **ese precedente contiene el bug que estoy arreglando** (`streamingStt.ts:65` cierra
ante cualquier final tras el finalize, lo que trunca cuando hay varios `is_final=true` antes
del `done`), y la interfaz propuesta ya no podía expresar la distinción terminal/no-terminal.
Un precedente que hay que corregir no valida la abstracción.

Se extraerá **cuando llegue ElevenLabs**, leyendo el ciclo de vida común de dos
implementaciones reales. ⇒ `docs/plans/loqui-go-port.md:71` (`streamingStt.ts` →
`internal/stt/stream.go`) queda obsoleto: no habrá `stream.go` hasta entonces, y será paquete
propio, nunca parte del contrato.

```
internal/stt/grok/
  url.go        # BuildURL(language) — query params (puro)          ✅ hecho
  events.go     # tipos del protocolo + decode de un mensaje (puro)
  timeline.go   # la línea de tiempo por palabras (puro)
  provider.go   # Provider: ciclo de vida del socket
  errors.go     # HTTP del handshake → ErrorCode estructurado
```

## Concurrencia

La revisión encontró un P0 de liveness en la v2: un único canal de comandos que además
transporta audio se satura mientras `run()` está bloqueado escribiendo, y entonces o se
descarta el `stop` (se cuelga la sesión) o `PushAudio` bloquea (se congela el micrófono).

Estructura v3 — **tres caminos separados**, cada uno con su propia garantía:

1. **`run()`** es la única goroutine que toca el estado de la sesión y la única que escribe en
   el socket ⇒ el orden de los frames es el de aceptación, por construcción.
2. **Audio: un anillo acotado por bytes, con su propio mutex.** No va por canal.
   `PushAudio` toma el mutex, añade, y si se pasa del tope **descarta el más viejo**; luego
   hace una señal no bloqueante a `run()` de "hay audio". Así el descarte cae siempre en el
   PCM más antiguo, nunca en una señal de control, y `PushAudio` no puede bloquear ni pierde
   nunca el frame más reciente (que era el defecto de la v2).
   - Tope: **30 s** = 960 000 bytes a 16 kHz/16-bit/mono. Se registra una vez al empezar a
     descartar.
3. **Control: un canal propio que nunca se descarta.** `stop`, y los avisos del lector y de
   los timers. Capacidad pequeña y fija, y cada señal se manda **una sola vez**, así que no
   puede llenarse. `Stop` es idempotente (`sync.Once`) y **devuelve de inmediato**, como exige
   el contrato (`stt.go:73`) — nunca bloquea, así que puede llamarse incluso desde dentro de un
   callback del sink sin riesgo de deadlock.

Más:

- **`Stop` rechaza audio nuevo de forma atómica** (una bandera bajo el mutex del anillo) antes
  de pedir la finalización, y conserva lo aceptado antes. Hace falta porque `stopCapture`
  señala y cierra la captura pero **no espera** al pump (`dictation.go:357-369`), así que un
  frame tardío puede llegar después. Es lo que ya hace Azure (`recognizer.go:196`).
- **El canal de audio nunca se cierra** (no hay canal; y el anillo se marca cerrado bajo
  mutex), así que un `PushAudio` concurrente nunca escribe en algo cerrado.
- `PushAudio` y `Stop` **antes** de `Start`, y `Start` **dos veces**: definidos y probados —
  no-op y error respectivamente, como el helper (`helper/provider.go:108`).
- Todo cuelga de un `context` cancelable ⇒ ni el dial ni el lector ni el escritor sobreviven a
  `Stop`. La suite espera el **`sync.WaitGroup` del proveedor**, no un contador global de
  goroutines (que sería flaky, como señaló la revisión).
- `Stopped` sale exactamente una vez, sólo desde la salida de `run()`.

### Timeouts (diseño, no un timer suelto)

| Timeout | Valor | Regla |
| --- | --- | --- |
| **dial** | 10 s | Del `context` del `Dial`. Vencido → `Canceled{ConnectionFailure}` |
| **ready** (`transcript.created`) | 15 s | Armado al abrir el socket, desarmado al llegar `Ready`. Vencido → `Canceled{ServiceTimeout}` |
| **escritura** | 5 s por frame | `context` por `Write`. Vencido → ruta terminal |
| **finalize** | 10 s | Armado **al mandar `audio.done`**, no antes. Vencido → cerrar y emitir lo ensamblado |

Los cuatro son campos de `Config` con default cero ⇒ sólo los tests los bajan, igual que el
helper (`helper/provider.go:46-50`).

## Mapeo a `ErrorCode` (los de `internal/session/policy.go:31`)

| Situación | `ErrorCode` | Clase | Reintenta |
| --- | --- | --- | --- |
| Sin API key | `NotConfigured` | config | no |
| Handshake 401 / 403 | `AuthenticationFailure` | auth | no |
| Handshake 429 | `TooManyRequests` | network | sí |
| Handshake otro 4xx | `BadRequest` | config | no |
| Handshake 5xx | `ServiceUnavailable` | network | sí |
| Dial falla sin respuesta | `ConnectionFailure` | network | sí |
| Timeout de ready | `ServiceTimeout` | network | sí |
| Cierre inesperado del socket | `ConnectionFailure` | network | sí |
| Evento `error` del servidor | `BadRequest` | config | **no** |

La última fila es el punto discutido. La revisión objeta, con razón, que esos errores incluyen
"pipeline failures" y "stream timeouts", que no son culpa de la configuración, y que la
política queda incoherente (un `error` no reintenta pero un cierre abrupto sí, siendo
equivalentes).

Se mantiene igual, y es una **decisión con coste declarado**: mientras `controller.go:278`
resetee `reconnectAttempt` en cada `Started`, cualquier código reintentable puede convertirse
en un bucle sin tope contra un servicio que **factura por hora**. Un mensaje engañoso es un
coste menor que una factura abierta. Cuando se arregle el presupuesto de reintentos (el
cambio aparte), esta fila pasa a `ServiceError` con reintento acotado — está anotado allí.

Nunca se clasifica por prosa: `policy.go:36` documenta ese bug.

## Idioma y log

- Slot `grok` en `store.DefaultSettings()` con `["auto"]` ✅ hecho. `auto` ⇒ se **omite** el
  parámetro. En xAI `language` es un interruptor de **formato**, no una pista de reconocimiento.
- `Event.Language` **vacío**: el streaming no reporta idioma detectado.
- `Log func(tag, msg string)` como el helper. **Nunca** texto de transcripción, **nunca** la
  key. Sí: fallos de parseo/lectura/escritura/cierre, el `id` de `transcript.created`, el
  descarte por anillo lleno, y la rama del `done` sin `words`. **Con test** de que ni la key
  ni el texto acaban en el log.

## Escotilla de entorno ✅ hecho

`keyReaderFor` era Azure-only, así que una key de otro proveedor se ignoraba en silencio y la
lectura caía al Keychain que no responde con firma ad-hoc (bloqueo 1 de `CONTINUITY.md`).
Generalizada a un env var por slot (`LOQUI_GROK_KEY`, y los demás), con test de que un slot no
satisface la lectura de otro.

## Plan de pruebas (TDD: rojo antes de cada pieza)

### `url.go` ✅ hecho (4 casos, verdes)
Params fijos; `language` omitido con `auto`/vacío; presente con un idioma real; **sin** `model`.

### `events.go` (puro)
`transcript.created` → `Ready` + expone el `id`; `is_final=false` → `Partial`; `error` **plano**
→ `Error`; tipo desconocido → `Ignore`; JSON inválido → `Ignore` sin panic; `text` ausente o de
otro tipo → vacío sin panic; `confidence` ausente ("omitted when 0") no rompe el decode;
`words` ausente no rompe el decode.

### `timeline.go` (puro) — los contraejemplos de la revisión, uno por test
- dos finales sin solape → se unen en orden;
- **hueco de silencio**: palabras en `[0,1)` y `[4,5)`, luego un final en `[2,3)` → **las tres**
  sobreviven (el caso que rompía la v2);
- `speech_final` "stitched" que repite el trozo → reemplaza, no duplica;
- corrección del mismo tramo con distinto número de palabras → gana la nueva;
- **`done` que solapa parcialmente** dos tramos (`[0,2)`, `[2,4)`, done en `[1,3)`) → se
  conservan `"one"` y `"four"`;
- palabra a caballo de la frontera (previo hasta 3.2 con última palabra en 2.9, nueva 3.1→3.4)
  → no se pierde;
- **palabras adyacentes** (`0.24→0.48`, `0.48→0.96`) → no se borran entre sí (semiabierto);
- marcas idénticas → orden estable y determinista;
- final fuera de orden (start menor) → se inserta en su sitio;
- `done` con `text: ""` → no borra nada;
- `done` sin `words` con la línea de tiempo **no vacía** → se ignora y se registra;
- `done` sin `words` con la línea de tiempo **vacía** → se usa su texto;
- los interinos nunca modifican la línea de tiempo.

### `provider.go` (contra un servidor WS local real: `httptest` + `websocket.Accept`)
Cada caso afirma la **secuencia completa** y que nada llega después de `Stopped`:
- audio encolado antes de `Ready` se vuelca **en orden**; `Started` una vez;
- `Partial` con el `gen` sellado; **todo** evento lleva `Gen`;
- `Stop` manda `audio.done` (frame de **texto**) y espera el `done` ⇒ `Final` → `Stopped`;
- **varios `is_final=true` antes del `done`** → un solo `Final` con todo, nada truncado;
- el `done` que no llega → timeout de finalize, `Final` de lo ensamblado → `Stopped`;
- evento `error` → **`Final` primero**, luego `Canceled{BadRequest}`, luego `Stopped`;
- cierre abrupto → `Final` → `Canceled{ConnectionFailure}` → `Stopped`;
- handshake 401/403/429/4xx/5xx → los códigos de la tabla, sin `Final`;
- dial a un host inexistente → `Canceled{ConnectionFailure}`;
- sin key → `Canceled{NotConfigured}`;
- timeout de ready → `Canceled{ServiceTimeout}`;
- **`Stop` durante el dial** → `Stopped`, sin colgarse;
- **`Stop` compitiendo con `Ready`** → la cola se manda **antes** del `audio.done`, afirmado en
  el lado del servidor;
- **saturación determinista**: servidor que no lee, anillo lleno → descarta lo **viejo**, el
  `stop` **sigue llegando** y la sesión termina acotada (el P0 de liveness);
- `PushAudio` tras `Stop` → se rechaza, sin panic ni bloqueo;
- `PushAudio` y `Stop` antes de `Start` → no-op; `Start` dos veces → error;
- `Stop` dos veces, y `Stop` **desde dentro** del callback del sink → un solo `Stopped`, sin
  deadlock;
- mensaje entrante de 200 KB → se procesa (límite de lectura a 1 MiB, contra el default de
  32 KiB de `coder/websocket`);
- **wire**: el PCM sale como frame **binario**, el `audio.done` como **texto**, el header
  `Authorization: Bearer <key>` exacto, y los query params exactos;
- **privacidad**: ni la key ni el texto de transcripción aparecen en el log;
- **goroutines**: el `WaitGroup` del proveedor cierra en cada ruta terminal;
- `-race` limpio.

### `internal/app` ✅ parcial
Hecho: el env override por slot (3 casos). Falta: `buildProvider("grok")` sin key → error de
configuración distinguiendo "no hay key" de "el Keychain no contesta"; con `LOQUI_GROK_KEY` →
construye.

## Criterios de aceptación

1. `./scripts/task.sh test` verde con `-race`; `vet` y `gofmt` limpios.
2. `cmd/stt-probe --provider grok` transcribe varias frases y aparecen en `history.jsonl` como
   **un** mensaje, **sin duplicados**.
3. Una key inválida → **un** `Canceled{AuthenticationFailure}`, sin reintento.
4. `internal/stt` sigue sin importar `coder/websocket`.
5. Ninguna ruta nueva abre un bucle de reconexión sin tope.

## Riesgos

1. **`start` relativo a la sesión no está demostrado.** Documentado ("seconds from stream
   start") pero sin ningún ejemplo multi-enunciado. La línea de tiempo depende de eso. Si fuera
   relativo al enunciado, todos solaparían en `[0, dur)` y se sobreescribirían: el síntoma
   sería "sólo transcribe la última frase" — inconfundible, no silencioso. Mitigación: el
   experimento de cinco minutos de la investigación en cuanto haya key.
2. **El 401 del handshake no está documentado.** Se comprueba con una key mala.
3. **`error` ¿mata el socket?** Los docs se contradicen; se asume terminal.
4. **Sin key de xAI no hay criterio 2.** Todo lo demás se verifica contra el servidor local.
5. **La rama del `done` sin `words` es mejor esfuerzo declarado**, no una garantía. Nunca
   duplica; puede perder una corrección final. (La retractación *con* `words` sí quedó resuelta
   por `speech_final` — ver el punto 4 de los hallazgos de implementación.)

## Fuera de alcance: dos bugs preexistentes (decisión del usuario)

Están **ya** en `main` y afectan a Azure hoy. Verificados leyendo el código. Van en su propio
cambio, con su propio ciclo de revisión:

1. **El presupuesto de reintentos no acota nada si la conexión llega a abrir.**
   `controller.go:278` pone `reconnectAttempt = 0` en cada `Started`; el único tope está en
   `controller.go:339`. Un ciclo `Started → Canceled(reintentable) → reconectar` se repite
   **para siempre** al primer intervalo de backoff. Con facturación por hora es un bucle de
   gasto. Arreglo probable: resetear cuando la sesión produce texto, no al conectar; o
   presupuesto por ventana de tiempo. **Al arreglarlo, el evento `error` de Grok pasa a
   `ServiceError` con reintento acotado.**
2. **La reconexión filtra la captura anterior.** `controller.go:359` llama a `StartEngine` sin
   `StopEngine`; `dictation.go:115` sobreescribe `d.provider` y `dictation.go:359-361` los
   únicos handles de `capture`/`pumpDone` ⇒ cada reintento filtra un device y una goroutine, y
   el habla durante el backoff se pierde. Relacionado: `stopCapture` no **espera** al pump, de
   ahí que el proveedor tenga que rechazar audio tardío por su cuenta (ya contemplado arriba).

## Lo que cambió al implementar (hallazgos de la fase de código)

Tres cosas que ni el plan ni las dos revisiones de diseño vieron, y que sólo aparecieron
escribiendo el código y hablando con el servicio real:

1. **El servicio devuelve 400, no 401, con una key inválida.** Verificado contra
   `api.x.ai` el 2026-07-28, contradiciendo la propia tabla de errores de xAI. Mapear sólo por
   status daba el mensaje "xAI rechazó la petición (status 400)", que manda al usuario a auditar
   su configuración cuando lo que pasa es que la key está mal. `handshakeFailure`
   (`internal/stt/grok/errors.go`) lee el cuerpo para distinguirlo. **No** reintroduce el bug de
   clasificar por prosa: ambas ramas son no-reintentables, así que cambia el mensaje y nunca el
   comportamiento.
2. **`Stop` perdía la cola del dictado** (encontrado en el auto-repaso, y confirmado
   independientemente por la revisión de código como P0). `select` elige al azar entre casos
   listos, así que el `stop` podía ganarle a su propio audio y mandar `audio.done` antes de los
   últimos frames. El test lo reprodujo: llegaban **4 de 80 bytes**. Ahora se drena antes de
   finalizar.
3. **El reemplazo de la línea de tiempo usaba la envoltura convexa de las palabras entrantes**
   (P1 de la revisión de código). Palabras entrantes en `[0,1)` y `[4,5)` daban `[0,5)` y
   borraban una palabra almacenada en `[2,3)` que nada entrante solapaba. El test de hueco de
   silencio existente sólo cubría el orden de llegada contrario, que pasa de las dos formas —
   así se escapó. Ahora se compara contra **cada palabra entrante**.
4. **`speech_final` resuelve el caso que se había dado por irresoluble.** La v3 del plan
   declaraba "mejor esfuerzo" ante una **retractación**: si el servidor reenvía un tramo con
   *menos* palabras ("a b c" → "a c"), la "b" no solapa nada entrante y sobrevive como texto
   viejo. La revisión de código señaló que la señal existe y la estaba tirando a la basura: los
   docs llaman al evento `speech_final=true` la "complete **stitched** utterance", o sea
   **autoritativa para todo su tramo**. Así que ahora hay dos reglas, no una:
   - final de trozo (`speech_final=false`) → **incremental**, reemplazo por palabra;
   - final de enunciado (`speech_final=true`) y `transcript.done` → **autoritativo**, se limpia
     el tramo entero y se insertan sus palabras.

   Y el tramo que limpia un evento autoritativo es el **declarado** (`start`, `start+duration`),
   unido con la extensión de sus propias palabras — **no** la envoltura de las que sobreviven.
   Esa distinción hizo falta una ronda más: con la envoltura, una retractación al **principio** o
   al **final** del enunciado queda fuera y sobrevive (`[a b c]` restado a `[b c]` dejaba la "a").
   `transcript.done` no trae tramo declarado, así que cae a sus palabras — que es justo lo que lo
   mantiene seguro sin saber si repite toda la sesión o sólo la cola.

   Con eso la retractación funciona en las tres posiciones y el hueco de silencio sigue
   funcionando, sin heurísticas. Ya no queda caso residual.

## Registro de las revisiones

**Iteración 1 — codex `gpt-5.6-sol` xhigh — REWORK.** 2 P0, 7 P1, 3 P2.

| Hallazgo | Resolución |
| --- | --- |
| P0 — el `error` reintentable abre un bucle sin tope | `error` → terminal. Bug del controlador documentado aparte |
| P0 — cerrar al primer `Final` trunca antes del `done` | Ruta terminal explícita; **un** `Final` ensamblado al salir |
| P1 — la marca de agua pierde palabras y no admite correcciones | Sustituida por la línea de tiempo |
| P1 — el fallback sin `words` duplica | Regla revisada otra vez en la it. 2 |
| P1 — `sync.Once` no ordena `Final` antes de `Stopped` | Una goroutine dueña; orden corregido otra vez en la it. 2 |
| P1 — `Stop` antes de `Ready` pierde la cola | `pendingFinalize`: volcar y **después** `audio.done` |
| P1 — el volcado puede reordenar el PCM | Un único escritor |
| P1 — extraer `wsstream` es prematuro | **Aceptado**: todo en `stt/grok` |
| P1 — la reconexión filtra la captura | Preexistente; fuera de alcance por decisión del usuario |
| P2 — límite de lectura de 32 KiB | 1 MiB, con test |
| P2 — "400 frames" no acota | Anillo por bytes |
| P2 — los tests afirman poco | Secuencias completas + fugas |

**Iteración 2 — codex `gpt-5.6-sol` xhigh — REWORK.** 2 P0 nuevos, 5 P1, y 5 de la it. 1
marcados como no resueltos. Todos verificados contra el código; ninguno descartado.

| Hallazgo | Resolución en la v3 |
| --- | --- |
| **P0** — `[Canceled] → Final` pierde el dictado en la ruta reintentable (`Bump` invalida el gen) | **Orden corregido a `Final → [Canceled] → Stopped`.** Verificado en `tracker.go:56-58` y `controller.go:286-289, 333-363` |
| **P0** — liveness bajo contrapresión: un solo canal descarta el `stop` o bloquea el micrófono | **Tres caminos separados**: anillo de audio por bytes con su mutex, canal de control que nunca se descarta, `Stop` idempotente que no bloquea |
| P1 — el reemplazo por segmento pierde tramos no solapados (huecos, solape parcial) | **Línea de tiempo por PALABRA** con intervalos **semiabiertos**; sólo se borran las palabras que solapan |
| P1 — ninguna regla de cadenas para el `done` sin `words` es correcta | **Aceptado**: ya no se compara. Se ignora `done.text` si hay línea de tiempo (y se registra); se usa sólo si está vacía. **Limitación declarada** |
| P1 — frames tardíos del pump cruzan la finalización | `Stop` rechaza audio nuevo atómicamente antes de finalizar, como Azure (`recognizer.go:196`) |
| P1 — `BadRequest` es semánticamente incorrecto para errores del servidor | **Se mantiene, con el coste declarado**: evita una factura abierta mientras el presupuesto de reintentos siga roto. Anotado para cambiar a `ServiceError` cuando se arregle |
| P1 — el leak de captura está en alcance | **Discrepancia consciente**: decisión del usuario. Grok no abre rutas nuevas hacia él (por eso el mapeo terminal) |
| P2 — faltan casos (saturación, reentrada, wire, privacidad, timeouts) | Añadidos todos, más el diseño de los cuatro timeouts con sus reglas |
| P2 — el contador global de goroutines es flaky | Sustituido por el `WaitGroup` del proveedor |
