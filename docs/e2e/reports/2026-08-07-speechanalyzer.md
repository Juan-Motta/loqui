# E2E — el motor de Apple (SpeechAnalyzer)

VERDICT: PASS

**El blocker era falso.** Durante semanas la tabla de proveedores dijo *"blocked before `started`,
cause unknown"* y el motor de Apple figuró como ⛔. Nunca estuvo bloqueado: alcanza el mismo estado
vivo que whisper, y lo que fallaba eran **dos huecos de instrumentación** que hacían indistinguible un
motor muerto de uno funcionando.

- **Branch:** `fix/speechanalyzer`
- **Run:** 2026-08-07T16:51–17:12-05:00 — ocho lanzamientos
- **Build:** `bin/loqui.app` reempaquetada, helper Swift recompilado. `task check` verde (15 paquetes,
  `vet`, tipos del frontend).
- **Máquina:** macOS 26.5.2 (25F84)

## Cómo se encontró: instrumentar en vez de teorizar

El helper es autocontenido y emite JSON por línea, así que se puede correr **sin la app**. Solo, llegaba
a `{"type":"started"}`. Por la app, no. Ese diferencial descartó el framework de Apple y señaló al
arranque desde la app — pero no decía dónde.

Se le añadieron **migas entre cada `await`** y se comparó. Por la app el helper recorre **todos** los
pasos: construye el transcriber, consulta los locales instalados, crea el `SpeechAnalyzer`, obtiene el
formato de audio, arranca `AVAudioEngine`, arranca el analizador y se queda esperando resultados.

Y como `starting analyzer` aparece **después** de la línea que emite `started`, `started` se emitió.
Las migas se quitaron después: eran para una investigación, no para el log de cada sesión.

## UC-APPLE-01 — el motor alcanza el mismo estado vivo que whisper: PASS

La prueba definitiva no fue una miga, fue comparar contra un motor que sí funciona:

```
macos     OVERLAY listening → idle    MIC peak 0.08
whisper   OVERLAY listening → idle    MIC peak 0.16
```

`listening` lo produce **únicamente** `stt.Started` (`session/overlay.go:38`). Así que el evento del
helper de Apple se recibe, se parsea y se consume, exactamente igual que el de whisper.

## Los dos huecos que sostenían el blocker

**1. `stt.Started` no se registraba en ninguna parte.** Se consume para mover la píldora del overlay, y
nada lo escribía. Así que *"el motor nunca arrancó"* y *"arrancó y nadie lo apuntó"* producían **el
mismo log**, y el blocker se construyó sobre esa ambigüedad. Lo que lo delató: **whisper, que funciona,
también registra cero menciones de "started"** — o sea que la ausencia nunca fue evidencia de nada.
Ahora el estado del overlay se registra.

**2. El helper de Apple no reportaba niveles de micrófono.** Abre el micrófono él mismo, así que el
host no puede medirlo, y el `MIC peak 0.00` resultante se leía como silencio. **Casi vuelve a
descarrilar esta investigación:** ese 0.00 se tomó al principio como pista de contención del
micrófono, antes de comprobar que ese medidor simplemente no existía para este motor. Ahora reporta
niveles desde el tap: **0.08** donde antes 0.00.

## La revisión del arreglo — codex: 0 P0, 6 P1, 1 P2

Todos arreglados. El código nuevo vive en un **hilo de audio en tiempo real**, que es donde un
descuido cuesta caro:

- **Nada bloqueante en el tap.** La primera versión hacía `JSONSerialization`, `print` y `fflush` desde
  ahí. Si el pipe se llena o el host se pausa, **el tap se atasca y se pierde audio capturado** — el
  motor transcribiría peor por culpa de su propio medidor. Ahora el tap sólo actualiza un número bajo
  un lock y una cola serie emite.
- **El máximo se acumula, no se muestrea.** Los callbacks llegan cada ~85 ms y el throttle es de
  100 ms, así que uno de cada dos se descartaba entero: un sonido corto que cayera dentro de un
  descartado desaparecía, y el log seguiría diciendo 0.00.
- **RMS ×4, no pico crudo**, porque es lo que el host calcula para los proveedores de nube
  (`audio/pcm.go`). Con pico crudo la misma voz daba ~0.10 aquí y ~0.28 allí: ni las barras ni el
  umbral de "¿hubo audio?" significarían lo mismo según el motor.
- **El layout del buffer no se asume.** Un input **entrelazado** tiene UN puntero con los canales
  tejidos, así que indexar por frame se mete en el otro canal; y un buffer que no sea Float32 no tiene
  `floatChannelData` en absoluto, lo que habría reportado silencio mientras una interfaz externa
  transcribía perfectamente. Ahora se miran `stride`, `isInterleaved` e `int16ChannelData`.
- **Un nivel que llegaba tras el stop sembraba el pico de la SIGUIENTE sesión.** `StopEngine` resetea
  el pico y *después* para el helper, y al de Apple se le manda SIGTERM 300 ms más tarde. En esa
  ventana un nivel tardío hacía que un dictado que no oyó nada se registrara **como si hubiera habido
  audio** — destruyendo la única línea cuyo propósito es distinguir "no llegó audio" de "llegó audio y
  el motor no devolvió nada". Hay un flag de sesión que se abre al arrancar y se cierra al reportar.
- **El log del overlay llevaba la prosa cruda del proveedor.** Ese texto lo escribe el vendor y este
  log es lo que se adjunta a un reporte de error; este proyecto ya arregló dos fugas de credencial que
  vivían justamente en esa clase de texto. Ahora se registra el estado y, si hubo mensaje, sólo que lo
  hubo.
- **P2, el estado del throttle sin sincronizar** — resuelto al mover la emisión a la cola serie.

## Lo que este informe NO cubre

1. **Que transcriba.** No se ha verificado y **no puede verificarse sin una voz**: el afordance arranca
   y detiene un dictado, pero no habla. Lo que queda demostrado es que **la premisa del blocker es
   falsa** — el motor alcanza el mismo estado que whisper y ahora sus barras se mueven, que es
   precisamente lo que antes no había forma de saber. La tabla de proveedores queda a la espera de que
   el dueño lo confirme, igual que los tres de nube.
2. **El locale exacto.** Apple no soporta `es-CO` y el helper cae a `es-CL` por idioma base, que es su
   diseño. Nadie ha comprobado si esa elección afecta la calidad para un hablante colombiano.
3. **Un input entrelazado o de 16 bits de verdad.** El código nuevo los maneja, pero el micrófono de
   este equipo entrega Float32 deinterleaved: esas dos ramas están escritas y sin ejecutar. Harían
   falta una interfaz externa o un dispositivo agregado.
