# E2E — la fila del modelo de Whisper

VERDICT: PASS

**Lo último del encargo de fidelidad.** El contenedor `.model-box` estaba en el markup desde el
principio, copiado literal de Electron, y vacío: no había una sola línea que lo rellenara. Hasta hoy
la única forma de tener el modelo era `./scripts/build-whisper-stt.sh` — **el script hacía el trabajo
que debía hacer la interfaz**.

- **Feature:** estado del modelo de whisper.cpp, descarga reanudable con progreso, cancelar y eliminar
- **Branch:** `feat/whisper-model-row`
- **Run:** 2026-08-07T15:59–16:02-05:00 — cuatro lanzamientos
- **Build:** `bin/loqui.app`, reempaquetada de este árbol. 15 paquetes verdes, `gofmt` y `vet`
  limpios, `tsc --target es2022` limpio.

## Cómo se provocó cada estado, y por qué hubo que provocarlos

**En esta máquina el modelo ya está**, dos veces: dentro del bundle y como symlink de desarrollo al
proyecto Electron. Así que el estado que el usuario vería en una instalación nueva —"falta el
modelo"— es justamente el que aquí no ocurre nunca.

Se provocó **ocultando las copias y devolviéndolas**, y hubo un paso intermedio que enseña algo: con
sólo la del bundle oculta, la fila seguía diciendo `bundled:true`. No era un fallo — `HelperPath`
encuentra la copia de desarrollo, que es el orden que `WhisperModelPath` documenta. Hicieron falta
las dos.

Y una comprobación que valió la pena: al restaurar, mi verificación contó **una** copia de 465 MB en
lugar de dos. `helpers/bin/ggml-small.bin` pesa 80 bytes porque es un **symlink** al modelo del
proyecto Electron — 80 es la longitud de la ruta. Resuelto con `readlink`: apunta a los 487.601.967
bytes correctos. Nada roto, pero el susto justifica haberlo mirado en vez de asumirlo.

---

## UC-MODEL-01 — el modelo vino con la app: PASS

```
MODEL-ROW  boxes:1  bundled:true  ok:true  problem:  stateClass:ready
           buttons: model-get=absent model-stop=absent model-del=absent
```

- `stateClass:ready` — la clase la decide **Go**, no la página, igual que el badge de conexión: es lo
  que colorea la línea, y una página que la dedujera de la frase estaría reimplementando la decisión
  en otro idioma.
- **Los tres botones ausentes, y el que falta a propósito es Eliminar.** Una copia empaquetada no es
  nuestra para borrar: vino con el build y esta app no puede volver a descargarla *a esa ruta* — la
  siguiente descarga aterrizaría en la carpeta de datos, dejando al usuario sin el archivo que tenía
  y sin explicación. El backend lo rechaza además por su cuenta, no sólo la UI.

## UC-MODEL-02 — instalación nueva, no hay modelo: PASS

Las dos copias ocultas.

```
MODEL-ROW  bundled:false  ok:false  problem:missing  stateClass:warn
           buttons: model-get=shown model-stop=absent model-del=absent
```

Aparece **sólo** Descargar. Es el estado que hasta hoy no tenía salida por la interfaz.

## UC-MODEL-03 — descarga interrumpida: PASS

Un parcial de 120 MB en la ruta de datos, con las copias ocultas.

```
MODEL-ROW  bundled:false  ok:false  problem:incomplete  stateClass:warn
           buttons: model-get=shown
```

`incomplete` es **un veredicto propio y no un sabor de "corrupto"**, y esa distinción es toda la
razón de ser de la fila: uno se reanuda y el otro no. El botón dice *Reanudar descarga*, no
*Descargar* — decir "descargar" ahí sugiere empezar de cero, que es exactamente lo que reanudar
existe para evitar.

**Limpieza:** las dos copias devueltas a su sitio y el parcial borrado. Verificado tras la corrida.

---

## Lo que sostiene esto en tests, y no en esta corrida

Los caminos que **no** se ejercitaron contra la app son los que cuestan 465 MB, y están cubiertos por
tests contra un **servidor HTTP real** —no un cliente simulado—, porque lo que se prueba es un
`Range`, un 206 y bytes añadidos a un archivo que ya tenía algunos:

| Test | Qué fija |
| --- | --- |
| descarga completa | el archivo en disco es lo que el servidor mandó, y el veredicto queda ok |
| **reanudar** | el servidor ve **exactamente un** `Range` — no empezó de cero — y el archivo resultante es correcto |
| digest incorrecto | se reporta **y el archivo se borra**: si se quedara, whisper lo cargaría y transcribiría basura, que se lee como un bug de esta app y no como una descarga mala |
| progreso | se reporta, es **monótono** (una barra que retrocede parece un fallo) y el último es 100 |
| eliminar | borra de verdad, y el veredicto vuelve a `missing` |

Y las reglas puras, en `internal/store/model.go`, **verificadas por mutación**: aceptar `unverified`
como ok, dejar de comparar el digest, o quitar el acotado del porcentaje — las tres mutaciones matan
su test y sólo el suyo.

El modelo está **fijado por tamaño Y digest**. El tamaño solo aceptaría un archivo truncado y
rellenado, o un mirror sirviendo otra cosa del largo justo.

## Lo que este informe NO cubre

1. ~~**Una descarga real de 465 MB.**~~ **EJECUTADA — ver abajo.**
2. **Cancelar, contra la app.** El botón y su ruta existen y el servicio los expone; provocarlo en
   vivo exige interrumpir una descarga real a mitad, y la que se hizo terminó en 55 s.
3. **La segunda caja.** El original dibuja en *cada* `.model-box` y menciona dos —Conexiones y el
   tutorial—; este markup tiene una, y el reporte dice `boxes:1`. Es fidelidad del markup heredado, no
   de este código: el módulo ya recorre todas las que encuentre.

---

## La revisión cruzada del diff — codex: 0 P0, 7 P1, 3 P2

Todos verificados contra el código; **los diez arreglados**. Y el primero deja en evidencia que sin
esta ronda la feature habría sido decorativa.

### El que hacía inútil todo lo demás

`build/darwin/Taskfile.yml` copiaba **cada** archivo de `helpers/bin/` al bundle, y el script de
whisper pone el modelo ahí. Resultado medido: el `.app` pesaba **497 MB** y `WhisperModelPath`
encontraba siempre una copia empaquetada, **así que el descargador no se ejercitaba nunca — ni en
producción ni en desarrollo.** La fila existía y no servía para nada.

Excluido el modelo del copiado:

```
antes:  497M  bin/loqui.app
ahora:   32M  bin/loqui.app
con LOQUI_BUNDLE_MODEL=1:  497M   (escape para una build personal sin red)
```

Y con el bundle de 32 MB, la fila hace lo que debe: `problem:missing`, `model-get=shown`.

### El arreglo estructural, que cerró tres hallazgos de una vez

La descarga escribía **directamente en la ruta canónica**. Todo lo demás en la app juzga el modelo por
esa ruta —`dictation.go` por existencia, `provider_fallback.go` por tamaño— así que un archivo parcial
o sin verificar ahí significa **lanzar Whisper contra basura**, y la basura se lee como un bug de esta
app, no como una descarga mala. Un crash entre el último byte y el hash dejaba un archivo del tamaño
justo sin verificar, y `status()` decía "listo".

Ahora los bytes aterrizan en `<ruta>.part` y el **rename ocurre sólo tras cuadrar el digest**. Eso
convierte en *sólido* el atajo de `status()` de no hashear en cada repintado — antes era un riesgo
disfrazado de optimización. Un crash deja un parcial reanudable, nunca un modelo falso.

### Los otros

- **Un 206 desalineado se aceptaba.** Un proxy con caché podía devolver un tramo que empieza en otro
  sitio, pegando un prefijo bueno a un sufijo equivocado. Ahora se compara el `Content-Range`.
- **Un 200 con Range ignorado se añadía al parcial**, produciendo un archivo del largo correcto hecho
  de bytes equivocados — que el digest atrapa, pero 465 MB más tarde. Ahora reinicia.
- **Cancelar no llegaba a la petición.** Un canal comprobado entre lecturas no interrumpe un servidor
  que se cuelga antes de las cabeceras: Cancelar reportaba éxito mientras la transferencia seguía ahí
  para siempre. Ahora va por `context`.
- **El ETA usaba los bytes que ya estaban en disco.** Reanudar 400 MB y recibir 1 MB en un segundo se
  leía como 401 MB/s, así que un minuto de trabajo se anunciaba como "0 s".
- **`Bundled` pisaba el veredicto.** Una copia de desarrollo truncada se describía como lista y la
  fila escondía Descargar *y* Eliminar: Whisper fallaba y no había salida.
- **El progreso se construía en español** y la fila lo pinta literal, así que una sesión en inglés
  mostraba una línea en español. Era la única cadena de esta feature que la página no podía arreglar.
- **`O_NOFOLLOW`** al abrir el parcial: la ruta es nuestra, pero un symlink plantado ahí haría que este
  código truncara lo que apuntara.

### Y la crítica que más duele, porque era sobre mis tests

*"Sólo modelas un servidor cooperativo cuyo Content-Range coincide exactamente."* Cierto: no había
cobertura de rangos ignorados, 206 desalineados, archivos locales demasiado grandes, cancelación ni
concurrencia — **así que los seis fallos de arriba estaban en verde**. Seis casos nuevos, todos contra
un servidor HTTP real, incluidos uno que se cuelga a mitad para probar Cancelar y otro que verifica
que la ruta canónica está **vacía** mientras la descarga corre.

---

## UC-MODEL-04 — la descarga real, 465 MB desde Hugging Face: PASS

Pedida por el dueño. **Demuestra la única cosa que ningún test podía**: que el sha256 fijado en el
código es el del archivo que la URL sirve **hoy**. Un test contra un servidor local prueba el
mecanismo; sólo esto prueba el *pin*.

Conducida pulsando **el botón de la fila**, no el binding por detrás — un probe que se saltara la fila
verificaría el descargador y no diría nada del control que el usuario aprieta.

```
16:24:13  MODEL-CLICK  asked:download found:true
16:24:13  MODEL        download requested
16:24:13  MODEL-ROW    downloading:true  model-get=absent  model-stop=shown
          en disco:    ggml-small.bin.part   (248 MB a los 25 s; ~10 MB/s)
16:25:08  MODEL        download complete and verified
16:25:08  MODEL-ROW    ok:true  stateClass:ready  model-del=shown  model-stop=absent
          en disco:    ggml-small.bin  487.601.967 bytes   (sin .part residual)
```

**Verificado por fuera, no sólo por el propio código:**

```
shasum -a 256 del archivo descargado: 1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b
fijado en internal/store/model.go:    1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b
```

Y lo que sólo una corrida real enseña: **la ruta canónica estuvo vacía durante toda la transferencia**.
En disco había únicamente `ggml-small.bin.part`. Esa es la garantía por la que se reestructuró el
descargador — sin ella `dictation.go`, que juzga por existencia, podía lanzar Whisper contra un
archivo a medias.

### Y Whisper dictó con ese modelo

Con el symlink de desarrollo **oculto**, es decir con el modelo descargado como única copia:

```
16:27:28  STT-ERR  whisper_init_state: kv cross size = 56.62 MB
16:27:28  STT-ERR  whisper_init_state: compute buffer (encode) = 39.45 MB
          "falta el modelo": 0 apariciones
16:27:33  CTRL     stopEngine gen=1        MIC  peak level this session: 0.10
```

Las líneas `STT-ERR` son el stderr de whisper.cpp, donde registra su arranque — no son errores. El
pico de 0.10 es silencio: nadie habló, así que no hay transcripción que mostrar. Lo que queda probado
es que el modelo descargado **se encuentra y se carga**.

**Un tropiezo, por si vuelve:** la primera corrida dictó con Azure. `LOQUI_DEBUG_DICTATE` disparó un
segundo antes de que el cambio de motor aterrizara, así que el motor guardado seguía siendo el
anterior cuando arrancó el engine. Hay que dejar whisper guardado ANTES, no cambiarlo en la misma
corrida.

**Estado devuelto:** symlink de desarrollo restaurado, motor en `azure`, idioma en `en`, credenciales
con el mismo sha256. El modelo descargado se deja en la carpeta de datos — es una copia legítima e
independiente del proyecto Electron, y borrarla gastaría el ancho de banda otra vez para nada.
