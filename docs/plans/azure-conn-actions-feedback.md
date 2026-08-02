# Las acciones del card de conexión: "Probar conexión", y decir que algo pasó

## Goal

Que las cuatro acciones del card de un motor en Ajustes → Conexiones **hagan algo observable**:
portar "Probar conexión", que un botón deshabilitado se vea deshabilitado, y que un éxito se diga
en vez de dejar la línea de estado en blanco.

## Architecture

Sin cambios estructurales. Se mantiene el reparto del port: las reglas y los mensajes viven en Go,
la página pinta lo que recibe y no decide nada. Lo nuevo es un método bindeado de solo lectura
(`SettingsService.TestConnection`) y un campo más en el resultado que ya devuelven los setters.

## Tech Stack

Go 1.x + Wails v3 (bindings generados), TypeScript sin comprobación de tipos (deuda declarada en
`CONTINUITY.md:31-33`), CSS del maquetado original de Electron.

## El síntoma que lo abrió

El usuario, validando el vínculo de un motor nuevo: puso la clave de Azure y pulsó **Probar
conexión**, **Usar este motor**, **Borrar clave** y **Guardar** — "no funciona ninguna acción".

## Causa raíz — cuatro causas distintas, un solo síntoma

Los logs de la app durante esa sesión (`UI-ACTION`) demuestran que **dos de los cuatro botones
llegaron a llamar al backend, y ambas llamadas tuvieron éxito**:

```
11:18:39  UI-ACTION  map[action:setProvider(azure) error: ok:true]
11:41:02  UI-ACTION  map[action:saveConnection(azure) error: ok:true]
11:41:02  CONN       map[... azure=active ...]
```

De los otros dos no hay traza, y por razones distintas: "Probar conexión" no tiene handler que
emitir, y "Borrar clave" estaba deshabilitado, así que su clic no llegó a ninguna parte.

| # | Acción | Qué pasa de verdad | Evidencia |
|---|--------|--------------------|-----------|
| 1 | Probar conexión | **Nunca se portó.** `#test` no tiene handler y no hay método bindeado. La lógica existe y está testeada pero nadie la llama en producción. | `index.html:979`; `settings.ts:665-717` no engancha `#test`; `azure.TestConnection` en `internal/stt/azure/token.go:148` sin llamadores; ya declarado inerte en `CONTINUITY.md:86-91` |
| 2 | Borrar clave | Deshabilitado correctamente (no había clave), pero **indistinguible de uno activo**: no existe regla `:disabled` y `button.btn` fija color y fondo y conserva el `:hover`. | `settings.ts:534-541`; `index.html:317-325` |
| 3 | Guardar | Funcionó. `run()` pinta `res.error`, que en éxito es cadena vacía → **el éxito es indistinguible de un clic perdido**. | `settings.ts:626`; log `saveConnection(azure) ok:true` |
| 4 | Usar este motor | Funcionó, pero sin clave el estado sigue siendo `unconfigured`: la insignia sigue diciendo "Sin configurar" y el botón sigue ahí. Ningún cambio visible **en el card**. | `settings_write.go:54-73`; `connection.go:ConnectionStateFor`; log `setProvider(azure) ok:true` |

Las cuatro comparten una causa de percepción: **la línea de estado solo habla cuando algo falla**,
así que un éxito y un clic que no llegó se ven igual. Los arreglos 3 y 4 atacan esa causa; 1 y 2 son
controles que nunca se terminaron.

## Los arreglos

### 1. `SettingsService.TestConnection(slot, region, secret) ProbeResult` — nuevo, en `internal/app/settings_probe.go`

```go
type ProbeResult struct {
    OK      bool            `json:"ok"`
    Error   string          `json:"error"`
    Message string          `json:"message"`
    Payload SettingsPayload `json:"payload"`
}
```

**No devuelve error de Go**, por la misma razón que `WriteResult`: Wails descarta el resultado de un
método que también devuelve error.

**Sí devuelve payload, aunque no mute nada.** La primera versión de este plan no lo llevaba —
"no muta, no hay nada que repintar"— y eso era falso en un caso que este código conserva a
propósito: una escritura de Keychain que **agota su plazo y aterriza después** (la llamada cgo no se
puede cancelar; ver `SetKey` y el test que fija ese comportamiento en `settings_write_test.go:582`).
Secuencia real en esta build de firma ad-hoc: Guardar agota los 10 s → el payload dice `unreadable`
→ la escritura aterriza tarde → el usuario prueba con el campo vacío → el probe **sí** encuentra la
clave y dice que todo va bien, mientras la insignia, la etiqueta y el botón Borrar siguen diciendo
lo contrario. El recovery por `Settings.Load` no lo cubre: solo corre ante una excepción de
transporte (`settings.ts:632-644`), y un timeout de Keychain llega como `WriteResult` normal.

Se devuelve el payload **entero** y se entrega a `paint()`, en vez de un snapshot parcial de
`KeyState` + `ConnectionRow`: un segundo camino de pintado con sus propias reglas de consistencia es
más superficie de la que este bug justifica. **Un solo árbitro para todos los payloads**: `paint()`
descarta el que traiga una `Revision` menor que la última pintada, venga del probe, de una escritura
o de Sistema. El epoch de card no interviene aquí — decide únicamente quién escribe el mensaje.

Cuesta una lectura más del Keychain (el fan-out de `keyStates`, hasta ~3 s en esta build). Se paga
porque su razón de ser es precisamente decir la verdad sobre el estado de la credencial.

**Y eso obliga a decir con precisión qué NO hace un rechazo.** Un probe rechazado no sale a la red y
no resuelve la clave que iba a probar — pero **sí** lee el Keychain, porque el payload que devuelve lo
lee, como lo lee cualquier `Load()` o cualquier setter (`bootstrap.go:477` → `store.KeyStatusFor`).
Decir "cero lecturas del Keychain" era falso. Los tests afirman lo que de verdad se cumple: cero
HTTP y cero llamadas al seam de resolución del probe.

**Orden de operaciones, deliberado y testeado** — todo lo que puede fallar sin red se resuelve antes
de salir a la red:

1. Slot conocido → si no, error.
2. Slot con prueba disponible, contra una **lista explícita de slots que tienen probe** — hoy solo
   `azure-speech`. **No vale `store.IsAvailableKeySlot`**: es true también para `grok`
   (`keychain_darwin.go:119-125`), así que usarla dejaría pasar una clave de Grok al probe de Azure,
   que la mandaría al endpoint STS de Azure. Cualquier slot sin probe — `grok` incluido, no solo el
   no portado `azure-openai` — devuelve un error honesto de "prueba no disponible", sin salir a la red
   y sin resolver ninguna credencial.
3. **Región**: la del formulario si viene, la guardada si no; `settings.NormalizeRegion` la valida.
   Falla → se devuelve, **cero HTTP**. Esto importa porque `LoadSettings` acepta cualquier cadena
   que sea JSON válido (`config.go:178`), así que la región guardada puede no ser válida.
4. **Clave**: la escrita si viene; si no, `LOQUI_AZURE_KEY` cuando no está vacía; si no, el
   Keychain. Probar antes de guardar es el caso de uso, así que la clave del formulario manda; **a
   partir de ahí** la precedencia es la de `keyReaderFor` (`dictation.go:624-632`), que solo decide
   entre entorno y Keychain. Con el campo vacío, prueba y dictado leen lo mismo — si discreparan, la
   prueba bendeciría una credencial que el dictado no usa.
5. Solo entonces: `context.WithTimeout` y `azure.TestConnection`.

**El fallo del Keychain no se confunde con "no hay clave"** (P1 de Codex, iteración 1). `GetKey`
devuelve tres resultados distintos y la distinción es load-bearing: `ErrNoSecret` significa
escríbela, `ErrKeychainTimeout` significa que la firma de la app está mal y reescribirla no
arreglará nada (`keychain_darwin.go:256-260`, y así lo trata ya el dictado en
`dictation.go:248-260`). Se clasifican con `errors.Is`. Ninguno de los tres sale a la red.

**El mensaje del timeout es propio, no el de `keychainMessage`.** Ese texto
(`settings_write.go:418-427`) describe una **escritura** indeterminada que todavía puede aterrizar y
manda reabrir Ajustes para ver el estado real; en una lectura no hay mutación tardía que esperar y
esa instrucción sería ruido. La redacción correcta aquí es la que ya usa el dictado
(`dictation.go:252`): el Keychain no respondió, firma la app con una identidad estable o pasa la
clave en `LOQUI_AZURE_KEY` para probar.

**Presupuestos de tiempo, dichos con precisión.** El probe construye su propio
`&http.Client{Timeout: 15 * time.Second}` cuando `probeClient` es nil, en vez de pasar nil y dejar
que `NewTokenService` instale el suyo de 10 s (`token.go:73-75`): así el número del código y el de
esta línea son el mismo. El `context.WithTimeout` de 15 s acota el intercambio HTTP; la lectura del
Keychain trae su propio límite de 3 s dentro de `store.GetKey` y ocurre **antes**. Sumando el payload
del final, que vuelve a leer el Keychain, el peor caso de la llamada Go es **~21 s**: 3 de resolver
+ 15 de red + 3 de payload. **Desde el clic no hay máximo**: la página espera además a que se drene
la cola de escrituras (arreglo 1b), y eso no está acotado.

**Seams de test** (la red y el Keychain nunca se tocan en un unit test), ambos en `SettingsService`
junto a los que ya existen:

- `probeClient azure.Doer` — nil = `http.Client` real.
- `getSecret func(store.KeySlot) (string, error)` — nil = `store.GetKey`. Hoy el servicio solo tiene
  seam de escritura y de borrado (`bootstrap.go:339-342`); sin este, los tres resultados del
  Keychain no son testeables.

**El botón queda siempre habilitado**, salvo mientras vuela su propia prueba. Es lo que pidió el
usuario y es lo correcto: la prueba se usa precisamente cuando el motor NO está bien configurado,
así que condicionarla al estado la haría inútil justo cuando hace falta.

### 1b. Coordinación del feedback: un epoch por card (P1 de Codex, iteración 1)

El probe y las tres escrituras escriben en **el mismo** `.status` del card (`settings.ts:670`). Como
el probe no va en la cola de escrituras (`writes`, `settings.ts:583-593`), una prueba lenta puede
terminar **después** de un Guardar posterior y sobrescribir su mensaje con un resultado viejo — la
misma llegada fuera de orden que la cola evita para las escrituras.

Dos medidas, cada una para un problema distinto:

- **Epoch por card, y SOLO sobre el `.status`.** Un contador por elemento `.conn`; cada acción
  (escritura o prueba) se queda con el valor al empezar y **solo escribe el mensaje si nadie ha
  empezado otra acción en ese card desde entonces**. El mensaje viejo se descarta en silencio: ya no
  describe nada.

  **El payload de una ESCRITURA no se gobierna por el epoch.** Es la corrección de un fallo de la
  primera versión de este plan: si el epoch decidiera sobre el resultado entero, la secuencia
  Guardar → Probar descartaría el repintado del Guardar — y ese payload es lo único que actualiza la
  insignia, el estado de la clave y el botón Borrar (`settings.ts:519-541`). La clave quedaría
  guardada con el card mostrando lo contrario. Las escrituras repintan **siempre** y **en orden**,
  que es lo que la cola ya garantiza (`settings.ts:583-593`). El epoch arbitra dos cosas: quién
  habla en la línea de estado, y si el payload **del probe** se aplica o se tira (arreglo 1) — ese
  sí, porque si llega tarde el card ya lo repintó algo más nuevo.

  **El payload se arbitra por una REVISIÓN que estampa el backend, no por un contador de pintados.** Un epoch por card no sirve para él y esto es un fallo de la iteración anterior de este
  plan: `paint()` no repinta un card, repinta **la página entera** — región, selector de motor, los
  seis cards, Sistema y onboarding (`settings.ts:334-549`). Con el arbitraje por card, una prueba de
  Azure podía capturar el estado A, una acción en Grok o en Sistema pintar B, y el probe aplicar A
  encima porque el epoch **de Azure** seguía intacto.

  Un contador que `paint()` incrementara tampoco sirve, y esta es la segunda corrección: mediría
  "alguien pintó", no la antigüedad del snapshot. Un guardado de idioma o de Sistema puede **empezar
  antes** que la prueba, fuera de `writes`, y pintar **después** de que la prueba capture el contador
  (`language.ts:35`, `system.ts:36`, `onboarding.ts:36`, `settings.ts:791-793`): el payload viejo
  pasaría y el fresco de la prueba se descartaría por haber llegado detrás.

  Así que la recencia la estampa quien produce el snapshot: `SettingsPayload` gana un campo
  `Revision`, y `paint()` ignora todo payload con revisión menor que la última pintada. El contador
  es un **`atomic.Uint64` de `Bootstrap`**, y su `Add(1)` es la **primera** operación de `Payload()`.
  Atómico no es adorno: Wails despacha cada llamada bindeada en su propia goroutine, así que dos
  payloads pueden construirse a la vez — que es exactamente la situación que el sello viene a ordenar.
  Un `revision++` normal daría carrera, incrementos perdidos y revisiones repetidas, y dos snapshots
  con la misma revisión son dos snapshots que no se pueden ordenar. El mensaje sigue arbitrado por epoch de card, que
  es donde se muestra.

  **Lo que eso garantiza, exactamente:** que un snapshot que EMPEZÓ antes no pise a uno que empezó
  después. No es una garantía total — `Payload()` lee el disco, el Keychain y los dispositivos en
  varios instantes, así que en teoría un snapshot que empezó antes puede haber leído un campo
  concreto más tarde. Es estrictamente mejor que lo de hoy (donde no hay arbitraje ninguno) y se
  documenta como lo que es, no como orden total.

  **Efecto secundario deliberado:** la revisión arbitra TODO el que pinta, no solo el probe — así que
  también cierra la carrera preexistente entre Sistema, idiomas, onboarding y Conexiones, que hasta
  ahora podía repintar un card con un snapshot más viejo. No se buscaba en este cambio; sale gratis
  del mecanismo correcto, y dejar fuera a los demás productores exigiría más código para conservar
  una carrera.

- **`await writes` antes de la prueba.** La prueba no entra en la cola (no debe bloquear un Guardar
  posterior), pero espera a que se drene lo que ya estaba pendiente. Sin eso, probar con el campo
  vacío justo después de un Guardar leería la clave **anterior** del Keychain y diría que la nueva
  falla. La región y el secreto del formulario se capturan **en el clic**, antes de esa espera,
  igual que ya hace Guardar (`settings.ts:690-698`): si se leyeran después, la prueba usaría lo que
  hubiera en el DOM al terminar la espera, no lo que el usuario pulsó.

### 2. Estado `:disabled` visible — `frontend/index.html`

```css
button.btn:disabled { opacity: 0.45; cursor: not-allowed; }
button.btn:disabled:hover { background: var(--card-bg); filter: none; }
button.btn.primary:disabled:hover { background: var(--accent); }
```

`button.btn:disabled:hover` tiene especificidad 0-3-1 contra 0-2-1 de `button.btn:hover`, así que el
hover deja de responder en un botón muerto. Alcanza a todos los `.btn` de la app, que es lo
deseable: por estado se deshabilitan hoy `.conn-delete` sin clave y `.conn-save` en un card no
soportado (`settings.ts:531-541`), y además `run()` deshabilita el botón pulsado mientras vuela, lo
mismo que hacen los de Permisos — ese parpadeo tampoco se veía. No alcanza los chips de idioma ni
`.btn-record`, que no son `button.btn`.

### 3. `WriteResult.Notice` — el éxito también se dice

Campo nuevo, `json:"notice"`, vacío por defecto. **El comentario de `WriteResult`
(`settings_write.go:29-40`) se actualiza para describir los tres campos como comportamiento actual**
— ese texto se copia literalmente a `models.ts` en los bindings, así que dejarlo describiendo dos
campos lo convierte en documentación falsa en dos sitios.

| Setter | Notice |
|--------|--------|
| `SaveConnection` | "Clave guardada" / "Región guardada" / "Clave y región guardadas", según lo que se escribió de verdad |
| `DeleteKey` | "La clave ya no está guardada" — **postcondición, no acción**: `DeleteKey` es idempotente ("Absent is success", `keychain_darwin.go:312`) y el servicio no conoce el estado anterior, así que "Clave borrada" mentiría cuando no había ninguna |
| `SetProvider` | ver arreglo 4 |

**Por qué el texto viene de Go y no de la página**, aunque `system.ts:36-54` pase un `okText` desde
TypeScript para el panel Sistema: los tres mensajes de aquí dependen de hechos que **solo Go tiene**
— qué se escribió realmente, cuál es el estado resultante del motor, y qué contestó Azure. Un
`okText` fijo en la página sería una cadena que puede no ser verdad.

La **convención de pintado** se toma de lo que ya existe, sin sobregeneralizarla: las clases
`ok`/`err` sobre el elemento de estado y el `✓`/`✗` delante del texto salen de dos sitios que las
usan de forma parecida pero no idéntica — `permissions.ts:139-144` pinta `status ok|err` con `✓` en
el éxito y sin `✗` en el error, y `system.ts:44-45` pinta `lang-status ok|err` con `✗` en el error.
Aquí el elemento es `.status`, cuya CSS ya define `.ok` y `.err` (`index.html:328`), y se usan las
dos marcas: `✓ <notice>` y `✗ <error>`.

**Alcance declarado, con las cuentas hechas:** el servicio tiene **12** setters
(`settings_write.go`). Reciben notice los **3** del card de conexiones — `SaveConnection`,
`DeleteKey`, `SetProvider`. Los **9** restantes no, y no todos son "de Sistema": **5** se alcanzan
desde el panel Sistema (`SetAppearance`, `SetAppLanguage`, `SetInputDevice`, `SetMode`,
`SetTrigger`), **1** desde idiomas (`SetLanguages`), **3** desde el tutorial o desde ningún sitio
(`SetKey`, `SetOnboarded`, y `SetRegion`, que hoy no tiene ni un llamador en el frontend). Los de
Sistema e idiomas ya dan feedback por su propio `okText` y persisten al cambiar sin botón, que es
otro patrón; el bug reportado es el card de conexiones.

### 4. `SetProvider` dice si el motor elegido sirve

El notice se deriva del estado del motor recién guardado, leído de las filas que ya calcula
`store.ConnectionRows` en el payload que el propio setter devuelve — la regla no se reimplementa:

- `active` → "Motor activo: Azure"
- `connected` / `available` → no ocurre para el motor recién elegido (ambos significan "no
  seleccionado"); si apareciera, se cae al texto genérico "Motor guardado".
- `unsupported` → "Motor guardado, pero no puede funcionar en este sistema"
- `unconfigured` → **dos textos, no uno**, porque este estado colapsa dos situaciones distintas:
  `presenceMap` reduce `unreadable` a "no hay clave" (`bootstrap.go:303-313`) y `ConnectionStateFor`
  no puede distinguirlas después (`connection.go:199-201`). Decir "le falta configuración,
  complétala" a alguien cuya clave SÍ está guardada pero cuyo Keychain no contestó es falso y le
  manda a reescribirla para nada — exactamente el error que la distinción de tres estados existe
  para evitar. Así que el notice consulta también `Payload.Keys` para el slot del motor:
  - clave con estado `unreadable` → "Motor seleccionado, pero el Keychain no respondió — no se puede
    confirmar si su clave está disponible"
  - cualquier otro caso → "Motor seleccionado, pero le falta configuración — no podrá dictar hasta
    que la completes"

**El caso `unsupported` SÍ es alcanzable** (P1 de Codex, iteración 1; mi primera versión afirmaba lo
contrario y era falso): `SetProvider` solo consulta `IsAvailableProvider`, que es un mapa global de
"portado" (`config.go:389-396`), mientras que `unsupported` depende además de la máquina —
plataforma, versión de macOS y presencia del helper (`connection.go:IsAvailableOn`). En un Mac con
macOS 15, `macos` es un motor portado y no soportado a la vez, y el binding lo acepta.

**Lo que este cambio NO hace: rechazarlo.** Que `SetProvider` deba negarse a guardar un motor que
esta máquina no puede ejecutar es una decisión de comportamiento distinta del bug reportado —
cambia qué se acepta, no qué se informa, y afecta a los estados del selector que se verificaron en
la sesión anterior. Aquí se informa con honestidad y se deja anotado.

**El seam `caps`, con cuidado de no romper lo que ya existe.** `hostCapabilities()`
(`bootstrap.go:320-329`) lee la máquina real sin inyección posible, así que se añade
`caps func() store.HostCapabilities` a `Bootstrap` junto a `keyStatus` / `perms` / `devices`. Pero
`Bootstrap` **se construye como literal en dos helpers de test** (`bootstrap_test.go:28-39`,
`settings_write_test.go:63-85`), no solo por `NewBootstrap`, así que llamar a `b.caps()` a secas
haría panic en toda la suite. Dos medidas juntas:

- **Accesor con fallback nil-safe**: `caps == nil` → `hostCapabilities()`. Ningún llamador puede
  petar por no conocer un campo nuevo.
- **Valor determinista en los dos helpers de test: `store.HostCapabilities{}`.** Hoy esos tests leen la máquina del
  desarrollador — plataforma, versión de macOS y presencia de helpers — así que su resultado depende
  de dónde se ejecuten. Fijarlo los aísla. Si alguna expectativa existente cambia al fijarlo, eso
  **es** el hallazgo: el test dependía de la máquina. Se trata en su sitio, no revirtiendo el seam.

### 5. Validación del formulario y habilitado de los botones (pedido por el usuario, 2026-08-01)

El encargo creció durante la validación: además de que las acciones digan algo, el card debe
**impedir** las que no tienen sentido y **señalar el campo** que falta.

| Regla pedida | Cómo se implementa |
|---|---|
| Guardar sin clave (ni escrita ni guardada) → borde rojo en el input y "la clave es obligatoria" | `SaveConnection` ya rechaza "no hay nada que guardar"; se le añade el caso "hay región pero ninguna clave, ni escrita ni guardada" y un campo nuevo en `WriteResult` que dice **qué input** señalar |
| Probar sin clave → la misma validación, sin salir a la red | El probe ya devuelve "falta la clave" sin HTTP; ahora también dice qué campo señalar |
| Tras probar, decir si la conexión sirvió | `ProbeResult.Message` / `.Error` (arreglo 1) |
| "Usar este motor" solo con conexión guardada | Matriz completa por estado, no `state !== "connected"`: `active` y `unsupported` siguen **ocultos** (como hoy); `unconfigured` pasa a **visible y deshabilitado**; `connected` y **`available`** visibles y habilitados. `available` es load-bearing: es el estado de Whisper y macOS, que no llevan credencial — tratarlos como "no configurados" haría inseleccionables los dos motores locales (`connection.go:190-215`) |
| "Borrar clave" solo con config guardada | **Ya es así** (`settings.ts:534-541`); lo que faltaba era que se notara — arreglo 2 |

**`WriteResult.Field` / `ProbeResult.Field`**, valores `"key"`, `"region"` o vacío. La página no
decide qué está mal: recibe el nombre del input y le pone la clase de error. Es la misma regla de
reparto que el resto del port — si la página dedujera "el error habla de la clave", estaría
reimplementando en TypeScript una validación que vive en Go.

**El borde rojo hay que dibujarlo: la clase no existe.** Hoy la CSS solo define el borde normal y el
de foco de los inputs, y `.status.err` para el texto (`index.html:275-283`, `:328`) — añadir una
clase a un input no lo pondría rojo. Así que:

```css
.conn-form input.invalid, .conn-form select.invalid { border-color: var(--err); }
.conn-form input.invalid:focus, .conn-form select.invalid:focus {
  border-color: var(--err); box-shadow: 0 0 0 3px color-mix(in srgb, var(--err) 22%, transparent);
}
```

**Cuándo se limpia**, que es la mitad que se olvida: al escribir en ese input (`input`) y al empezar
cualquier acción nueva en el card. Un borde rojo que sobrevive a la corrección es peor que no
tenerlo — el usuario arregla lo que le piden y la interfaz sigue acusándole.

**Qué exige exactamente la validación de `SaveConnection`.** Hoy el guardado de solo-región es
válido a propósito y hay dos regresiones que lo fijan (`settings_write_test.go:379`, `:554`), así que
la regla nueva se escribe para no romperlas — resolviendo la clave con la MISMA precedencia que el
resto:

| Situación | Resultado |
|---|---|
| Secreto escrito en el formulario | válido, se guarda |
| Campo vacío + `LOQUI_AZURE_KEY` no vacía | válido; **no se consulta el Keychain** y se guarda solo la región |
| Campo vacío + el Keychain devuelve la clave | válido, se guarda solo la región |
| Campo vacío + `ErrNoSecret` (no hay clave en ningún sitio) | **rechazo** con `Field="key"` y "la clave es obligatoria" |
| Campo vacío + `ErrKeychainTimeout` u otro error | mensaje propio de lectura fallida, `Field` vacío — **nunca** "falta la clave": acusar de ausencia a quien tiene la clave guardada es el mismo error que ya se corrigió en el probe |

**Lo que NO se convierte en estado nuevo: "probada".** El usuario pidió también que, si hay clave
pero no se ha probado la conexión, se avise. Con la respuesta de que "establecida" = **configurada y
guardada** (no hace falta prueba para usar el motor), ese aviso no necesita persistirse: se muestra
**tras guardar** — "Clave guardada. Pulsa Probar conexión para comprobar que sirve" — y desaparece
cuando la prueba pasa en esa sesión. Persistir un "probada" obligaría a invalidarlo en cada cambio
de clave o región, y sin persistirlo bien la app pediría reprobar un motor que lleva meses
funcionando en cada arranque. El original de Electron tampoco lo tiene: `connectionStatus.ts`
calcula la disponibilidad igual que el port.

**Guardar solo la región sigue siendo válido** cuando ya hay una clave guardada: es como se cambia
de región sin volver a pegar la credencial. La validación nueva solo salta cuando **no hay clave por
ningún lado**.

## Task list

1. `internal/app/settings_probe.go` + `settings_probe_test.go` — el probe, sus dos seams y la
   clasificación de errores del Keychain (rojo primero).
2. `internal/app/bootstrap.go` — seam `caps` en `Bootstrap`.
3. `internal/app/settings_write.go` + `settings_write_test.go` — `Notice` en los tres setters y el
   comentario de `WriteResult` actualizado.
4. `./scripts/task.sh common:generate:bindings` — método, struct y campo nuevos.
5. `frontend/src/settings.ts` — handler de `#test`, epoch por card, `await writes` antes de la
   prueba, pintado `✓`/`✗` con clases `ok`/`err`.
6. `frontend/index.html` — la regla `:disabled`.
7. `wiring.go` + `settings.ts` — sonda `LOQUI_DEBUG_CONN_CLICK`. Un botón dentro de un webview de
   Wails no se puede clicar desde un script (`CONTINUITY.md:71-77`). Gramática:
   `<provider>:<acción>[:<argumento>]`, y con `+` se encadenan acciones **sin esperar** a que la
   anterior termine, que es lo que hace verificables UC-3 y UC-4:
   - `azure:test` — pulsa "Probar conexión" tal cual está el formulario.
   - `azure:test:badkey` — escribe un sentinel fijo e inválido (`loqui-debug-clave-invalida`) en el
     campo antes de pulsar. **Nunca acepta una clave arbitraria del entorno**: eso metería un
     secreto real en el entorno y en los logs.
   - `azure:save-region:<id>` — elige esa región en el desplegable y pulsa Guardar.
   - `azure:clear-region` — deja el desplegable en el placeholder vacío, sin pulsar nada. Es lo que
     hace verificable UC-3.
   - `azure:test+save`, `azure:save-region:<id>+clear-region+test` — encadenadas sin espera.

   **La sonda se dispara con `ui:painted`, no con un `time.Sleep`.** El hook existente de
   `LOQUI_DEBUG_SET_PROVIDER` espera 3 s fijos (`wiring.go:272-278`), y eso es frágil aquí: `wire()`
   solo corre después de que resuelva `Settings.Load` (`settings.ts:753`), cuya lectura del Keychain
   puede consumir esos mismos 3 s en esta build. Un comando que llega antes del cableado se pierde
   en silencio y el UC parece fallar por otra cosa.
8. `internal/app/settings_probe.go` — dos líneas de log, sin secreto: `PROBE region=<id>
   source=<typed|env|keychain>` al resolver las entradas (UC-3 afirma sobre ella qué configuración
   usó la prueba) y `PROBE-DONE ok=<bool>` al terminar (UC-4 la necesita para establecer el orden
   real de terminación). La región se registra ya **normalizada**, que es la forma validada.
9. `internal/app/provider_test.go` — `TestUnportedProviderIsReported` usa `elevenlabs` como motor no
   portado (`provider_test.go:77-83`), pero ElevenLabs **está** portado desde la sesión anterior
   (`dictation.go:325`, `config.go:394`): el test pasa por el `ErrNoSecret` que le da `testDictation`,
   no por llegar al `default` de "no portado". Es exactamente el tipo de test vacuo que esta sesión
   anterior encontró cuatro veces (`CONTINUITY.md:79-84`). Se arregla aquí, con el motor desconocido
   correcto y afirmando el mensaje, porque lo encontró esta revisión — no se aparca.

## Tests

Go, con la red y el Keychain en seams (`internal/app`):

| # | Test | Afirma |
|---|------|--------|
| 1 | slot desconocido | error, `OK=false`, cero HTTP |
| 2 | slot `azure-openai` (conocido, no portado) | "prueba no disponible", cero HTTP, cero `getSecret` |
| 2b | slot `grok` (conocido, disponible, **sin probe**) | "prueba no disponible", cero HTTP, cero `getSecret` — no puede caer al probe de Azure |
| 3 | sin clave escrita, Keychain vacío (`ErrNoSecret`) | "falta la clave", cero HTTP |
| 4 | sin clave escrita, Keychain con timeout (`ErrKeychainTimeout`) | dice que el Keychain no respondió, **no** "falta la clave", cero HTTP |
| 5 | sin clave escrita, Keychain con otro error | mensaje de lectura fallida, cero HTTP |
| 6 | sin región escrita ni guardada | pide región, **cero HTTP y cero lecturas de `getSecret`** |
| 7 | región guardada inválida y argumento vacío | la rechaza `NormalizeRegion`, **cero HTTP y cero lecturas de `getSecret`**: una región que no sirve no justifica tocar el Keychain (ni pagar sus 3 s) |
| 8 | clave escrita + región + 200 | `OK=true`, `Message` no vacío |
| 9 | 401 | error de credencial, `OK=false` |
| 10 | campo vacío + clave guardada | usa la del Keychain (seam), llega a la red |
| 11 | `LOQUI_AZURE_KEY` puesta | gana sobre el Keychain, igual que `keyReaderFor` |
| 12 | `LOQUI_AZURE_KEY=""` | **no** cuenta como override: cae al Keychain |
| 13 | `LOQUI_AZURE_KEY="   "` | cuenta como override (es lo que hace `keyReaderFor` hoy) y el mensaje **nombra la variable**, para que nadie busque en el Keychain una clave que viene del entorno |
| 14 | Doer que bloquea hasta `ctx.Done()` | el deadline se respeta y el resultado lo dice |
| 14b | `getSecret` que tarda un tiempo controlado, y un Doer que mide cuánto contexto le queda | el presupuesto HTTP **nace después** del preflight: el deadline que ve el Doer no está mordido por lo que costó leer la clave |
| 15 | el secreto no aparece en `ProbeResult` | ningún campo del resultado lo contiene |
| 16 | `SaveConnection` con clave / con región / con ambas | tres notices distintos, `Error` vacío |
| 17 | `DeleteKey` sobre slot con clave y sobre slot vacío | el mismo notice de postcondición en ambos, y es verdad en ambos |
| 18 | `SetProvider` a motor configurado / sin configurar / no soportado en esta máquina (seam `caps`) | tres notices distintos y los dos últimos avisan |
| 18b | `SetProvider` a un motor cuya clave está `unreadable` (seam `keyStatus`) | el notice dice que el Keychain no respondió, **no** "falta configuración" — es la P1 de la iteración 2 fijada con test |
| 20 | `SaveConnection` sin clave por ningún lado | rechazo con `Field="key"`, nada escrito |
| 21 | `SaveConnection` solo-región con clave guardada, y con `LOQUI_AZURE_KEY` | los dos siguen siendo válidos (las regresiones de `settings_write_test.go:379` y `:554` no se rompen) |
| 22 | `SaveConnection` sin clave y con el Keychain ilegible | mensaje de lectura fallida, `Field` vacío, no dice "falta la clave" |
| 23 | `Payload().Revision` | crece en cada llamada **y ordena por inicio, no por final**: un snapshot que empieza antes y tarda más sale con revisión menor. Que `paint()` lo descarte es frontend y no lo cubre este test — ver el hueco declarado |
| 19 | probe con payload: escritura que agota plazo y aterriza tarde, luego probe con campo vacío | el `ProbeResult.Payload` **contiene** la clave ya presente — y solo eso: es un test de Go y no puede afirmar nada sobre lo que la página haga con él |

**Cada test nuevo se verifica rompiendo a propósito lo que dice cubrir** (`CONTINUITY.md:79-84`: en
la sesión anterior cuatro tests en verde no probaban nada, y los encontró mutar producción).

El frontend no tiene runner ni comprobación de tipos (deuda declarada, `CONTINUITY.md:31-33`), así
que el arreglo 1b (epoch + `await writes`) y la CSS se verifican por E2E contra la app empaquetada,
no por unit test. Para que esa verificación no dependa de lo que conteste Azure, **el probe registra
en el log de Go qué región y qué origen de clave resolvió** — `PROBE region=<id>
source=<typed|env|keychain>`, nunca el secreto, igual que el resto de sondas
(`CONTINUITY.md:71-77`). Sin esa línea, "usó la clave nueva" no es observable desde fuera.

## E2E Use Cases

#### Surface coverage decision

Interfaces que expone el proyecto: **UI** (única superficie de Ajustes).

- **UI** — Covered (UC-1, UC-2, UC-3, UC-4).
- **API** — N/A: la app no expone HTTP; los bindings de Wails no son una superficie de usuario, solo
  el transporte de esta misma UI.
- **CLI** — N/A: `cmd/stt-probe` dicta desde la terminal para aislar fallos de red y no configura
  motores; vincular un motor no es una capacidad que la CLI ofrezca hoy ni deba ofrecer (la clave se
  guarda en el Keychain del usuario desde la app firmada).

#### UC-1 — probar la clave antes de confiar en ella

- **Actor:** persona que acaba de sacar una clave de Azure Speech del portal y aún no sabe si la
  copió entera.
- **Scenario:** tiene el card de Azure abierto en Ajustes con la región elegida y la clave pegada.
  Quiere saber si sirve **antes** de guardarla y descubrirlo a mitad de un dictado.
- **Interface:** UI
- **Intent:** comprobar que la clave y la región que acaba de escribir sirven, y que se lo digan con
  palabras.
- **Setup:** abrir la app empaquetada con la configuración que ya hay en la máquina (clave de Azure
  guardada y su región). No se toca la clave desde fuera de la app.
- **Steps:** Ajustes → Conexiones → Configurar en Azure → pulsar "Probar conexión" con el campo de
  clave **vacío** (usa la guardada) → volver a pulsarlo con el sentinel inválido en el campo
  (`LOQUI_DEBUG_CONN_CLICK=azure:test:badkey`).
- **Verification:** la primera vez la línea de estado del card dice que la conexión es correcta, en
  verde; la segunda dice que la clave o la región no valen, en rojo. En ninguno de los dos casos el
  botón desaparece ni queda muerto: se puede volver a pulsar.
- **Persistence:** tras la prueba fallida, cerrar y volver a abrir Ajustes → la clave guardada sigue
  siendo la buena y el motor sigue activo: **probar no escribe nada**.

#### UC-2 — el guardado y la selección se dicen

- **Actor:** la misma persona, ya con la clave validada.
- **Scenario:** guarda la clave y elige Azure como motor. Antes esto no producía ningún cambio
  visible en el card y parecía que el botón estaba roto.
- **Interface:** UI
- **Intent:** guardar la clave y activar el motor, y ver confirmado cada paso.
- **Setup:** app empaquetada, card de Azure abierto, motor activo distinto de Azure.
- **Steps:** Guardar con el campo de clave vacío y la región puesta (guarda región) → pulsar "Usar
  este motor".
- **Verification:** tras Guardar, la línea dice en verde qué se guardó exactamente; tras "Usar este
  motor", dice que Azure queda activo y la insignia pasa a "Activo". "Borrar clave", que ahora se ve
  apagado cuando lo está, queda encendido.
- **Persistence:** cerrar y abrir la ventana de Ajustes → Azure sigue activo con su clave, y "Borrar
  clave" sigue encendido.

#### UC-3 — una prueba lanzada justo después de guardar no lee la configuración vieja

- **Actor:** la misma persona, corrigiendo la región porque copió la equivocada.
- **Scenario:** cambia la región y, sin esperar, pulsa "Probar conexión" con el campo de clave
  vacío. Espera que la prueba compruebe **lo que acaba de guardar**, no lo anterior.
- **Interface:** UI
- **Intent:** que probar justo después de guardar mida la configuración nueva.
- **Setup:** app empaquetada con la clave de Azure ya guardada y su región actual. Se anota la
  región actual para restaurarla al final.
- **Steps:** `LOQUI_DEBUG_CONN_CLICK=azure:save-region:westeurope+clear-region+test` — elige otra
  región válida y pulsa Guardar; **sin esperar**, deja el desplegable en el placeholder vacío y
  pulsa "Probar conexión" con el campo de clave también vacío.
- **Verification:** el log de Go muestra `PROBE region=westeurope source=keychain` — y eso solo
  puede venir del store, porque el formulario tenía la región vacía en el momento del clic. La línea
  de estado del card acaba mostrando el resultado de la prueba, y la insignia y el botón Borrar
  quedan como corresponde al estado guardado (el payload del Guardar sí se pintó).

> **Por qué el `clear-region` no es adorno** (hallazgo de Codex, iteración 3): sin él, el handler
> captura `westeurope` **del DOM** en el clic, así que el log diría `region=westeurope` aunque se
> borrara por completo el `await writes`. El UC no probaría nada. Vaciando el desplegable antes de
> pulsar, la única forma de que el probe conozca esa región es haber esperado a que la escritura se
> drene.
- **Persistence:** restaurar la región original por el mismo camino (elegir en el desplegable y
  Guardar) → cerrar y abrir Ajustes → la región original sigue puesta y el motor sigue activo.

> La discriminación **no depende de lo que conteste Azure**: la afirma la línea `PROBE region=…`. Si
> además el 401 aparece por la región equivocada, es confirmación extra, no la evidencia.

#### UC-4 — un resultado viejo no pisa el mensaje de una acción nueva

- **Actor:** la misma persona, impaciente: pulsa Probar y, mientras gira, pulsa Guardar.
- **Scenario:** la prueba tarda un viaje de red; el Guardar termina antes. Lo que quede escrito
  tiene que describir lo último que hizo, no lo primero.
- **Interface:** UI
- **Intent:** que la línea de estado describa la última acción, y que el card no se quede obsoleto.
- **Setup:** app empaquetada, card de Azure abierto, clave guardada.
- **Steps:** `LOQUI_DEBUG_CONN_CLICK=azure:test+save` — pulsa "Probar conexión" e inmediatamente
  después "Guardar".
- **Verification:** los timestamps del log establecen el orden real de terminación —
  `PROBE-DONE ok=<bool>` cuando la prueba resuelve, y el `UI-ACTION` del Guardar cuando resuelve la
  escritura. Con `PROBE-DONE` **después** del `UI-ACTION` del Guardar, la línea de estado tiene que
  mostrar el notice del Guardar: el resultado tardío se descartó. La insignia, el estado de la clave
  y el botón Borrar corresponden al payload de ese Guardar.
- **Persistence:** cerrar y abrir Ajustes → lo guardado sigue ahí, y el card lo refleja igual que
  antes de recargar.

> **Sin `PROBE-DONE` este UC no prueba nada** (hallazgo de Codex, iteración 3): `PROBE region/source`
> se emite al **resolver las entradas**, no al terminar, y el `UI-ACTION` de hoy solo lo emite el
> wrapper de escrituras (`settings.ts:627-631`). Sin una marca de finalización del probe, "acabó
> mostrando el notice del Guardar" no distingue "el probe terminó antes" de "terminó después y el
> epoch lo descartó" — que es justo lo que se quiere demostrar.
>
> **Honestidad sobre el determinismo:** quién gana la carrera lo decide un viaje de red real. El UC
> no fuerza el orden; lo **observa** en el log y solo entonces afirma. Si en una ejecución el probe
> termina primero, esa ejecución no dice nada sobre el epoch y se repite.

## Huecos declarados, no tapados

**El arbitraje por revisión DENTRO de `paint()` no queda cubierto.** Los tests de Go fijan que las
revisiones crecen y que ordenan por inicio; que la página descarte la menor es lógica de frontend, sin
runner (`CONTINUITY.md:31-33`) y sin forma determinista de provocar dos payloads cruzados desde fuera.

**Que la PÁGINA aplique el payload del probe no queda cubierto por ninguna prueba.** El test 19
demuestra que el payload trae el estado fresco; UC-3 y UC-4 terminan con un payload de Guardar que
ya deja el card correcto, así que **borrar `paint(res.payload)` del handler del probe no los
rompería**. Cubrirlo de verdad exige partir de un card obsoleto, y su única causa real es una
escritura de Keychain que agota su plazo y aterriza tarde — que no se puede provocar a voluntad sin
meter en producción un hook que finja el timeout de una escritura de credenciales. No lo vale.
Queda anotado aquí y en `.workflow/state.md`, no disimulado (`CONTINUITY.md` lleva su propia sección
de huecos declarados por la misma razón).

## Lo que NO entra

- **Rechazar** en `SetProvider` un motor que esta máquina no soporta (ver arreglo 4). Se informa; no
  se cambia qué se acepta.
- Subir `typescript` para que el frontend compruebe tipos (deuda declarada sin dueño,
  `CONTINUITY.md:31-33`). Este cambio añade ~60 líneas de TS sin red de tipos, igual que las ~1500
  que ya hay. Va a su propio cambio; lo digo, no lo tapo.
- Los campos del subservicio Azure OpenAI (`azureOpenAiResource`, `azureOpenAiDeployment`), que
  siguen inertes porque el subservicio no está portado.
- Notices en los otros nueve setters (ver el alcance declarado, con las cuentas, en el arreglo 3).
- Que "Usar este motor" siga visible en el motor activo: hoy se oculta a propósito, igual que el
  original. El usuario preguntó por ese botón creyendo que era "Probar conexión"; una vez aclarado,
  no pidió cambiarlo.
