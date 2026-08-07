# E2E — el idioma de la interfaz

VERDICT: PASS

**Este informe existe porque el cambio se mergeó sin él.** El i18n entró en `main` el 2026-08-07 con
`check-gates` en 3/6, por decisión explícita del dueño, y tres cosas quedaron descansando sólo en
leer el código: el cambio de idioma **en caliente**, el relabelado de la **bandeja**, y la revisión
cruzada del diff. Las tres se cierran aquí, con evidencia.

- **Feature:** el selector de idioma traduce la interfaz de verdad — markup, texto que emite Go,
  prosa que arma la página, el overlay y el menú nativo de la bandeja
- **Branch:** `verify/i18n-live-language` (el i18n en sí ya estaba en `main` en `c012328`)
- **Run:** 2026-08-07T15:02–15:07-05:00 — cuatro lanzamientos
- **Build:** `bin/loqui.app`, reempaquetada de este árbol, firmada ad-hoc. 15 paquetes verdes,
  `gofmt` y `vet` limpios, `tsc --target es2022` limpio.
- **Estado de partida:** `appLanguage` del usuario, sistema en `en_CO`

## Cómo se conduce, y el afordance que hubo que añadir

**Lo que hacía imposible verificar esto era el propio `<select>`:** dentro de un WKWebView de Wails no
se puede pulsar desde un script, así que "la interfaz sigue un cambio de idioma" no tenía forma de
comprobarse desde fuera. `LOQUI_DEBUG_SET_LANGUAGE=<es|en|system>` despacha un `change` **real** sobre
el control real, de modo que el que corre es el handler de la app — la misma regla que sigue el
afordance de las tarjetas de conexión. Se dispara **6 s después del arranque**, a propósito: lo que
había que ejercitar es cambiar el idioma de una interfaz **ya dibujada**.

Y la bandeja necesitó su propio observable, porque es el único sitio que ningún reporte de página
alcanza: los títulos de un `NSMenuItem` no están en ningún DOM. `relabelTray` registra las etiquetas
que construyó. Son copia de interfaz, nunca datos del usuario.

| Observable | Qué demuestra |
| --- | --- |
| `I18N locale/entries/marked/translated/sample` | qué hizo la página con el catálogo |
| `TRAY relabelled locale=… items=…` | que el menú nativo se reconstruyó, y en qué idioma |
| `LANG-HOOK` | que el hook de Go se disparó |
| `RECENT-SHAPE when` | la marca de tiempo tal como se renderizó |

---

## UC-LANG-01 — cambio el idioma con la interfaz ya en pantalla: inglés → español: PASS

```
15:02:33  I18N          locale:en  entries:201  marked:145  translated:129  sample:Home
15:02:33  RECENT-SHAPE  when:1h ago
15:02:38  LANG-SWITCH   requested:es
15:02:38  LANG-HOOK     interface language now es
15:02:38  TRAY          relabelled locale=es items=Dictar (prueba)|Ajustes…|Salir
15:02:38  I18N          locale:es  entries:0  marked:138  translated:0  sample:Inicio
15:02:38  RECENT-SHAPE  when:hace 1 h
```

- `sample` pasa de `Home` a `Inicio`: la página se retradujo con la ventana abierta.
- **`entries:0` en español es correcto, no un fallo.** La clave ES la respuesta, así que no hay tabla
  que enviar. Un catálogo no vacío ahí querría decir que algo se está traduciendo dos veces.
- **La bandeja siguió** — y es el caso que nunca se había ejecutado.
- El historial siguió: `1h ago` → `hace 1 h`. No es parte de lo que `paint()` reconstruye, así que
  llega por su propia vía.

## UC-LANG-02 — la vuelta: español → inglés: PASS

```
15:06:35  I18N          locale:es  entries:0    marked:145  sample:Inicio   when:hace 1 h
15:06:41  TRAY          relabelled locale=en items=Dictate (test)|Settings…|Quit
15:06:41  I18N          locale:en  entries:204  marked:138  sample:Home     when:1h ago
```

La ida y la vuelta, no sólo una dirección: un cambio que sólo funciona hacia un lado es un cambio que
funciona por accidente.

## UC-LANG-03 — "Seguir el sistema": PASS

```
sistema:  AppleLocale = en_CO
15:07:26  LANG-HOOK    interface language now en
15:07:26  TRAY         relabelled locale=en items=Dictate (test)|Settings…|Quit
15:07:26  I18N         locale:en  sample:Home
en disco: "appLanguage": ""
```

Guarda **vacío** y resuelve a `en`. Sólo Go puede contestar esto: la app lanzada desde Finder no
hereda `LANG`, así que el idioma del sistema se lee por cgo desde `NSLocale`.

**Y esto explica el reporte original del usuario.** Su sistema está en inglés y el ajuste decía
inglés — lo que faltaba no era resolver el idioma, era que alguien tradujera.

## UC-LANG-04 — `marked` baja de 145 a 138, y NO es un fallo: PASS

Lo destapó el propio observable, y merece quedar escrito porque parece una regresión:

```
antes del primer paint:  marked:145
después:                 marked:138   (−7)
```

`paint()` reconstruye el `innerHTML` del selector de motor, y los seis `<option>` del markup **están
marcados** con `data-i18n`. Al reemplazarlos, esas marcas desaparecen — que es exactamente el hallazgo
nº1 de la revisión de diseño, visto desde otro ángulo.

**Corregido en el mismo pase:** las opciones reconstruidas se traducen ahora al construirse, por
`t()`, tanto la etiqueta como los sufijos (`— no disponible aún`, `— sin configurar`, `— no disponible
en este sistema`). Las seis etiquetas de motor son nombres de producto y vuelven iguales; **los
sufijos no**, y esos se veían en español en una interfaz en inglés.

`ENGINE_LABELS` se sigue capturando **antes** de traducir, y eso es deliberado: el español es la
clave. Capturarlo después guardaría inglés y perdería la posibilidad de buscar nada.

---

## Lo que este informe NO cubre

1. **Ningún idioma más allá de `es` e `in`glés.** El original declara pt/fr/it/de y deliberadamente no
   los ofrece hasta estar traducidos y revisados. Se mantiene ese criterio.
2. **Que la traducción sea BUENA.** Está verificado que el mecanismo entrega inglés donde antes había
   español, no que el inglés sea el que un hablante nativo elegiría. Las ~204 entradas no las ha
   revisado un traductor.
3. **La ventana del overlay en vivo.** Pide su catálogo y escucha `settings:language`, y eso se lee en
   el código; lo que no se ha hecho es dictar en inglés y ver la palabra `reconnecting…` en la píldora.
   Necesita una reconexión real, que no se provoca a voluntad.
4. **El estado del menú de la bandeja tras un rebuild.** `relabelTray` reemplaza el menú entero. Hoy
   no tiene estado que perder — tres acciones sin marcas ni casillas — y está dicho en su comentario
   para quien añada una.

---

## La revisión cruzada del diff — codex: 0 P0, 10 P1, 4 P2

El tercer hueco. **Encontró un fallo que hacía que este informe fuera casi imposible de escribir con
honestidad**, y que ninguno de mis seis barridos veía.

### El que importa: las claves concatenadas nunca coincidían

Go une literales en tiempo de compilación. Un mensaje escrito así:

```go
return s.revealFailed("esta ranura la controla la variable de entorno %s — la clave que se usa "+
    "viene de ahí, no de las guardadas", name)
```

es UNA cadena cuando `phrase()` la busca. Mi extracción guardó **los fragmentos**, así que la
búsqueda fallaba y el mensaje entero llegaba al usuario en español — **y mi test de cobertura lo daba
por cubierto**, porque su regex también capturaba sólo el primer literal. El guardián y el fallo
compartían el mismo punto ciego.

Arreglado en el test primero, que es lo que dijo la verdad: **nueve mensajes completos** sin entrada.
Nueve fragmentos muertos borrados del catálogo, ocho mensajes enteros traducidos.

### Los otros que se arreglaron aquí

- **Cambiar de idioma con un error visible en el overlay lo BORRABA.** Un error no tiene clave —su
  texto lo arma Go— así que su `data-key` está vacío, y yo reescribía `tr("")` sin condición. Peor que
  dejarlo en el idioma anterior: borraba la explicación de por qué había fallado el dictado.
- **El comentario del overlay afirmaba que los errores venían traducidos de Go. No es cierto**, y hoy
  sigue sin serlo. Corregido el comentario en vez de dejar una mentira cómoda.
- **El overlay no tenía guarda de generación** para su catálogo, que la página sí tenía.
- **Los avisos de motor no pasaban por el traductor** — "Motor activo: OpenAI" en una interfaz en
  inglés. No podían: concatenan el nombre del motor. Ahora son claves con `{engine}`.
- **El afordance aceptaba cualquier texto.** `LOQUI_DEBUG_SET_LANGUAGE=en-US` dejaba el `<select>` en
  su opción vacía, **persistía "seguir el sistema"** —cambiando una preferencia real por un typo— y
  escribía el valor crudo del entorno al log. Ahora valida contra las opciones del propio control:
  `LANG-SWITCH allowed:|es|en rejected:true`, y el ajuste no se movió.
- **Yo había metido metadato de actividad al log.** El `when` de los reportes de forma es *cuándo
  dictaste*, y esos logs no lo llevaban antes. Un apoyo de verificación no puede ensanchar lo que
  escribe cada corrida normal: ahora va detrás de `LOQUI_DEBUG_TIME_TEXT=1`.

### Y una regresión que me hice yo al arreglar otra cosa

La revisión señaló que el cambio en caliente repintaba antes de traer el catálogo. Lo "arreglé"
trayendo el catálogo primero — **y quedó peor**: el fetch le pide a Go un idioma que todavía no le han
dicho, así que devolvía la tabla vieja. Se ve en el log de esa corrida, `I18N locale:en` **antes** de
`LANG-HOOK now es`.

El orden correcto son tres pasos y cada uno está porque los otros dos no bastan: **escribir**, para
que Go sepa; **traer**, para tener la tabla nueva; **repintar**, porque `applyTranslations()` sólo
reescribe nodos marcados y hay prosa que se CONSTRUYE con `t()` durante un pintado. Verificado:

```
15:23:01  LANG-SWITCH  requested:en
15:23:01  LANG-HOOK    interface language now en
15:23:01  TRAY         relabelled locale=en items=Dictate (test)|Settings…|Quit
15:23:01  I18N         locale:en
15:23:01  RECENT-SHAPE when:1h ago
```

### Declarados abiertos, con file:line

No se tocan aquí, y ninguno es una fuga ni deja la app peor de lo que estaba:

1. **`AboutService` no tiene idioma** (`about_service.go:69`): "Sistema operativo", "Carpeta de datos".
2. **Prosa que la página arma en runtime, fuera de Ajustes**: permisos (`permissions.ts:94-145`),
   estados vacíos e historial (`history.ts:129-220`), onboarding (`onboarding.ts:159-238`).
3. **`translatePayload` se deja `ProviderHint` y los controles de idioma de dictado**
   (`i18n_payload.go:34`, `language.ts:65-121`).
4. **Cambiar el idioma DESDE el onboarding no recarga el catálogo** (`onboarding.ts:229`).
5. **`relabelTray` no libera el menú anterior** (`main.go:378`): cambiar de idioma muchas veces
   acumula menús y callbacks en el mapa global de Wails. Verificado por codex contra el código de
   Wails, no ejecutado.
6. **La cobertura sigue sin ver** prosa suelta en TypeScript, campos del payload, `AboutService` ni
   los `WriteResult` construidos a mano. Es el hueco que decide si el resto se termina.

---

## Los seis puntos declarados, cerrados — 2026-08-07, más tarde

Se cerraron **empezando por el sexto**, y el orden fue la decisión que lo hizo tratable: ensanchar el
guardián primero, para que dijera exactamente qué faltaba en los otros cinco en vez de adivinarlo.

### 6 → primero: dos barridos nuevos, y van al revés que los anteriores

Los seis barridos existentes sólo veían una cadena que alguien **ya había enrutado** — un elemento
marcado, una llamada a `t()`, un mensaje entregado a un constructor conocido. Eso protege contra
olvidar traducir lo que te acordaste de enrutar. Es ciego al fallo real de una migración a medias:
**prosa que nunca se enrutó**.

Los dos nuevos buscan prosa española en las fuentes de la página y en los servicios de Go, y fallan
ante lo que no esté demostrablemente atendido. Encontraron **35**: 18 en la página, 17 en Go.

Verificados por mutación (una constante en español en `permissions.ts`, una fila renombrada en
`about.go`); cada uno muere en su hueco.

Una decisión que merece constar: **`no` está deliberadamente fuera** de la lista de palabras
españolas, aunque lo sea. También es inglesa, y las cadenas de diagnóstico de estos archivos son
inglesas (`no such card`, `no language select`). Un guardián que grita en falso se acaba silenciando,
y eso es peor que un hueco.

### Y de paso destapó un fallo latente que nadie había pedido buscar

`settings.ts` decidía si ya había añadido el sufijo "— no disponible aún" preguntando si el texto
visible **contenía esa frase en español**. En cuanto la etiqueta se traduce eso deja de funcionar: una
opción en inglés recibiría el sufijo otra vez en cada repintado, alargando el nombre cada vez. Es
exactamente el bug que el comentario de `ENGINE_LABELS` documenta, reintroducido por la puerta de
atrás. Ahora la marca es un flag de datos, no una subcadena.

### 1 — `AboutService`: PASS

`BuildAbout` recibe el idioma como **parámetro** en vez de traducirse en la frontera, y la razón es
concreta: `"Versión {v}"` interpola un valor, así que su forma final no puede ser clave de catálogo.
Sigue siendo una función pura de sus entradas, que es lo que la hace testable sin store ni máquina.

**Los VALORES no se traducen nunca** — versión de macOS, arquitectura, rutas, código de locale.
Traducirlos corrompería justamente los datos que este panel existe para que se copien a un reporte.

```
arrancando en inglés:  ABOUT  version:Version 0.1.0
```

### 2 — la prosa que arman permisos, historial y onboarding: PASS

Enrutada por `t()`. Las filas de permisos ya pasaban por el traductor desde la rama anterior — lo que
faltaba eran las entradas, así que se traducían a sí mismas y nadie lo notaba.

### 3 — `ProviderHint` y los controles de idioma: PASS

`ProviderHint` es la frase bajo el selector de motor: la que explica por qué el motor elegido no puede
dictar. Era **el aviso más importante de esa pantalla** y salía en español. También los `label`,
`desc` y las etiquetas de opción de los controles de idioma de dictado — nombres de idioma, que son
copia; los **códigos** de al lado no se tocan.

### 4 — cambiar el idioma desde el onboarding: PASS

Sólo escribía. En una instalación nueva, elegir inglés actualizaba Go, el overlay y la bandeja
mientras el tutorial que el usuario estaba mirando seguía en español hasta reiniciar — y ése es el
único momento en que se forma una primera impresión. Ahora sigue los tres pasos: escribir → traer →
repintar.

### 5 — la fuga de menús de la bandeja: PASS

`Menu.Destroy()` **sí existe** en la versión de Wails fijada; Codex tenía razón en que no se llamaba.
Dos cambios, y el segundo importa tanto como el primero:

- **Se libera el menú anterior DESPUÉS de instalar el nuevo**, nunca antes: hasta que `SetMenu`
  retorna, el menú viejo es lo que la bandeja está mostrando, y liberar un `NSMenu` todavía adjunto
  es como se introduce un crash nativo.
- **No se reconstruye si el idioma no se movió.** Antes llegaba aquí cada guardado del panel Sistema.

```
cambio real   en → es :  rebuilds: 1
mismo idioma  es → es :  rebuilds: 0   (y el hook sí se disparó)
```

---

## Revisión de la calidad del inglés — 2026-08-07

Las 249 entradas leídas una por una, como traductor y no como programador. Auditoría del catálogo:
**0 iguales a su clave, 0 vacías**, y **una sola** traducción repetida — que es un arreglo, no un
descuido (ver abajo).

**Una alarma mía que resultó infundada, y la retiro:** creí que `"Mantén la tecla"` → `"Hold the"`
estaba truncado. No lo está. La frase va partida alrededor de `<b>fn</b>` y el segundo fragmento
**empieza por `key`**, así que compone "Hold the **fn** key (or use the tray icon)…". El orden de
palabras —que en español pone el sustantivo antes del nombre de la tecla y en inglés después— ya
estaba resuelto.

### Lo que sí estaba mal

**La misma acción con dos nombres.** La bandeja decía `Dictate (test)` y el botón de Inicio
`Test dictation` **para el mismo comando**. Un usuario no tiene forma de saber que son lo mismo.
Ahora las dos dicen `Test dictation` — es la única traducción repetida del catálogo, y lo es a
propósito. Verificado: `TRAY relabelled locale=en items=Test dictation|Settings…|Quit`.

**Los nombres de los paneles de macOS, en su capitalización real.** `Input Monitoring` y
`Speech Recognition`, no en minúsculas: el usuario va a buscarlos **literalmente** en Ajustes del
Sistema, y una etiqueta que no coincide con la de Apple es una que no encuentra.

**"Application" es la palabra del manual; la interfaz dice "App".** Y esta misma app ya usaba "app"
en otra frase, así que había dos registros conviviendo. Unificado.

**Líneas de estado con artículo.** El inglés de interfaz no lo lleva: `Testing connection…`,
`Deleting key…`, `nothing to save`, `Could not list microphones:`.

**`Detección automática` → `Auto-detect`**, no "Automatic detection": es una opción de un selector,
no la descripción de una función.

**`Abrir reporte en GitHub` → `Open an issue on GitHub`.** GitHub no los llama "reports". El botón se
busca por el nombre que usa la plataforma.

**`✗ Deja al menos un idioma` → `✗ Keep at least one language`.** "Leave" se lee como *marcharse*, que
es lo contrario de lo que pide.

**Y siete calcos** que eran correctos y sonaban traducidos: pasivas heredadas del español
("Lets the transcribed text be inserted" → "Lets Loqui insert…"), paralelismo roto ("No key and no
internet" frente a un "It needs…" en la frase siguiente → "Needs no key and no internet"), y frases
que decían la idea sin decirla como la diría un nativo.

### Lo que esta revisión NO es

**Un hablante nativo no la ha visto.** Es mi juicio aplicado con las convenciones de interfaz de
macOS y con el criterio de consistencia interna, que es donde estaban los defectos que importaban.
Sigue siendo una revisión de una sola persona, y las 249 entradas no han pasado por un traductor
profesional.
