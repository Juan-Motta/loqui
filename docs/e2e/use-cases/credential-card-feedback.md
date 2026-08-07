# Use cases — la tarjeta de credenciales: plegar al guardar, enmascarar lo guardado

Interface: **UI** (Conexiones, en Ajustes). No hay journey de Playwright: es un WKWebView de Wails,
sin origen servido. Driver: `LOQUI_DEBUG_CONN_CLICK` (clics reales sobre los handlers del ratón) y
`LOQUI_DEBUG_CONN_REPORT` (el estado en que la tarjeta ASIENTA, 6 s después).

**Antes de re-ejecutar, tres cosas que cuestan una corrida cada una:**

- Lanzar **sin sandbox**, o la app imprime su línea de plataforma y calla.
- **Llaves**: `"${p}:test"`, nunca `"$p:test"` — zsh se come `:t`.
- **UC-MASK-06 destruye una credencial.** Respaldar `secrets.json` antes y restaurarlo después.

Los observables son **clasificaciones, jamás valores** — estos reportes se escriben al log tal cual:
`keyField` (`empty|masked|revealed|typed`), `keyVisible`, `formOpen`, el estado de cada botón
(`shown/disabled/busy`), y `KEY-SENT kind` (`typed|empty|masked-blocked|revealed`).

**`set-key` sólo acepta tokens fijos** — `badkey`, `other`, `empty` — nunca texto libre. Pasarle una
clave real la escribiría en el log, que es justo lo que el afordance existe para evitar.

Última corrida: 2026-08-07 — los quince PASS.
Informe: `docs/e2e/reports/2026-08-07-credential-card-feedback.md`.

---

## UC-MASK-01 — abro una tarjeta cuya clave guardé antes

- **Actor:** alguien que vuelve a un proveedor que ya configuró.
- **Intent:** ver que **hay** una credencial, sin que la app la enseñe nunca.
- **Setup:** una clave guardada en la ranura. No pulsar nada.
- **Steps:** `LOQUI_DEBUG_CONN_REPORT="${p}"` y leer la tarjeta.
- **Verification:** `keyField:masked`. Lo que hay dentro es una constante fija de doce caracteres, no
  la clave ni nada derivado de ella — **su longitud tampoco dice nada de la real**.
- **Persistence:** ninguna; abrir no escribe.

## UC-MASK-02 — pulso Guardar sin tocar el campo enmascarado

**El caso para el que existe el guardarraíl.** Si la máscara se enviara, doce asteriscos
sustituirían una credencial que funciona.

- **Setup:** clave guardada, campo enmascarado por la página, sin tocarlo. Anotar el sha256 de
  `secrets.json`.
- **Steps:** `LOQUI_DEBUG_CONN_CLICK="${p}:save"` sobre un proveedor **sin región** (openai, grok,
  elevenlabs).
- **Verification:** `KEY-SENT kind:masked-blocked`; `error:no hay nada que guardar`; y sobre todo
  **`secrets.json` byte-idéntico**. Además `formOpen:true` — la escritura falló, así que la tarjeta
  NO se pliega: el mensaje y el borde rojo viven dentro del formulario.
- **Persistence:** el hash del fichero de credenciales es la aserción principal, no un extra.

## UC-MASK-03 — pulso Guardar: el botón gira, y al terminar la tarjeta se pliega

- **Setup:** Azure, cuyo guardado con la máscara intacta **sí** escribe la región — así el camino de
  éxito se ejercita sin escribir ninguna credencial.
- **Steps:** `LOQUI_DEBUG_CONN_CLICK="azure:save" LOQUI_DEBUG_CONN_REPORT="azure"`.
- **Verification:** en vuelo `save:shown/disabled/busy`; al aterrizar `ok:true`, `formOpen` pasa de
  **true a false** y el botón vuelve a `shown/enabled`. El plegado es **inmediato**: el spinner ya dio
  el aviso mientras duraba, así que no hay nada que esperar después.
- **Persistence:** `secrets.json` sin cambios.
- **Ojo:** Azure NO contesta "no hay nada que guardar" — la página siempre manda la región y
  `SaveConnection` la escribe. Es correcto; el plan lo afirmaba mal y se corrigió.

## UC-MASK-04 — la máscara no pisa lo que estoy escribiendo

`paint()` corre tras **cada** escritura de la ventana (Sistema, idiomas, onboarding, permisos), así
que una máscara incondicional borraría una clave a medio pegar.

- **Steps:** `LOQUI_DEBUG_CONN_CLICK="${p}:test:badkey" LOQUI_DEBUG_CONN_REPORT="${p}"`.
- **Verification:** `keyField:typed` **después** del repintado del probe. Sólo se enmascara un campo
  vacío.

## UC-MASK-05 — el driver de debug no engaña a su propio test

- **Intent:** que el afordance escriba como una tecla, no asignando `.value`.
- **Verification:** `KEY-SENT kind:typed`, `PROBE source=typed`, `ok=false code=invalid_api_key`.
- **Por qué importa:** con la asignación directa la marca de máscara sobrevivía, el guardarraíl
  mandaba vacío, Go probaba **la clave buena guardada**, y el caso negativo salía `ok=true`. Un E2E
  demostrando lo contrario de lo que afirma.

## UC-MASK-06 — borrar la clave quita la máscara *(destructivo)*

- **Setup:** **respaldar `secrets.json`.** Clave guardada y campo enmascarado.
- **Steps:** `LOQUI_DEBUG_CONN_CLICK="${p}:delete" LOQUI_DEBUG_CONN_REPORT="${p}"`, luego restaurar.
- **Verification:** `keyField:empty`, `keyState:(no configurada)`, `badge:Sin configurar`. Sin esto
  la máscara sobrevive al borrado y la tarjeta afirma una credencial que ya no existe.
- **Persistence:** restaurar y comprobar que el sha256 vuelve al de partida.

## UC-MASK-07 / 08 — un ✓ nunca aparece junto a algo que nadie probó

- **Intent:** un veredicto es sobre las entradas con las que se pidió. El formulario puede moverse
  mientras la red trabaja, y con la máscara el campo puede contener por diseño algo que no es la clave.
- **Steps:**
  - clave: `LOQUI_DEBUG_CONN_CLICK="${p}:test+${p}:set-key:other"`
  - región: `LOQUI_DEBUG_CONN_CLICK="azure:test+azure:set-region:westeurope"`
- **Verification:** aunque `PROBE-DONE ok=true`, la página muestra **"El formulario cambió durante la
  prueba — vuelve a probar"** con `statusClass:status` — ni ✓ ni ✗, porque no se probó ni se refutó
  nada sobre lo que hay ahora en pantalla. Y se DICE: una línea vacía es indistinguible de un clic
  que no llegó.
- **Persistence:** la región en disco no se mueve, y la elección sin guardar sobrevive en pantalla.

## UC-EYE-01 — pulso el ojo y veo mi clave

- **Intent:** el único sitio donde el secreto cruza a la página. Se pide al pulsar, jamás en un payload.
- **Steps:** `LOQUI_DEBUG_CONN_CLICK="${p}:eye" LOQUI_DEBUG_CONN_REPORT="${p}"`.
- **Verification:** `REVEAL slot=… ok=true` (el ACTO, nunca el valor), `keyField:revealed`,
  `keyVisible:true`. **Y la aserción que de verdad importa:** escanear el log entero buscando
  cualquier ventana de 8 caracteres de las claves reales — cero.

## UC-EYE-01b — el segundo clic OCULTA, no vuelve a pedirla

- **Steps:** `"${p}:eye+wait:1500+${p}:eye"`.
- **Verification:** **una sola** llamada `REVEAL` en toda la corrida, y termina `keyField=masked`.
  Dos llamadas significan que ha vuelto el fallo del `blur`: pulsar el ojo con el campo enfocado
  disparaba primero el blur, que re-enmascara, y el clic veía "masked" y volvía a revelar.

## UC-EYE-02 — vuelve a ocultarse por el ojo y por apartar la vista

- **Steps:** `"${p}:eye+wait:1500+${p}:eye"` y `"${p}:eye+wait:1500+${p}:blur-key"`.
- **Verification:** `keyField=masked` en ambos.
- El tercer camino, el temporizador de 15 s, **no se ejecuta** aquí: ver abajo.

## UC-EYE-03 — el ojo está muerto donde la app no guarda nada

- **Steps:** comparar una ranura con clave guardada contra una mandada por `LOQUI_GROK_KEY`.
- **Verification:** `eye=shown/enabled` frente a `eye=shown/disabled`, y la etiqueta explicando por
  qué. Deshabilitado y no oculto: un control que desaparece se lee como un fallo de pintado.

## UC-EYE-05 — una clave revelada y EDITADA no se queda legible

- **Steps:** `"${p}:eye+wait:1200+${p}:set-key:other+wait:300+${p}:blur-key"`.
- **Verification:** `keyField=typed` **y `keyVisible=false`**. Hacen falta los dos: por procedencia
  es "typed" en ambos casos, y sólo `keyVisible` distingue el fallo — la primera versión dejaba
  `type="text"` con todas las vías de vuelta apagadas, y la credencial se quedaba a la vista.

---

## Lo que todavía no es caso de uso

**El auto-ocultado a los 15 s.** Está cableado y `wait` podría ejercitarlo, pero una pausa de 15 s
dentro de una corrida cuyo reporte se toma a los 6 s exige rehacer el retardo del reporter. Los otros
dos caminos de re-ocultado SÍ se ejecutan (UC-EYE-02), así que lo que descansa en leer el código es el
temporizador, no el re-ocultado que dispara.

**Las otras dos cancelaciones** del plegado —reabrir la tarjeta y cambiar la región— dejaron de existir
con el plegado inmediato. La del tecleo se ejecutó contra el diseño de 1.2 s y pasó; ver UC-EYE-04 en
el informe.
