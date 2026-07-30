# Evidencia E2E — ajustes de UI, vistas inertes y dos proveedores nuevos

VERDICT: PARTIAL

> **Deliberadamente NO es `PASS`.** `check-gates.sh` sólo acepta `VERDICT: PASS` para dar la casilla por
> cumplida, y este informe no puede declararlo: **ElevenLabs y OpenAI realtime no se han ejecutado
> contra su servicio real** — no hay credenciales de ninguno de los dos en esta máquina. Todo lo demás
> —cada ajuste de interfaz, el tutorial, Acerca de, los enlaces, la activación al arrancar— sí se
> verificó contra la app empaquetada. Poner `PASS` sería firmar como probado un porte cuyo camino de red
> nadie ha recorrido de verdad. La casilla del ship-gate va como `N/A:` con el motivo a la vista.

- **Fecha:** 2026-07-30
- **Rama:** `fix/home-engine-select-height` (el nombre nació para un dropdown y acabó llevando diez temas)
- **Objetivo:** la app empaquetada (`bin/loqui.app`), con las sondas gateadas por variable de entorno, y
  servidores WebSocket **locales** para el ciclo de vida de los dos proveedores nuevos.
- **Resultado:** **8 recorridos PASAN**, **2 BLOQUEADOS** por falta de credencial.

## Cómo se midió, y por qué importa decirlo

Tres cosas invalidaron mediciones durante esta sesión y cambiaron el método:

1. **Las coordenadas fijas en capturas no valen.** La ventana aparece en un sitio distinto en cada
   lanzamiento. Una afirmación mía —que "Acerca de" abría desplazada— era **falsa**: un recorte 30 px
   fuera de sitio. Los recortes de aquí se **anclan al degradado del logo**, y el DOM medido desde dentro
   manda sobre el píxel.
2. **Otras apps roban el frente.** Varias capturas salieron ocluidas; una midió una foto de un
   teleférico. El script reintenta comparando una región conocida y comprueba que la app esté visible.
3. **Los iconos del Dock no se pueden medir en capturas.** El tamaño de los tiles depende de cuántos
   iconos hay y arrancar la app los reordena: tres intentos dieron 38, 61 y 32 px para lo mismo. Se
   comparan los **archivos generados**.

## UC-01 — alturas de los controles → **PASA**

Medido en la app empaquetada, borde superior e inferior en píxeles:

```
dropdown de Inicio            444..478   35 px
Historial — búsqueda          478..512   35 px
Historial — select de fecha   478..512   35 px
Historial — botón opciones    478..512   35 px
Sistema — idioma              696..730   35 px
Sistema — micrófono           851..885   35 px
```

Antes: el de Inicio 20 px, el de fecha 20, los de Sistema 21. Causa: WKWebView dibuja un popup nativo y
descarta el padding (`systemtray_darwin.m` es el equivalente para el tray; aquí es el control nativo).

## UC-02 — columna de fechas de Actividad reciente → **PASA**

Las cuatro fechas terminan en el mismo borde derecho, incluida la fila de dos líneas. Antes se pegaban
al texto cuando la entrada era corta.

## UC-03 — Sistema guarda sin botón → **PASA**

```
SYS-PROBE   map[control:appearance value:light via:radio]   ← por el control real, no el binding
SYS         map[appearance:light ...]
=== app cerrada, leyendo el disco ===
appearance = 'light'
```

Partiendo de `dark`, un click en el radio → la app muerta y el valor en disco es `light`. La sonda
**clica el radio**, no llama al setter: driving el binding pasaría con el listener ausente.

## UC-04 — Acerca de informa de verdad → **PASA**

```
ABOUT   map[pathRows:3 systemRows:4 version:Versión 0.1.0]
```

En pantalla: `macOS 26.5.2 (arm64)`, `en-CO`, `go1.26.5`, `v3.0.0-alpha2.119` y las tres rutas reales.

## UC-05 — el tutorial se muestra, por las dos vías → **PASA**

Botón del pie, con el flag ya en `true` para que el auto-abrir no falsee el resultado:

```
WIZARD  map[open:true step:0 steps:6]
WIZARD  map[configControls:1 engines:6 permRows:5 prefsControls:4 step:0 steps:6]
```

Primera vez, flag en `false` y sin sonda: se abre solo. Al terminar, `onboarded` pasa de `False` a
`True` en disco con la app cerrada. La sonda **clica el botón real** del pie.

## UC-06 — la ventana llega al frente al arrancar → **PASA**

```
open  #1 #2 #3 : visible=true miniaturized=false key=true appActive=true
term  #1 #2 #3 : visible=true miniaturized=false key=true appActive=true
```

Tres corridas por vía. Antes, desde terminal: `key=false appActive=false` — visible y **detrás**, nunca
minimizada. La primera medición tras el arreglo dio `appActive=false` con `open` y parecía regresión;
repetir mostró que era otra app robando el frente.

## UC-07 — los enlaces de donación abren → **PASA**

```
DONATE  map[found:true probe:openDonate]    DONATE  map[from:openDonate ok:true]
DONATE  map[found:true probe:aboutDonate]   DONATE  map[from:aboutDonate ok:true]
```

Abrió dos pestañas reales en el navegador. Las sondas clican los botones reales.

## UC-08 — el selector de motores no ofrece lo que no funciona → **PASA**

```
whisper                = Whisper — local (offline)
macos                  = macOS — local (offline)
azure       [disabled] = Azure Speech — sin configurar
openai      [disabled] = OpenAI — sin configurar
grok        [disabled] = Grok (xAI) — sin configurar
elevenlabs  [disabled] = ElevenLabs — sin configurar
```

Coincide con las tarjetas de Conexiones (`CONN … azure=unconfigured openai=unconfigured …`). Con
`provider=azure` guardado y sin clave, esa opción se mantiene visible y el hint dice *"Este motor
necesita configuración — ábrela en Ajustes"*: un `<select>` no puede mostrar un valor sin opción, y
ocultarla haría que el selector mintiera sobre el motor en efecto.

## UC-09 — dictar con ElevenLabs → **BLOQUEADO**

No hay clave de ElevenLabs. **No ejecutado.**

Lo que sí está verificado, contra un servidor WebSocket local (sockets reales, no interfaces simuladas —
lo que falla en estos proveedores es orden y tiempos, y eso no se alcanza con un stub): handshake, audio
como JSON con base64, orden del audio previo al handshake, release antes del handshake, varios
`committed_transcript` unidos sin truncar, clasificación de 401/403/429/503 y del 400 que en realidad es
clave inválida, tope del búfer, orden de los eventos de cierre. **12 recorridos de red.**

**Hueco declarado:** invertir `flush` y `finalize` en la rama del stop pasa la suite entera. Anotado en
el código; un test que repetía la carrera 24 rondas también pasó con el orden invertido y se borró por
afirmar cobertura que no tenía.

## UC-10 — dictar con OpenAI realtime → **BLOQUEADO**

No hay clave de OpenAI. **No ejecutado.**

Verificado contra servidor local, **14 recorridos**, incluido el que sólo existe en este proveedor:

```
llegaron 480 muestras por 320 enviadas   (16 kHz -> 24 kHz)
```

El audio se **resamplea en el cable**. Sin eso el servicio acepta 16 kHz en una sesión declarada a 24 y
transcribe una voz acelerada — no da error, así que nada más lo reportaría. También: `session.update`
antes de cualquier audio (incluido el caso con audio ya en el búfer, que es el único que distingue el
orden), la clave en los subprotocolos y no en la URL, los deltas mostrados como frase que crece, el
`completed` sin transcript cayendo a los deltas, y deltas sin cerrar llegando al final.

## Lo que este informe no dice

- Que los dos proveedores nuevos transcriban. **No se ha comprobado.** Con una clave, un dictado real
  por `cmd/stt-probe` lo cerraría.
- Que el anillo de foco del selector de Inicio esté resuelto. No lo está.
- Que el hueco de orden en ElevenLabs esté cubierto. No lo está, y está escrito donde se ve.
