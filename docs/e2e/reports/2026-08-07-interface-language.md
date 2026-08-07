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
