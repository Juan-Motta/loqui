# Plan — que el idioma de la interfaz traduzca de verdad

- **Date:** 2026-08-07
- **Reported by the user** on 2026-08-07: "el idioma de la interfaz no está funcionando; me aparece
  que debería estar en inglés, pero toda la interfaz está en español".

## Diagnóstico: no hay nada roto que arreglar

**La traducción nunca se portó.** El markup trae 155 hooks `data-i18n` heredados verbatim de Electron
y **ninguna línea del port los aplica**. El selector confunde porque funciona a medias: persiste
(`SetAppLanguage`), se relee (`system.ts:259`) y hace una cosa real — el locale de fechas del
historial (`settings.ts:1389`).

No estaba en la lista de controles inertes de `CONTINUITY.md`. Se añade al cerrar.

## El diseño del original, que se respeta

**Las claves SON las cadenas en español.** No hay espacio de claves inventado que mantener, lo que
falte **degrada a español legible** en vez de escupir `ui.settings.header`, y el diff que introduce
i18n sigue siendo revisable. El precio —editar la copia rompe su clave— lo paga un test de cobertura.

Resolución de `t()`: idioma actual → inglés → **la propia clave**.

`resolveLocale(stored, systemLocale)`: vacío significa "sigue al sistema", que es el defecto; una
elección explícita siempre gana; un idioma del SO que no tenemos traducido cae al español en vez de
enseñar media interfaz.

## La decisión de arquitectura, tomada con el usuario

El original tenía la redacción en módulos TS puros que llamaban a `t()`. **Este port la movió a Go a
propósito.** Hoy 17 archivos de producción en Go emiten texto al usuario y ningún `data-i18n` los
alcanza.

> **El catálogo vive en Go y se aplica en los dos lados.** Go traduce lo suyo —ya tiene el idioma,
> está en los ajustes que ya lee— y expone el catálogo por un binding para que la página lo aplique
> a los hooks del markup.

**Una sola fuente.** Dos catálogos derivarían, y la deriva en i18n es invisible: se manifiesta como
una frase suelta en el idioma equivocado que ningún test ve.

**Descartado:** que Go devuelva códigos y la página traduzca. Es más ortodoxo, pero obliga a rehacer
`WriteResult`, `ProbeResult` y las filas de conexión y permisos —los seams que se acaban de
endurecer— y todos sus tests, a cambio de nada que el usuario note.

## Alcance, medido y no estimado

| Superficie | Cadenas | Ya en el catálogo del original | A traducir |
| --- | --- | --- | --- |
| Markup (`data-i18n`) | 117 distintas | 105 | **12** |
| Go (17 archivos) | ~94 (heurística) | 28 | **~66** |

**El catálogo del original NO se copia tal cual**, y esto ya mordió una vez al medirlo: las claves son
las cadenas en español y el port cambió parte de la copia. El placeholder de la clave dice
`"…se guarda en tu equipo, sin cifrar"` donde el original decía `"…se cifra al guardar"` — cambio
deliberado, porque aquí no se cifran. Copiar dejaría esa cadena en español **en silencio**.

De las 12 que faltan en el markup, seis son nombres de producto (Azure, OpenAI, Grok, ElevenLabs,
Whisper, macOS) que van a `SAME_IN_ENGLISH` — declarados explícitamente para que el test distinga
"igual a propósito" de "alguien se olvidó".

## Design

### 1. `internal/i18n`, y el catálogo es dato, no código

- `Locale`, `Available()` (`es`, `en`), `Default = es`.
- `ResolveLocale(stored, systemLocale)` — portado con su semántica.
- `T(locale, key, params)` con interpolación `{nombre}`; resolución idioma → inglés → clave.
- El catálogo en `en.go` como `map[string]string`, más `SameInEnglish` como conjunto.

`macos.SystemLocale()` **ya existe** — cgo, porque una app lanzada desde Finder no hereda `LANG`.

### 2. El seam: cómo llega el idioma a los servicios

`SettingsService` y `Bootstrap` ya leen el store, y el idioma está ahí. Se resuelve **una vez por
payload**, no por cadena, y se pasa a las funciones que redactan. Las funciones puras de `store`
(`ConnectionRows`, `PermissionRows`, `keyStateLabel`…) reciben el locale como parámetro: siguen siendo
puras y siguen siendo testables sin GUI, que es la propiedad que las hace valiosas.

### 3. La página aplica el catálogo al markup

Un binding entrega `{locale, catalog}`. La página, en el primer pintado y en cada cambio de idioma:

- guarda el español original en `data-i18n-src` la primera vez —después del primer pase el texto ya
  no es la clave—, y
- recorre `[data-i18n]` y `[data-i18n-attr]`.

### 4. El test de cobertura, que es la pieza que impide que esto se pudra

Un test en Go que lee `frontend/index.html`, extrae cada cadena marcada y **falla** si no tiene
entrada ni está declarada como igual en inglés. Es lo que el original ya tenía
(`test/unit/i18nCoverage.test.ts`) y la razón de que su catálogo siga vivo.

Segundo test, el reverso: **ninguna entrada puede ser igual a su clave** — eso significa "no
traducido" disfrazado de traducido.

## Tests (TDD, rojo primero)

| # | Caso | Asserts |
|---|---|---|
| 1 | `T` con locale `es` | devuelve la clave, sin tocar el catálogo |
| 2 | clave sin entrada en `en` | **devuelve la clave en español**, no vacío ni la clave cruda |
| 3 | interpolación `{n}` | sustituye, y deja intacto un `{desconocido}` |
| 4 | `ResolveLocale("", "en-GB")` | `en` — sigue al sistema |
| 5 | `ResolveLocale("es", "en-GB")` | `es` — la elección explícita gana |
| 6 | `ResolveLocale("", "de-DE")` | `es` — un idioma sin traducir cae al defecto |
| 7 | cobertura del markup | toda cadena marcada tiene entrada o está en `SameInEnglish` |
| 8 | ninguna entrada igual a su clave | atrapa el "traducido" que no traduce |
| 9 | el payload en `en` | los badges y avisos llegan en inglés |

Verificados por mutación.

## Riesgos

1. **Traducir 66 cadenas de Go toca 17 archivos.** Mecánico pero ancho, y cada uno tiene tests que
   afirman el texto en español. Se hace por paquetes, no de una vez.
2. **Los tests existentes afirman prosa española.** Los que comprueban el mensaje al usuario tendrán
   que pedir el locale `es` explícitamente, que además documenta cuál esperan.
3. **El overlay es otra ventana** con su propio HTML; entra en el mismo pase o queda declarado fuera.

## What is NOT included

- Idiomas más allá de `es` e `in`glés. El original declara pt/fr/it/de y deliberadamente no los
  ofrece hasta estar traducidos y revisados; se mantiene ese criterio.
- Traducir los logs. Son para diagnóstico, no para el usuario.

## Review de diseño — codex, 2026-08-07: 0 P0, 7 P1

Ninguna fuga ni riesgo de seguridad. Los siete hallazgos dicen lo mismo desde siete ángulos: **el
plan cubría el markup estático y la interfaz tiene mucha más prosa que eso.** Todos verificados
contra el código. Dos eran defectos en lo ya escrito y están arreglados; cinco son alcance que el
plan daba por cerrado y no lo estaba, y quedan **declarados abiertos** en vez de silenciados.

### Arreglados

**La caché de origen envenenaba los nodos que cambian en runtime.** El peor de los siete, porque su
resultado es *peor que no traducir*: enseña la traducción equivocada. `#wizNext` pasa de `Continuar`
a `Empezar` y el botón de dictado alterna `Probar dictado`/`Detener`, los dos marcados con
`data-i18n`. La caché guarda el primer texto como clave para siempre, así que el botón habría dicho
"Continue" en el último paso del tutorial. Ahora hay `setText(el, español)`, que reescribe la clave y
la traducción a la vez, y se usa en los dos sitios.

**El catálogo no estaba ordenado contra sí mismo.** Dos cambios de idioma seguidos producen dos
peticiones en vuelo, y la que contesta última no es la que se pidió última. Un contador de generación
descarta la respuesta rancia — el mismo razonamiento que la arbitración por revisión del payload.

### Declarados abiertos, con su file:line

1. **La prosa generada en runtime no la toca nadie.** Las opciones del selector de motor
   (`settings.ts:535`), las de idioma y dispositivo (`system.ts:255`), los permisos
   (`permissions.ts:104`), las marcas de tiempo relativas (`history.ts:64`) y el onboarding
   (`onboarding.ts:159`) fabrican español directamente, sin `data-i18n`. Se ven en inglés a medias.
2. **`PermissionsService` no tiene idioma.** Se construye sin store ni resolutor (`main.go:133`), así
   que "Volver a comprobar" reconstruye sus filas en español (`permission_rows.go:64`).
3. **El overlay y la bandeja son otra UI.** El overlay es un webview aparte con `reconectando…`
   escrito a mano (`overlay.ts:29`) y la bandeja se construye una vez en español (`main.go:348`).
   Ninguno tiene propagación de cambio de idioma.
4. **Un mensaje de un payload rechazado por rancio se sigue pintando** (`settings.ts:1070`), así que
   puede caer un veredicto en español sobre una página ya en inglés.
5. **Los tests de cobertura son más flojos que los del original.** Los míos sólo miran cadenas YA
   marcadas. Los del original fallan además ante markup **sin marcar**, claves de `t()` sin entrada y
   literales españoles sueltos (`../loqui/test/unit/i18nCoverage.test.ts:29,66,102`). Con los míos,
   añadir `<button>Exportar</button>` pasa desapercibido — y el fallback a español hace que la
   omisión parezca intencionada. **Es el hallazgo que más importa a medio plazo**, porque es el que
   decide si el resto se termina o se pudre.

### Y lo que el plan ya sabía: las cadenas de Go

~66 cadenas en 17 archivos siguen en español. Es la mitad ancha del trabajo y no ha empezado.
