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
./scripts/task.sh typecheck      # los tipos del frontend, con el TypeScript del propio repo
./scripts/task.sh check          # las tres de arriba, que es lo que hay que correr antes de subir

./scripts/task.sh probe:devices  # lists microphones
./scripts/task.sh probe:mic      # microphone level, without touching the network
SPEECH_KEY=... SPEECH_REGION=eastus ./scripts/task.sh probe -- -seconds 20
```

With `wails3` on the PATH, `wails3 task <the-same>` is equivalent.

## Developer ID release setup (one time)

Releases for other Macs require two identities and notarization credentials; ordinary ad-hoc
packages do not. Configure them without putting secrets in this repository or shell history:

1. In Xcode **Settings → Accounts → Manage Certificates**, create an **Apple Development**
   certificate for stable daily builds.
2. Create or download a **Developer ID Application** certificate from the Apple developer account.
   In Keychain Access, confirm its private key appears beneath the certificate.
3. Export the Developer ID certificate plus private key once as an encrypted `.p12`, and store it
   outside this repository in the owner's encrypted backup.
4. Create an app-specific Apple password, then store it through `notarytool`'s interactive prompt:

```bash
printf 'Apple ID: '
IFS= read -r APPLE_ID
printf 'Apple Team ID: '
IFS= read -r TEAM_ID
xcrun notarytool store-credentials loqui-notary \
  --apple-id "$APPLE_ID" --team-id "$TEAM_ID"
unset APPLE_ID TEAM_ID
```

The password is intentionally omitted; `notarytool` prompts securely and validates the credentials
before saving them. Verify setup without copying identity hashes or credentials into reports:

```bash
security find-identity -v -p codesigning
xcrun notarytool history --keychain-profile loqui-notary --output-format json >/dev/null
```

Run the complete local gate immediately before the release entry point:

```bash
./scripts/task.sh check
LOQUI_NOTARY_PROFILE=loqui-notary ./scripts/task.sh release:macos
```

Success publishes `bin/release/Loqui-<version>-macos-arm64.dmg` only after Developer ID signing,
notarization, stapling, Gatekeeper assessment, and atomic publication all pass.

## GitHub release automation

The **Release** Action is manual-only and releases the version already merged into `main`. It uses
the protected Environment `release`; it never edits `build/config.yml`, moves an existing tag, or
deletes ambiguous remote state.

One-time setup:

1. Export the **Developer ID Application** identity with its private key from Keychain Access as an
   encrypted `.p12`. Validate it locally with `/usr/bin/openssl pkcs12 -noout`; Apple's parser accepts
   the legacy algorithms Keychain Access may emit.
2. Copy its base64 form without writing encoded material into this repository:

   ```bash
   base64 -i /absolute/path/DeveloperID.p12 | pbcopy
   ```

   In a private temporary directory, also verify that decoding produces a non-empty `.p12`. Never
   paste the archive, its base64 form, or its password into logs, issues, commits, or pull requests.
3. In App Store Connect, create a **Team API key** with notarization access and download its one-time
   `.p8`. Keep the Key ID and Issuer ID. Individual API keys are intentionally unsupported because
   the workflow uses the Team-key `--issuer` contract.
4. Create Environment `release`, restrict deployment branches to exactly `main`, add a required
   reviewer, and leave **Prevent self-review** disabled while this remains a single-maintainer
   repository. This provides an operator confirmation, not separation of duties.
5. Add these five Environment secrets (not repository secrets):

   - `MACOS_CERTIFICATE_P12_BASE64`
   - `MACOS_CERTIFICATE_PASSWORD`
   - `APP_STORE_CONNECT_API_KEY_P8`
   - `APP_STORE_CONNECT_KEY_ID`
   - `APP_STORE_CONNECT_ISSUER_ID`

   Allow the workflow's protected release job to request `contents: write`; the preflight job stays
   read-only and cannot access the Environment secrets. Draft Releases are checked during the
   protected job's revalidation because GitHub does not expose drafts to the read-only token.

For each new release, change the quoted stable version in `build/config.yml` through a normal PR,
then run:

```bash
./scripts/patch-plists.sh
./scripts/task.sh check
```

After that PR merges, open **Actions → Release → Run workflow**, select `main`, inspect the
secret-free preflight, and approve the waiting Environment deployment. The repository-wide
concurrency group serializes releases: do not park an approval indefinitely or dispatch multiple
replacement runs, because GitHub can cancel an older pending run. On success, verify that the tag
targets the recorded commit and that the public Release has exactly the DMG, its `.sha256`, and
generated notes. Success evidence and any available sanitized notarization-failure evidence are
kept as 14-day Actions artifacts, never as public Release assets.

If publication reports ambiguous residual state, inspect before changing anything:

```bash
gh release view v0.1.0
git ls-remote --tags origin refs/tags/v0.1.0
```

The Action never deletes. Only when inspection proves a partial and unannounced draft/tag may a
maintainer deliberately run `gh release delete v0.1.0 --cleanup-tag --yes` and dispatch again. Never
delete a public release; supersede a bad public build with a new patch version.

Development affordances (documented where they are read, not only here):

```bash
LOQUI_DEBUG_OVERLAY=1 ./bin/loqui.app/Contents/MacOS/loqui   # shows the pill after 2s
LOQUI_DEBUG_DICTATE=6 ./bin/loqui.app/Contents/MacOS/loqui   # dictates 6s without a keypress
LOQUI_AZURE_KEY=...                                          # bypasses the stored keys (see below)
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

Install Go, Node/npm, CMake, and the Xcode command-line tools, then run:

```bash
cd frontend && npm install && cd ..
./scripts/task.sh package
```

`package` orchestrates every repo-owned build step: it installs the pinned `wails3` when needed,
fetches the Azure framework with its pinned sha256, and builds every native helper plus its required
dylibs before assembling the app bundle.

## macOS permissions

- **Microphone** — asked for once, on first use.
- **Accessibility** — without it the synthetic `Cmd+V` is swallowed *silently*: dictation
  transcribes and nothing appears. The app says so at launch.
- **Input Monitoring** — for the `fn` key, granted to the `globe-listener` helper.

**With ad-hoc signing these have to be re-granted on every rebuild**, because macOS ties
permissions to the signature and it changes each time. Signing dev builds with a stable
identity would end that, and it remains the honest fix.

It used to be worse: the Keychain read **hung** on these builds, so the app could not read
its own API key. That is why the credentials no longer live in the Keychain — see below.

## Where the API keys live

`~/Library/Application Support/LoquiGo/secrets.json`, mode `0600`, **in the clear**.

This is a deliberate trade for a personal build, not an oversight. The keys used to live in the
login Keychain, which encrypted them with the account password — but on an ad-hoc-signed build
`SecItemCopyMatching` never returns (macOS wants to authorise the access and cannot show the
prompt), so the app could not read its own credential and could not dictate. A three-second
timeout made that diagnosable, not fixed.

What you give up, stated plainly: anything running as your user can read the file, and so can a
backup or a lost laptop. **Turning FileVault on** restores encryption at rest and costs one
toggle; without it the keys sit in cleartext on the disk. These credentials bill by the hour, so
a leak is somebody else's invoice.

The escape hatches still win over the file, so `LOQUI_AZURE_KEY=... ` is the way to run without
storing anything at all.

To go back to OS-level protection, the route is a stable signing identity rather than a different
file format: that fixes the hang at its source and stops macOS revoking Accessibility and Input
Monitoring on every rebuild too.

## Structure

```
main.go, wiring.go      the Wails app: windows, tray, hotkey
internal/session/       the dictation controller (pure decisions, with tests)
internal/stt/           provider contract + azure/
internal/audio/         capture (malgo/CoreAudio) + PCM/level
internal/inject/        paste with NSPasteboard.changeCount + focus guard
internal/store/         settings JSON + credentials file
internal/hotkey/        fn-key protocol + listener
helpers/                the 3 native helpers (Swift/C++), ported unchanged
frontend/               index.html = Settings, overlay.html = the pill
cmd/stt-probe/          dictation from the CLI, to isolate failures
```
