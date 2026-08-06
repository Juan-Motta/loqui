# loqui-go

Voice dictation for macOS: hold a key, speak, and the text appears wherever the cursor is —
in the terminal, in an email, in any app. A **Go + Wails v3** port of
[Loqui](../loqui) (Electron + TypeScript).

Status and next step: **`CONTINUITY.md`**. Design and module map:
**`docs/plans/loqui-go-port.md`**.

## The first thing to know

> **Do not use bare `go` in this repo.**

The Go binding for the Azure Speech SDK is cgo over the native library and **declares no
`#cgo` directives of its own**, so the header path and link flags can only come from the
environment. Go has no file where a project can record them (`CGO_CFLAGS` is per-process),
and the package that needs them is the SDK's, not ours — putting `#cgo` lines in our own
code would not help.

Without them, any build that reaches the SDK dies like this:

```
fatal error: 'speechapi_c_error.h' file not found
```

Even commands that never touch Azure, like `-mic-only`, because the binary still links it.

**Use the wrapper**, which is where the flags live (and which fetches the framework on first
use):

```bash
./scripts/go.sh test ./...
./scripts/go.sh run ./cmd/stt-probe -mic-only
. scripts/go.sh            # or source it, and use plain `go` in that shell
```

## The second thing: `wails3` is not on your PATH

`wails3` installs into `$(go env GOPATH)/bin`, which on macOS is **not on the PATH** unless
you put it there. So `wails3 task ...` fails with `command not found` on a machine that has
everything correctly installed.

Use the wrapper, which also installs it if missing:

```bash
./scripts/task.sh build
./scripts/task.sh probe:mic
```

Or fix it at the root and use `wails3` directly everywhere:

```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc && exec zsh
```

## Commands

```bash
./scripts/task.sh build          # compiles (frontend + go)
./scripts/task.sh package        # builds bin/loqui.app and signs it ad-hoc
./scripts/task.sh dev            # hot reload
./scripts/task.sh test
./scripts/task.sh vet

./scripts/task.sh probe:devices  # lists microphones
./scripts/task.sh probe:mic      # microphone level, without touching the network
SPEECH_KEY=... SPEECH_REGION=eastus ./scripts/task.sh probe -- -seconds 20
```

With `wails3` on the PATH, `wails3 task <the-same>` is equivalent.

Development affordances (documented where they are read, not only here):

```bash
LOQUI_DEBUG_OVERLAY=1 ./bin/loqui.app/Contents/MacOS/loqui   # shows the pill after 2s
LOQUI_DEBUG_DICTATE=6 ./bin/loqui.app/Contents/MacOS/loqui   # dictates 6s without a keypress
LOQUI_AZURE_KEY=...                                          # bypasses the Keychain (see below)
LOQUI_GROK_KEY=...                                           # same, for xAI
```

There is one escape hatch per provider (`LOQUI_AZURE_KEY`, `LOQUI_GROK_KEY`,
`LOQUI_OPENAI_KEY`, `LOQUI_AZURE_OPENAI_KEY`, `LOQUI_ELEVENLABS_KEY`). One does **not** work
for another provider, deliberately: dictating against the wrong service with the wrong
credential is worse than not dictating.

To isolate a failure without the app, `cmd/stt-probe` runs a dictation from the CLI against
whichever provider you name:

```bash
./scripts/go.sh run ./cmd/stt-probe -mic-only                        # is the microphone giving audio?
XAI_API_KEY=... ./scripts/go.sh run ./cmd/stt-probe -provider grok    # xAI, 15s
SPEECH_KEY=... ./scripts/go.sh run ./cmd/stt-probe                   # Azure (the default)
```

## Setup on a fresh machine

```bash
cd frontend && npm install && cd ..
./scripts/build-globe-listener.sh    # the fn-key listener
./scripts/task.sh package            # installs wails3 if missing
```

The Azure framework is fetched by `scripts/vendor-speech-sdk.sh` on its own, with a pinned
sha256.

## macOS permissions

- **Microphone** — asked for once, on first use.
- **Accessibility** — without it the synthetic `Cmd+V` is swallowed *silently*: dictation
  transcribes and nothing appears. The app says so at launch.
- **Input Monitoring** — for the `fn` key, granted to the `globe-listener` helper.

**With ad-hoc signing these have to be re-granted on every rebuild**, because macOS ties
permissions to the signature and it changes each time. Worse: the Keychain read **hangs**
(hence the timeout in `GetKey` and the `LOQUI_*_KEY` escape hatches). Signing dev builds
with a stable identity is the project's next step, not a convenience.

## Structure

```
main.go, wiring.go      the Wails app: windows, tray, hotkey
internal/session/       the dictation controller (pure decisions, with tests)
internal/stt/           provider contract + azure/
internal/audio/         capture (malgo/CoreAudio) + PCM/level
internal/inject/        paste with NSPasteboard.changeCount + focus guard
internal/store/         settings JSON + Keychain
internal/hotkey/        fn-key protocol + listener
helpers/                the 3 native helpers (Swift/C++), ported unchanged
frontend/               index.html = Settings, overlay.html = the pill
cmd/stt-probe/          dictation from the CLI, to isolate failures
```
