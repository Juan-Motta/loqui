# Plan — feedback al guardar una credencial: plegar y enmascarar

- **Date:** 2026-08-07
- **Asked for by the user** on 2026-08-07: "una vez se guardan unas credenciales, me gustaría que el
  acordeón se cierre, para que el usuario pueda ver que se configuró correctamente; y si llega a abrir
  el acordeón con unas credenciales ya guardadas, que en el input se vieran los datos de manera
  enmascarada, sólo asteriscos, de tal manera que se pueda saber que existen ya unos datos".

## El problema, dicho como lo vive el usuario

Pegas una clave, das a Guardar, y **la tarjeta se queda exactamente igual**: el formulario sigue
abierto y el campo se vacía. El vaciado es deliberado (el secreto no debe quedarse en el DOM), pero
el resultado es que la única confirmación es una línea de texto, y el campo vacío se lee como "no se
guardó nada". Al volver más tarde, la tarjeta abierta vuelve a mostrar un campo vacío: no hay forma
de saber, mirando el formulario, si hay o no una credencial.

## Goal

Que el estado "ya hay una clave aquí" sea **visible en el formulario**, y que guardar **se sienta**
como que pasó algo.

## Decisiones ya tomadas con el usuario

1. **Al guardar bien: mostrar `✓ Clave guardada` ~1.2 s y ENTONCES plegar.** No plegar de inmediato:
   la línea de estado vive dentro de `.conn-form`, así que cerrar de golpe se lleva la confirmación
   por delante y deja como única señal el badge de la fila. Con el retardo se tienen las dos cosas.
2. **La máscara se pinta como el valor del campo**, no como `placeholder`. El usuario pidió "que se
   vieran los datos"; un placeholder gris se lee como una pista, no como contenido.

## La restricción que manda sobre todo lo demás

**El secreto nunca sale de Go.** `KeyState` (`internal/app/bootstrap.go:29`) lleva `status`,
`fromEnv` y `available`, jamás el valor — y el branch que acaba de cerrarse arregló dos fugas de
credencial. Así que:

> La máscara es una **constante de longitud fija**. No es la clave, no deriva de la clave, y su
> longitud no dice nada de la longitud real — que también es información.

De ahí sale el requisito central: **la máscara jamás puede enviarse como secreto**. Si se enviara,
Guardar sobrescribiría la clave buena con doce asteriscos.

## Lo que el backend ya hace, y por eso no hay que tocarlo

Medido, no supuesto:

| Necesidad | Ya resuelto en | Comportamiento |
| --- | --- | --- |
| "Guardar sin cambiar la clave" | `settings_write.go:466` | secreto vacío ⇒ deja la guardada en paz |
| "Probar la clave guardada" | `settings_probe.go:242` | typed vacío ⇒ `source=stored` |
| "No hay nada que guardar" | `settings_write.go:514` | ni región ni clave ⇒ error explícito |

Así que la máscara no necesita un canal nuevo: basta con **mandar cadena vacía** cuando el campo
sigue enmascarado y sin tocar. El backend ya interpreta eso correctamente en los dos caminos.

## Design

### 1. La regla va en Go: `KeyState.Stored`

Nuevo campo booleano, `Stored = Status == KeyPresent && !FromEnv`. Es la respuesta a "**esta app**
tiene aquí una credencial que ella misma guardó", que es exactamente la condición para enmascarar.

Va en Go y no en la página por dos razones, y la segunda es la que decide:

- El frontend **no tiene runner de tests** (`frontend/package.json` no declara `test`), así que una
  regla escrita en TS no se puede probar.
- `fromEnv` es la parte que puede **mentirle al usuario**. Una clave que viene de una variable de
  entorno no la guardó la app: enmascarar ahí diría "tengo tu clave" cuando no la tiene, y
  `bootstrap.go:41-44` ya documenta que el campo debe verse vacío precisamente en ese caso. Es la
  clase de regla que el principio del proyecto —las reglas en Go, la página no decide— existe para
  proteger.

**No se reutiliza para el botón Borrar**, aunque la condición se parezca: `deletable`
(`settings.ts:579`) usa la disponibilidad de la **tarjeta** (`state !== "unsupported"`), no la de la
ranura. Colapsarlos sería cambiar comportamiento a escondidas.

### 2. La máscara sólo cae sobre un campo vacío — el peligro real

`paint()` corre **después de cada escritura**, y no sólo de las de esta tarjeta: Sistema, idiomas,
onboarding y el refresco de permisos también repintan. Si la máscara se escribiera sin condición,
pisaría la clave que el usuario está tecleando en ese momento.

> Regla: se escribe la máscara **sólo si el campo está vacío o ya contiene la máscara**. Nunca sobre
> texto del usuario.

El campo se marca con `dataset.masked` para distinguir "esto es una máscara" de "esto lo escribió
alguien" — el valor por sí solo no basta, porque un usuario podría teclear asteriscos.

### 3. Se limpia al primer carácter, no al foco

`beforeinput`, no `focus`. Con `focus` la máscara desaparece en cuanto el usuario pincha el campo
para mirarlo, y se pierde justo la señal que se quería dar. Con `beforeinput` se borra el valor
entero **antes** de que aterrice el primer carácter, así que no se teclea "detrás" de la máscara.

### 4. Los dos guardarraíles: Guardar y Probar

Si el campo sigue enmascarado y sin tocar, ambos mandan **cadena vacía**:

- **Guardar** ⇒ el backend deja la clave en paz. Para un proveedor **sin región** eso significa que
  no queda nada que escribir y contesta "no hay nada que guardar", que es cierto. Para **Azure no**:
  la página siempre manda la región y `SaveConnection` la escribe cuando viene
  (`settings_write.go:489`), así que contesta "Región guardada" y pliega. Correcto — guardó algo.
- **Probar** ⇒ `source=stored`: prueba la clave guardada, que es lo que el usuario quiere decir al
  pulsar Probar sobre un campo enmascarado. Idéntico a lo que hoy hace un campo vacío.

**Y el guardarraíl tiene que ser observable, o no se puede probar que funcionó.** Mirar el campo
asentado no distingue "la máscara fue bloqueada" de "la máscara se guardó como clave": en los dos
casos el payload dice `present` y al reabrir se ve la misma máscara. Así que ambos handlers emiten
`ui:key-submitted` con la CLASIFICACIÓN de lo enviado — `typed`, `empty` o `masked-blocked` —
**nunca el valor**.

### 4b. Quitar la máscara cuando deja de haber clave

La transición inversa es tan obligatoria como la directa. Al borrar la clave el payload vuelve con
`Stored=false`; si nadie limpia el campo, la tarjeta sigue afirmando una credencial que ya no
existe. `paint()` limpia **sólo lo que pintó la propia página** (`dataset.masked`), nunca texto que
haya escrito el usuario.

### 5. Plegar sólo en éxito, y el plegado tiene que poder CANCELARSE

`run()` no expone hoy el resultado a quien lo llama. Se le añade un `onOk` opcional, invocado
únicamente cuando `res.error === ""` **y** `isCurrent(card, epoch)` — la misma guarda que decide
quién habla.

Plegar sólo en éxito no es un detalle: si la escritura falló, el borde rojo y el mensaje están
**dentro** del formulario, así que cerrarlo escondería la queja y el campo que hay que corregir.

**La guarda de época NO basta, y creer que sí era el error de la v1.** Reabrir la tarjeta sólo cambia
`hidden` (`settings.ts:877`) y teclear no llama a `beginAction` (`:657`): en ninguno de los dos casos
se mueve la época. Guardas, empiezas a escribir una clave nueva, y 1.2 s después el formulario se
cierra debajo de ti.

Así que el plegado pendiente es **cancelable**, uno por tarjeta, y lo cancelan:

- **el toggle** — si tocaste "Configurar", el estado del formulario lo mandas tú;
- **cualquier tecla** en el campo de clave o un cambio de región — estás escribiendo, no mirando;
- **cualquier acción nueva** sobre la tarjeta (`beginAction`), que ya es el punto por donde pasa todo.

Al disparar se vuelve a comprobar la época, que sigue haciendo falta para el caso que las
cancelaciones no cubren: una acción sobre OTRA tarjeta cuyo repintado supersede a ésta.

### 6. De paso: la carrera de éxito rancio (heredada)

Encontrada por la revisión cruzada del branch anterior y diferida a éste a propósito, porque éste
edita justo ese handler. Hoy: pulsas Probar, cambias el campo mientras vuela, y el `✓ Conexión
correcta` se pinta junto a una clave que nunca se probó (editar un input no avanza la época,
`settings.ts:657`).

Aquí importa **más** que antes: la máscara pone en el campo un valor que no es la clave, así que
"lo que se probó" y "lo que se ve" pueden divergir por diseño. El arreglo: `probe()` recuerda lo que
probó y, al volver, si el formulario ya no es ése, dice que el formulario cambió en vez de dar un ✓
sobre algo que nadie probó.

**Y compara la región, no sólo la clave.** El probe captura las dos (`settings.ts:894-898`). Sin
esto: pruebas Azure contra `eastus`, cambias el selector a `westus` mientras vuela, y sale
`✓ Conexión correcta` junto a `westus`. Es el mismo fallo que UC-6 de
`docs/e2e/reports/2026-08-01-connection-card-actions.md` ya cerró en la dirección contraria — allí se
estableció que el probe debe usar la región DEL FORMULARIO; aquí, que debe callarse si esa región
cambió.

### 7. El afordance de debug tiene que mentir menos que el usuario

`debugConnStep` asigna `key.value` directamente (`settings.ts:1183`), sin pasar por `beforeinput`.
Sobre una tarjeta ya enmascarada eso dejaría `dataset.masked` puesto: el guardarraíl mandaría vacío,
Go probaría la clave **buena** guardada, y el caso negativo del E2E saldría **verde**. El E2E habría
verificado exactamente lo contrario de lo que afirma.

El driver escribe por el mismo helper que una tecla real, que limpia la marca. No es cosmético: es la
diferencia entre una verificación y una que se engaña a sí misma.

## Review de diseño — codex, 2026-08-07, REWORK

1 P0, 3 P1, 3 P2. **Las siete verificadas contra el código antes de aceptarse; ninguna descartada.**
Tres cambian el diseño, no sólo la redacción.

**P0 — la telemetría del probe habría reintroducido la fuga.** La v1 decía que el reporte de tarjeta
llevara "el valor que el probe realmente usó". Los reportes se registran **verbatim**
(`settings.ts:1261` → `wiring.go:145`), así que eso habría escrito una clave real en el log — justo
lo que el branch anterior cerró dos veces. **Resuelto:** sólo una CLASIFICACIÓN, jamás un valor.
`keyField: empty|masked|typed`, y para lo enviado `kind: typed|empty|masked-blocked`.

**P1 — el temporizador SÍ podía disparar sobre el usuario.** La v1 afirmaba que la guarda de época
cubría la reapertura. Es falso: el toggle sólo cambia `hidden` (`settings.ts:877`) y teclear no llama
a `beginAction` (`:657`), así que la época no se mueve. Guardas, y 1.2 s después el formulario se
cierra mientras escribes. **Resuelto:** un plegado pendiente por tarjeta, **cancelable**, y lo
cancelan el toggle, cualquier tecla en la clave o la región, y cualquier acción nueva.

**P1 — la máscara sobrevivía al borrado.** La v1 decía cuándo poner la máscara y no decía nunca
cuándo quitarla. Borras la clave, el payload vuelve con `Stored=false`, y el campo **sigue
enmascarado**: la tarjeta afirma una credencial que ya no existe. **Resuelto:** la transición
inversa es explícita, y sólo limpia lo que pintó la propia página (`dataset.masked`), nunca texto
del usuario.

**P1 — el chequeo de prueba rancia se olvidaba de la región.** El probe captura clave **y** región
(`settings.ts:894`). Pruebas Azure contra `eastus`, cambias a `westus` mientras vuela, y sale
`✓ Conexión correcta` junto a `westus`, que nadie probó. **Resuelto:** se comparan las dos.

**P2 — nada podía demostrar que el guardarraíl funcionó.** Observar el campo asentado no distingue
"la máscara fue bloqueada" de "la máscara se guardó como clave": en ambos casos el payload dice
`present` y el campo vuelve a mostrar la misma máscara. **Resuelto:** `ui:key-submitted` emite la
clasificación de lo enviado. Es la diferencia entre un test que mira y uno que prueba.

**P2 — el propio afordance de debug habría falseado el E2E.** `debugConnStep` asigna `key.value`
directamente (`:1183`) sin pasar por `beforeinput`, así que `dataset.masked` seguiría puesto: el
guardarraíl mandaría vacío, Go probaría la clave **buena** guardada, y el caso negativo del E2E
diría verde. Habría verificado lo contrario de lo que afirma. **Resuelto:** el driver escribe por el
mismo helper que una tecla real.

**P2 — la afirmación sobre Azure era falsa.** La v1 decía que Guardar sobre una tarjeta enmascarada
sin tocar contesta "no hay nada que guardar". Para Azure no: la página siempre manda la región
(`settings.ts:921`) y `SaveConnection` la escribe cuando viene, así que contesta "Región guardada" y
pliega. El comportamiento está bien; la afirmación estaba mal. **Corregido arriba.**

## Tests

**En Go, rojo primero:**

| # | Caso | Asserts |
|---|---|---|
| 1 | ranura con clave guardada por la app | `Stored=true` |
| 2 | ranura vacía | `Stored=false` |
| 3 | clave por variable de entorno | `Stored=false` **aunque `Status=present`** — el caso que mentiría |
| 4 | credenciales ilegibles | `Stored=false` — "no pude leer" no es "hay una" |

Verificados por mutación, los cuatro.

**En la página**, que no tiene runner: se verifica por E2E con el afordance, y para eso el reporte de
tarjeta (`reportCard`, `settings.ts:1217`) gana tres campos observables — `keyField` (vacío /
enmascarado / escrito), `formOpen`, y el valor que el probe realmente usó.

## Riesgos

1. **El retardo de 1.2 s es un temporizador en una página que ya arbitra por épocas.** Mitigado
   reusando la misma guarda; el riesgo residual es plegar una tarjeta que el usuario acaba de
   reabrir, y por eso la época se re-comprueba al disparar y no sólo al programar.
2. **`dataset.masked` es estado en el DOM**, que es justo lo que este código evita en todas partes
   (una sola ruta de estado a píxeles). Aceptado: es estado sobre el WIDGET, no sobre el dominio, y
   no hay dónde más ponerlo — el valor de un input no puede distinguir origen por sí solo.
3. **Regenerar los bindings** tras añadir el campo (`./scripts/task.sh common:generate:bindings`), o
   la página no lo verá.

## What is NOT included

- Cambiar qué se guarda o cómo. Ningún cambio de backend salvo el campo `Stored`.
- Mostrar cualquier fragmento de la clave real. Ni prefijo ni cola: el branch anterior cerró dos
  fugas y ésta sería la tercera.
- Tocar el botón Borrar ni su condición.
