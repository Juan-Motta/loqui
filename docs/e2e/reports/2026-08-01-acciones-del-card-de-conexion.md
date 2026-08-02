# E2E — las acciones del card de conexión (Azure)

VERDICT: PARTIAL

**PARTIAL a propósito, y esto es lo único que falta:** el camino verde de "Probar conexión" —
`✓ Conexión correcta` — no se ha ejecutado, porque en esta máquina no hay ninguna clave de Azure
válida. La borró el propio bug que este cambio arregla: el usuario pulsó "Borrar clave" el
2026-08-01, la llamada tuvo éxito y no dijo nada, y el ítem `azure-speech` desapareció del Keychain
(comprobado con `security find-generic-password -s com.jualopezmo.loquigo`). Poner PASS afirmando un
recorrido que nadie ha hecho sería firmar como probado lo que no lo está.

Todo lo demás está verificado contra `bin/loqui.app` empaquetada y firmada ad-hoc, con las salidas
de las sondas pegadas literalmente.

- **Fecha:** 2026-08-01
- **Build:** `bin/loqui.app`, `provider=azure`, `region=eastus`, slot `azure-speech` **vacío**
- **Interfaz:** UI (única superficie de Ajustes)
- **Cómo se dirige:** `LOQUI_DEBUG_CONN_CLICK` / `LOQUI_DEBUG_CONN_REPORT` — un botón dentro de un
  webview de Wails no se puede clicar desde un script, así que las sondas despachan clics REALES
  sobre los mismos handlers que alcanza el ratón. El informe del card se toma 6 s después, para
  medir el estado en el que QUEDA y no el "…" intermedio.

---

## UC-5 — guardar sin clave: PASS

Pedido por el usuario el 2026-08-01: "si no hay api key configurada, al dar click en guardar se
debería resaltar el campo de key en rojo y un mensaje indicando que la key es requerida".

```
CONN-CLICK  ran:save
UI-ACTION   map[action:saveConnection(azure) error:la clave es obligatoria: pégala antes de guardar
            field:key notice: ok:false]
CONN-CARD   map[card:map[badge:Sin configurar  invalid:key
            status:✗ la clave es obligatoria: pégala antes de guardar  statusClass:status err
            test:shown/enabled  use:shown/disabled  delete:shown/disabled  save:shown/enabled]]
```

- El campo señalado es el correcto: `invalid:key` — la clase la pone la página desde
  `WriteResult.Field`, que decide Go.
- El mensaje queda escrito y en rojo (`statusClass:status err`), no se borra.
- **Nada se escribió**: la región no se movió.

## UC-1a — probar sin clave: PASS

```
CONN-CLICK  ran:test(nokey)   status:Probando la conexión…   test:shown/disabled
UI-PROBE    map[error:falta la clave: escríbela o guárdala antes de probar field:key ok:false]
CONN-CARD   invalid:key   status:✗ falta la clave: escríbela o guárdala antes de probar
            statusClass:status err   test:shown/enabled
```

- **No hay línea `PROBE`**, que es la que el probe emite al resolver las entradas: la validación
  cortó antes de salir a la red, como exige el plan.
- El botón se deshabilita mientras vuela y vuelve a estar disponible después. Nunca desaparece.

## UC-1b — probar con una clave inválida, contra Azure de verdad: PASS

Esta es la que recorre la cadena entera: página → binding → resolución de región y clave → HTTPS
real al endpoint STS de `eastus` → clasificación del 401 → mensaje.

```
CONN-CLICK  ran:test(badkey)
PROBE       slot=azure-speech region=eastus source=typed
PROBE-DONE  slot=azure-speech ok=false
UI-PROBE    map[error:Clave o región inválida (401/403) field: ok:false provider:azure]
CONN-CARD   status:✗ Clave o región inválida (401/403)  statusClass:status err  test:shown/enabled
```

- `source=typed` confirma que usó la clave del formulario (el sentinel inválido fijo
  `loqui-debug-clave-invalida`, nunca una credencial real del entorno).
- El secreto no aparece en ninguna línea del log.
- `field:` vacío: una credencial rechazada por Azure no es un campo mal rellenado, así que no se
  pinta ningún borde rojo.

## UC-2 — la matriz de botones por estado: PASS

Azure sin configurar, en las tres capturas de arriba:

| Botón | Estado | Correcto porque |
|---|---|---|
| Probar conexión | `shown/enabled` | se usa precisamente cuando el motor NO está bien configurado |
| Usar este motor | `shown/disabled` | pedido por el usuario: solo con conexión guardada |
| Borrar clave | `shown/disabled` | no hay clave que borrar |
| Guardar | `shown/enabled` | es la acción que arregla el estado |

Y los motores locales, que la regla nueva no debe romper:

```
CONN      rows:whisper=available macos=available azure=unconfigured openai=unconfigured ...
CONN-CARD map[card:map[provider:whisper badge:Disponible badgeClass:conn-state available
          use:shown/enabled  test:absent  delete:absent  save:absent]]
```

`available` es el estado de Whisper y macOS, que no llevan credencial: siguen seleccionables.
Tratar "sin clave" como "sin configurar" los habría dejado inservibles.

## Disposición del mensaje: PASS (verificado por captura)

El usuario reportó que el mensaje "no se ve bien" al lado de los botones: `.conn-actions` era un
flex row y el estado quedaba en una columna de ~60 px partiendo una o dos palabras por línea. Ahora
ocupa una fila propia encima de los botones, alineado a la izquierda, y cuando está vacío no reserva
espacio. Comprobado con captura de pantalla de la app empaquetada, no por inspección del CSS.

## UC-6 — una prueba no toca el formulario: PASS

Hallazgo de la revisión de código (iteración 4): `paint()` rellena el selector de región desde lo
GUARDADO, así que probar una región antes de guardarla la devolvía en silencio a la anterior — y el
siguiente Guardar habría persistido la clave contra una región distinta de la que se probó. Es la
misma trampa que ya se había corregido para la clave.

```
CONN-CLICK  ran:set-region(westeurope) | test(badkey)
PROBE       slot=azure-speech region=westeurope source=typed
UI-PROBE    map[error:Clave o región inválida (401/403) field: ok:false provider:azure]
CONN-CARD   map[card:map[... provider:azure region:westeurope ...]]
```

Y en disco, después: `region = eastus`.

- La prueba usó la región **del formulario** (`region=westeurope`), no la guardada.
- Tras el repintado, el selector **sigue** en `westeurope`: la elección sin guardar sobrevive.
- El disco no se movió: un probe no escribe nada.

---

## Lo que este informe NO cubre

1. **`✓ Conexión correcta`** — necesita una clave de Azure válida. Ver la cabecera.
2. **`✓ Clave guardada` y `✓ Motor activo: Azure`** — el camino de éxito de Guardar y de "Usar este
   motor" tampoco se ha ejecutado, por lo mismo: sin clave válida no se llega a `connected`. El
   texto está fijado por tests de Go (`TestASuccessfulWriteCarriesSomethingToSay`,
   `TestTheSaveNoticeNamesWhatWasActuallyWritten`), lo cual es una afirmación distinta y más débil
   que haberlo visto en pantalla.
3. **UC-3 y UC-4 (concurrencia)** — `await writes` y el arbitraje del epoch/revisión. Requieren una
   configuración válida para que las dos acciones encadenadas tengan sentido, así que van con el
   siguiente informe.
4. **Que la página aplique el payload del probe** — hueco declarado en el plan: su única causa real
   es una escritura de Keychain que agota su plazo y aterriza tarde, y provocarla a voluntad exigiría
   meter en producción un hook que finja el timeout de una escritura de credenciales.
