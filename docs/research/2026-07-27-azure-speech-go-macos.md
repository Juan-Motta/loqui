# Research — Azure Speech desde Go en macOS arm64 (spike de riesgo)

- **Fecha:** 2026-07-27
- **Pregunta:** ¿puede un backend Go reproducir la ruta Azure Speech de Loqui
  (endpoint universal v2 + LID continuo + reconocimiento continuo) en macOS arm64,
  con el audio entrando por push stream en vez del micrófono del SDK?
- **Por qué es la primera pregunta:** es la única dependencia del port que no tiene
  equivalente puro en Go. Todo lo demás (WebSockets, helpers nativos, UI) es
  mecánico. Si esto no funciona, la arquitectura del port cambia.
- **Veredicto: VIABLE — verificado ejecutando.**

## Contexto: por qué se dudaba

El binding oficial [`Microsoft/cognitive-services-speech-sdk-go`](https://github.com/microsoft/cognitive-services-speech-sdk-go)
es cgo sobre la librería nativa del Speech SDK. La documentación de instalación
cubre Linux y Windows; macOS no está documentado y hay issues abiertas que
reportan justo eso:

- [#66](https://github.com/microsoft/cognitive-services-speech-sdk-go/issues/66) — `fatal error: 'speechapi_c_error.h' file not found` en Mac
- [#72](https://github.com/microsoft/cognitive-services-speech-sdk-go/issues/72) — "Finish installation documentation for macs"
- [#102](https://github.com/microsoft/cognitive-services-speech-sdk-go/issues/102) — "Unable to run on Mac Again!"

La hipótesis de las issues es que en macOS el SDK se distribuye como
`MicrosoftCognitiveServicesSpeech.xcframework` (pensado para Xcode/ObjC) y que por
eso no habría headers C para cgo.

## Hallazgo 1 — el xcframework SÍ trae los headers C

`https://aka.ms/csspeech/macosbinary` → `MicrosoftCognitiveServicesSpeech-MacOSXCFramework-1.51.1.zip`
(3.8 MB comprimido). Dentro de
`MicrosoftCognitiveServicesSpeech.xcframework/macos-arm64_x86_64/MicrosoftCognitiveServicesSpeech.framework`:

| Qué | Detalle verificado |
| --- | --- |
| `Headers/` | 191 headers, de los cuales **117 son `speechapi_c_*.h`** — exactamente los que cgo necesita, incluido `speechapi_c_auto_detect_source_lang_config.h` |
| Binario | `Versions/A/MicrosoftCognitiveServicesSpeech`, Mach-O universal **arm64 + x86_64**, 10.3 MB |
| Install name | `@rpath/MicrosoftCognitiveServicesSpeech.framework/Versions/A/MicrosoftCognitiveServicesSpeech` |
| Símbolos LID | presentes: `create_auto_detect_source_lang_config_from_languages`, `recognizer_create_speech_recognizer_from_auto_detect_source_lang_config` |

Es decir: las issues describen un problema de *documentación*, no de disponibilidad.
El paquete macOS tiene todo lo necesario; sólo hay que apuntar cgo al framework.

**Versión:** el binding Go y el framework macOS están en paridad — ambos `1.51.1`.

## Hallazgo 2 — el SDK Go no fija flags de cgo, así que se pasan por entorno

Los archivos del SDK sólo declaran `#include <speechapi_c_*.h>`; no hay directivas
`#cgo LDFLAGS`. Los flags se inyectan desde afuera:

```sh
FW=<ruta>/MicrosoftCognitiveServicesSpeech.xcframework/macos-arm64_x86_64
export CGO_CFLAGS="-I$FW/MicrosoftCognitiveServicesSpeech.framework/Headers"
export CGO_LDFLAGS="-F$FW -framework MicrosoftCognitiveServicesSpeech -Wl,-rpath,$FW"
go build ./...
```

Compila y enlaza limpio en `darwin/arm64` (Go 1.26.5, Xcode 26.6). `otool -L` sobre
el binario resultante confirma la dependencia por `@rpath`.

## Hallazgo 3 — la configuración de Loqui se reproduce entera, incluido el push stream

El spike (`spike-azure/main.go`, ver "Reproducir" abajo) replica lo que hace
`src/engine/engine.ts` del proyecto Electron, con una diferencia deliberada: el
audio entra por **push stream** en vez de por el micrófono del SDK, porque en el
port es Go quien captura y todos los proveedores reciben los mismos frames PCM16.

Salida real de la ejecución (sin credenciales):

```
  [1] SpeechConfig created from the universal v2 endpoint (dylib loaded)
  [2] LanguageIdMode=Continuous set
  [3] AutoDetectSourceLanguageConfig over [es-CO en-US]
  [4] push audio stream at 16000Hz/16bit/mono
  [5] SpeechRecognizer built (LID + push stream)
  [6] callbacks wired (started/partial/final/canceled/stopped)
  <- started  session= a174cd5534d14849adfd3ed0b14069e7
  [7] continuous recognition started
  -> pushing 187500 bytes of PCM from /tmp/loqui-spike.wav
  <- canceled reason=Error errorCode=AuthenticationFailure details="WebSocket upgrade
     failed: Authentication error (401). Please check subscription information and
     region name. SessionId: a174cd5534d14849adfd3ed0b14069e7"
  <- stopped
```

Lo que esto prueba, paso por paso:

1. La dylib **carga en runtime** (paso 1 es la primera llamada al framework).
2. `LanguageIdMode = "Continuous"` y `AutoDetectSourceLanguageConfig` sobre
   `[es-CO, en-US]` se **aceptan** — la ruta LID existe en el binding Go.
3. El **push stream a 16 kHz/16-bit/mono se acepta** como `AudioConfig`, que es
   la pieza que permite mover la captura a Go.
4. El reconocimiento continuo **arranca y abre sesión** (`SessionId` real).
5. Se **conecta de verdad al endpoint v2** y Azure contesta un 401 legítimo.
6. El SDK entrega **`errorCode=AuthenticationFailure`**: el mismo código
   estructurado del que depende `sessionPolicy.classifyCancel` en Electron para
   decidir reintentar vs. rendirse. **La política de reconexión porta 1:1.**

## Lo que NO está verificado

- **Transcripción real y cambio de idioma en vivo.** Requiere una key válida. El
  spike queda listo para eso: `SPEECH_KEY=... SPEECH_REGION=... ./spike file.wav`.
  Hay un WAV de prueba bilingüe generado con `say` en `/tmp/loqui-spike.wav`.
  Riesgo bajo: es el mismo servicio, el mismo endpoint y las mismas propiedades
  que el SDK JS ya usa en producción; lo que el spike no cubre es comportamiento
  del servicio, no del binding.
- **La key de Azure del proyecto Electron está marcada como expuesta** y pendiente
  de regenerar (ver `CONTINUITY.md` de `loqui`). No se usó aquí.
- **Empaquetado.** La dylib de 10.3 MB tendrá que ir dentro del `.app`, firmada, con
  el `@rpath` apuntando a `Contents/Frameworks`, y pasar notarización. No probado
  todavía; es trabajo conocido, no incógnita. Ver el plan.
- **Tamaño del binario.** El spike pesa 3 MB + 10.3 MB de framework.

## Consecuencias para el port

1. La ruta Azure Speech **se queda en Go**. No hace falta ni conservar un webview
   oculto con el SDK JS, ni reimplementar el protocolo WebSocket de Azure a mano.
2. **Go es dueño de la captura de audio** para *todos* los proveedores (un solo
   camino de PCM16 → cada proveedor). Esto elimina la ventana `engine` oculta de
   Electron y su dependencia de `getUserMedia` en el webview, que en WKWebView es
   territorio dudoso.
3. El framework hay que **vendorizarlo con un script**, igual que `loqui` hace con
   whisper.cpp (`scripts/build-whisper-stt.sh`): descargar, verificar y colocar.
   No se commitea la dylib.
4. `CGO_ENABLED=1` obligatorio → **sin cross-compilación**; el build de macOS se
   hace en macOS. Ya era el caso por los helpers Swift.

## Reproducir

El spike vive en el scratchpad de la sesión (no en el repo, todavía):
`spike-azure/main.go`. Se moverá a `internal/stt/azure` como base del proveedor
real durante la fase 1.
