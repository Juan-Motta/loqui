# loqui-go

Dictado por voz para macOS: mantenés una tecla, hablás, y el texto aparece donde está el
cursor — en la terminal, en un correo, en cualquier app. Port a **Go + Wails v3** de
[Loqui](../loqui) (Electron + TypeScript).

Estado y siguiente paso: **`CONTINUITY.md`**. Diseño y mapa de módulos:
**`docs/plans/loqui-go-port.md`**.

## Lo primero que hay que saber

> **No uses `go` a secas en este repo.**

El binding de Go del Azure Speech SDK es cgo sobre la librería nativa y **no declara sus
propios `#cgo`**, así que la ruta de headers y los flags de enlace sólo pueden venir del
entorno. Go no tiene ningún archivo donde un proyecto pueda dejarlos fijados (`CGO_CFLAGS`
es por proceso), y el paquete que los necesita es el del SDK, no uno nuestro — poner
directivas `#cgo` en nuestro código no ayudaría.

Sin ellos, cualquier build que alcance el SDK muere así:

```
fatal error: 'speechapi_c_error.h' file not found
```

Incluso comandos que no tocan Azure, como `-mic-only`, porque el binario igual lo enlaza.

**Usá el wrapper**, que es donde viven los flags (y que descarga el framework la primera vez):

```bash
./scripts/go.sh test ./...
./scripts/go.sh run ./cmd/stt-probe -mic-only
. scripts/go.sh            # o sourcealo, y usá `go` normal en esa shell
```

## Comandos

```bash
wails3 task build            # compila (frontend + go)
wails3 task package          # arma bin/loqui.app y lo firma ad-hoc
wails3 task dev              # hot reload
wails3 task test             # tests
wails3 task vet

wails3 task probe:devices    # lista micrófonos
wails3 task probe:mic        # nivel del micrófono, sin tocar la red
SPEECH_KEY=... SPEECH_REGION=eastus wails3 task probe -- -seconds 20
```

Afordancias de desarrollo (documentadas donde se leen, no sólo acá):

```bash
LOQUI_DEBUG_OVERLAY=1 ./bin/loqui.app/Contents/MacOS/loqui   # muestra el pill a los 2s
LOQUI_DEBUG_DICTATE=6 ./bin/loqui.app/Contents/MacOS/loqui   # dicta 6s sin tocar una tecla
LOQUI_AZURE_KEY=...                                          # evita el Keychain (ver abajo)
```

## Setup en una máquina nueva

```bash
cd frontend && npm install && cd ..
./scripts/build-globe-listener.sh    # el listener de la tecla fn
wails3 task package
```

El framework de Azure lo baja `scripts/vendor-speech-sdk.sh` solo, con sha256 fijado.

## Permisos de macOS

- **Micrófono** — se pide solo la primera vez.
- **Accesibilidad** — sin esto el `Cmd+V` sintético se traga *en silencio*: el dictado
  transcribe y no aparece nada. La app lo avisa al arrancar.
- **Input Monitoring** — para la tecla `fn`, concedido al helper `globe-listener`.

**Con firma ad-hoc hay que re-concederlos en cada rebuild**, porque macOS ata los permisos a
la firma y ésta cambia cada vez. Peor: la lectura del Keychain **se cuelga** (de ahí el
timeout en `GetKey` y la escotilla `LOQUI_AZURE_KEY`). Firmar los builds de dev con una
identidad estable es el siguiente paso del proyecto, no una comodidad.

## Estructura

```
main.go, wiring.go      la app Wails: ventanas, tray, hotkey
internal/session/       el controlador de dictado (decisiones puras, con tests)
internal/stt/           contrato de proveedor + azure/
internal/audio/         captura (malgo/CoreAudio) + PCM/nivel
internal/inject/        paste con NSPasteboard.changeCount + focus guard
internal/store/         settings JSON + Keychain
internal/hotkey/        protocolo de la tecla fn + listener
helpers/                los 3 helpers nativos (Swift/C++), portados sin cambios
frontend/               index.html = Ajustes, overlay.html = el pill
cmd/stt-probe/          dictado desde la CLI, para aislar fallos
```
