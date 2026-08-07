# Use cases — la tarjeta de credenciales: plegar al guardar, enmascarar lo guardado

Interface: **UI** (Conexiones, en Ajustes). No hay journey de Playwright: es un WKWebView de Wails,
sin origen servido. Driver: `LOQUI_DEBUG_CONN_CLICK` (clics reales sobre los handlers del ratón) y
`LOQUI_DEBUG_CONN_REPORT` (el estado en que la tarjeta ASIENTA, 6 s después).

**Antes de re-ejecutar, tres cosas que cuestan una corrida cada una:**

- Lanzar **sin sandbox**, o la app imprime su línea de plataforma y calla.
- **Llaves**: `"${p}:test"`, nunca `"$p:test"` — zsh se come `:t`.
- **UC-MASK-06 destruye una credencial.** Respaldar `secrets.json` antes y restaurarlo después.

Los tres observables son **clasificaciones, jamás valores** — estos reportes se escriben al log tal
cual: `keyField` (`empty|masked|typed`), `formOpen`, y `KEY-SENT kind`
(`typed|empty|masked-blocked`).

Última corrida: 2026-08-07 — los ocho PASS.
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

## UC-MASK-03 — guardo bien y la tarjeta se pliega sola

- **Setup:** Azure, cuyo guardado con la máscara intacta **sí** escribe la región — así el camino de
  éxito se ejercita sin escribir ninguna credencial.
- **Steps:** `LOQUI_DEBUG_CONN_CLICK="azure:save" LOQUI_DEBUG_CONN_REPORT="azure"`.
- **Verification:** `notice:Región guardada ok:true`, y `formOpen` pasa de **true a false**. El ✓ se
  ve primero: plegar de inmediato se lo llevaría por delante.
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
  - clave: `LOQUI_DEBUG_CONN_CLICK="${p}:test+${p}:set-key:otra-clave-distinta"`
  - región: `LOQUI_DEBUG_CONN_CLICK="azure:test+azure:set-region:westeurope"`
- **Verification:** aunque `PROBE-DONE ok=true`, la página muestra **"El formulario cambió durante la
  prueba — vuelve a probar"** con `statusClass:status` — ni ✓ ni ✗, porque no se probó ni se refutó
  nada sobre lo que hay ahora en pantalla. Y se DICE: una línea vacía es indistinguible de un clic
  que no llegó.
- **Persistence:** la región en disco no se mueve, y la elección sin guardar sobrevive en pantalla.

---

## Lo que todavía no es caso de uso

**Que el plegado de 1.2 s se cancele al teclear o al reabrir.** Las cancelaciones están cableadas
(`beforeinput`, `change` de región, el toggle, `beginAction`), pero provocar la carrera desde fuera
exige un paso que dispare *dentro* de esa ventana, y la gramática `+` ejecuta sus pasos de inmediato.
Es la garantía más débil de este cambio: hoy descansa en leer el código.
