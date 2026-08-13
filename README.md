![Loqui — real-time multilingual dictation for macOS](docs/assets/loqui-banner.png)

English · [Español](README.es.md)

# Loqui

Real-time multilingual dictation for macOS.

## What is Loqui?

Loqui turns your voice into text in any macOS app. Hold the configured key, speak, and Loqui inserts the transcription wherever your cursor is. You can use an on-device engine for privacy or connect an optional cloud engine when you want its language or realtime capabilities.

## Download

[**Download the latest Loqui release**](https://github.com/Juan-Motta/loqui/releases/latest)

The current release is **v0.2.0**:

- App: [Loqui-0.2.0-macos-arm64.dmg](https://github.com/Juan-Motta/loqui/releases/download/v0.2.0/Loqui-0.2.0-macos-arm64.dmg)
- Checksum: [Loqui-0.2.0-macos-arm64.dmg.sha256](https://github.com/Juan-Motta/loqui/releases/download/v0.2.0/Loqui-0.2.0-macos-arm64.dmg.sha256)

Updater-enabled releases also publish a signed `Loqui-<version>-macos-arm64.zip` and its `SHA256SUMS` manifest alongside the DMG.

Loqui requires an Apple Silicon Mac. The app declares macOS 14 as its minimum runtime version, while public releases are currently tested on macOS 26. Apple Speech is available only on macOS 26 or newer.

## Features

- Dictates directly into the app where your cursor is focused.
- Supports multilingual and realtime speech engines.
- Keeps recent transcriptions in a searchable local history.
- Offers local engines for private, offline-friendly use.
- Supports optional cloud engines when you want their models and capabilities.
- Lets you switch engines without changing your dictation workflow.

## Supported engines

| Engine | Runs | Best for | Notes |
| --- | --- | --- | --- |
| Whisper | Locally | Private, offline transcription | Requires a one-time model download from **Settings → Connections**. |
| Apple Speech | On device | Native macOS transcription | Requires macOS 26 or newer. |
| Azure Speech | Cloud | Multilingual continuous dictation | Requires an Azure Speech key and region. |
| Azure OpenAI Realtime | Cloud | Streaming transcription with your Azure deployment | Supports deployments backed by `gpt-realtime-whisper` or `gpt-live-transcribe`; requires an Azure OpenAI resource and key. |
| xAI / Grok | Cloud | Realtime transcription | Requires an xAI API key. |
| OpenAI Realtime | Cloud | Realtime transcription | Requires an OpenAI API key. |
| ElevenLabs | Cloud | Realtime transcription | Requires an ElevenLabs API key. |

## Install and start dictating

1. Download the DMG from the [latest release](https://github.com/Juan-Motta/loqui/releases/latest).
2. Open the DMG and copy **Loqui** to **Applications**.
3. Launch Loqui. If macOS blocks the first launch, open **System Settings → Privacy & Security** and choose **Open Anyway**.
4. Choose a speech engine in **Settings → Connections** and configure it if it needs an API key or region. If you choose Whisper, select **Download model** there and let the one-time download finish.
5. Grant the macOS permissions requested by the app.
6. Place the cursor in any text field, hold the configured dictation key, speak, and release the key.

Whisper's approximately **465 MB** model must be downloaded once from **Settings → Connections**; the download is resumable. Apple Speech may automatically download the selected language model on first use. Neither model is bundled in the DMG.

## Automatic updates

Loqui can check for new signed releases from GitHub without interrupting dictation. Checks are enabled by default and run at most once every 24 hours while the app is open. A check never installs anything on its own: when an update is available, Loqui shows the version and waits for your confirmation before downloading and installing it.

You can trigger a check from **About → Updates** or the tray menu, install a confirmed update there, and restart Loqui when macOS is ready. To opt out of scheduled checks, turn off **Settings → System → Automatic update checks**; manual checks remain available. Updates use a notarized ZIP release containing the signed `Loqui.app`, while the DMG remains available for a fresh installation.

## macOS permissions

- **Microphone** lets Loqui hear and transcribe you.
- **Accessibility** lets Loqui paste the transcription into the focused app. Without it, transcription can succeed while the synthetic `Cmd+V` is silently blocked.
- **Input Monitoring** lets the `globe-listener` helper detect the `fn`/Globe key.

Open **System Settings → Privacy & Security** to review these grants. Ad-hoc development builds must receive the permissions again after every rebuild because macOS binds them to the changing signature; see Development below.

## Privacy and API keys

Whisper and Apple Speech process audio locally. Azure Speech, Azure OpenAI Realtime, xAI/Grok, OpenAI Realtime, and ElevenLabs send audio to the selected provider, subject to that provider's terms and privacy policy. Loqui uses only the engine you select.

Stored provider keys live at `~/Library/Application Support/LoquiGo/secrets.json`, with file mode `0600`, in cleartext. This keeps the file readable only by your macOS user but does not encrypt its contents. Enable FileVault to protect the keys at rest if the Mac or its storage is lost.

For temporary development sessions, these provider-specific environment variables take precedence and avoid storing the matching credential in `secrets.json`:

- `LOQUI_AZURE_KEY`
- `LOQUI_GROK_KEY`
- `LOQUI_OPENAI_KEY`
- `LOQUI_AZURE_OPENAI_KEY`
- `LOQUI_ELEVENLABS_KEY`

Each variable applies only to its matching provider. `LOQUI_AZURE_KEY` is for Azure Speech; `LOQUI_AZURE_OPENAI_KEY` is for the separate Azure OpenAI realtime resource and deployment.

For Azure OpenAI, select the base model separately from the deployment name. Azure deployment names
are user-defined and do not have to match `gpt-realtime-whisper` or `gpt-live-transcribe`.

## Development

The normal development path uses the repository wrappers so native dependencies and pinned tooling are configured consistently.

### Requirements

- An Apple Silicon Mac.
- **Go 1.25 or newer**.
- **Node.js and npm**. Local development does not currently pin a Node.js version; the release Action uses Node.js 24.
- **CMake**.
- **Xcode 26**, or matching Command Line Tools, providing the **macOS 26 SDK**.
- Network access for npm packages and first-use native dependencies.

The source-build requirement is stricter than the app's declared macOS 14 runtime floor because the `macos-stt` helper targets `arm64-apple-macos26.0`.

### Run locally

```bash
git clone https://github.com/Juan-Motta/loqui.git
cd loqui
cd frontend && npm install && cd ..
./scripts/task.sh dev
```

The development task starts Wails with hot reload. Grant Microphone, Accessibility, and Input Monitoring when macOS asks.

### Build the app

```bash
./scripts/task.sh build
./scripts/task.sh package
```

`build` compiles the frontend and Go application. `package` also compiles the native helpers and assembles an ad-hoc-signed `bin/loqui.app`. The task wrapper resolves the pinned Wails CLI before either task runs and fetches verified native dependencies when needed.

### Useful commands

```bash
./scripts/task.sh dev            # run with hot reload
./scripts/task.sh build          # compile the frontend and Go application
./scripts/task.sh package        # compile helpers and create ad-hoc-signed bin/loqui.app
./scripts/task.sh test           # run Go tests
./scripts/task.sh vet            # run Go static checks
./scripts/task.sh typecheck      # check frontend TypeScript
./scripts/task.sh check          # run the complete local verification gate
./scripts/task.sh release:macos  # build a notarized maintainer release
./scripts/task.sh probe:devices  # list microphones
./scripts/task.sh probe:mic      # inspect microphone levels locally
```

### Troubleshooting

Do not use bare `go` for this repository: the Azure Speech SDK needs cgo paths and linker flags supplied by `scripts/go.sh`. Commands that reach the SDK can otherwise fail with `fatal error: 'speechapi_c_error.h' file not found`. Use the wrappers instead:

```bash
./scripts/go.sh test ./...
./scripts/go.sh run ./cmd/stt-probe -mic-only
./scripts/task.sh build     # resolves the pinned wails3 CLI automatically
```

With ad-hoc signing, Microphone, Accessibility, and Input Monitoring must be **re-granted after every rebuild** because the app signature changes. A stable development signing identity avoids that churn.

Use the focused probes and UI affordances to isolate failures:

```bash
./scripts/go.sh run ./cmd/stt-probe -mic-only
XAI_API_KEY=... ./scripts/go.sh run ./cmd/stt-probe -provider grok
SPEECH_KEY=... ./scripts/go.sh run ./cmd/stt-probe
LOQUI_DEBUG_OVERLAY=1 ./bin/loqui.app/Contents/MacOS/loqui
LOQUI_DEBUG_DICTATE=6 ./bin/loqui.app/Contents/MacOS/loqui
```

### Project structure

```text
main.go, wiring.go      Wails app: windows, tray, and hotkey
internal/session/       dictation controller and tests
internal/stt/           speech-provider contract and engines
internal/audio/         microphone capture and audio levels
internal/inject/        safe paste into the focused app
internal/store/         settings and credential storage
internal/hotkey/        fn/Globe-key protocol and listener
helpers/                native Swift and C++ helpers
frontend/               Settings, History, and dictation overlay
cmd/stt-probe/          command-line diagnostics
```

See [CONTINUITY.md](CONTINUITY.md) for the current project status and [docs/plans/loqui-go-port.md](docs/plans/loqui-go-port.md) for the port and module map.

## Maintainer releases

<details>
<summary>GitHub release automation and Developer ID setup</summary>

**Local Developer ID release**

Releases for other Macs require two identities and notarization credentials; ordinary ad-hoc packages do not.

1. In Xcode **Settings → Accounts → Manage Certificates**, create an **Apple Development** certificate for stable daily builds.
2. Create or download a **Developer ID Application** certificate from the active Apple developer account. In Keychain Access, confirm its private key appears beneath the certificate.
3. Export the Developer ID certificate and private key once as an encrypted `.p12`, and store it outside this repository in the owner's encrypted backup.
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

The password is intentionally omitted: `notarytool` prompts securely and validates the credentials before saving them. Verify the setup without copying identity hashes or credentials into reports:

```bash
security find-identity -v -p codesigning
xcrun notarytool history --keychain-profile loqui-notary --output-format json >/dev/null
```

Run the complete local gate immediately before the release entry point:

```bash
./scripts/task.sh check
LOQUI_NOTARY_PROFILE=loqui-notary ./scripts/task.sh release:macos
```

Success publishes `bin/release/Loqui-<version>-macos-arm64.dmg` and the signed updater bundle `Loqui-<version>-macos-arm64.zip` only after Developer ID signing, notarization, stapling, Gatekeeper assessment, and atomic publication all pass.

**GitHub release automation**

The **Release** Action is manual-only and releases the version already merged into `main`. It uses the protected Environment `release`; it never edits `build/config.yml`, moves an existing tag, or deletes ambiguous remote state.

One-time setup:

1. Export the **Developer ID Application** identity with its private key from Keychain Access as an encrypted `.p12`. Validate it locally with `/usr/bin/openssl pkcs12 -noout`; Apple's parser accepts the legacy algorithms Keychain Access may emit.
2. Copy its base64 form without writing encoded material into this repository:

   ```bash
   base64 -i /absolute/path/DeveloperID.p12 | pbcopy
   ```

   In a private temporary directory, verify that decoding produces a non-empty `.p12`. Never paste the archive, its base64 form, or its password into logs, issues, commits, or pull requests.
3. In App Store Connect, create a **Team API key** with notarization access and download its one-time `.p8`. Keep the Key ID and Issuer ID. Individual API keys are intentionally unsupported because the workflow uses the Team-key `--issuer` contract.
4. Create Environment `release`, restrict deployment branches to exactly `main`, add a required reviewer, and leave **Prevent self-review** disabled while this remains a single-maintainer repository. This provides operator confirmation, not separation of duties.
5. Add these five Environment secrets, not repository secrets:

   - `MACOS_CERTIFICATE_P12_BASE64`
   - `MACOS_CERTIFICATE_PASSWORD`
   - `APP_STORE_CONNECT_API_KEY_P8`
   - `APP_STORE_CONNECT_KEY_ID`
   - `APP_STORE_CONNECT_ISSUER_ID`

Allow the protected release job to request `contents: write`; the preflight job stays read-only and cannot access Environment secrets. Draft Releases are checked during the protected job's revalidation because GitHub does not expose drafts to the read-only token.

For each release, change the quoted stable version in `build/config.yml` through a normal PR, then run:

```bash
./scripts/patch-plists.sh
./scripts/task.sh check
```

After that PR merges, open **Actions → Release → Run workflow**, select `main`, inspect the secret-free preflight, and approve the waiting Environment deployment. The repository-wide concurrency group serializes releases: do not park an approval indefinitely or dispatch multiple replacement runs, because GitHub can cancel an older pending run.

On success, verify that the tag targets the recorded commit and that the public Release contains exactly the DMG, its `.sha256`, the updater ZIP, `SHA256SUMS`, and generated notes. Success evidence and any available sanitized notarization-failure evidence are kept as 14-day Actions artifacts, never as public Release assets.

If publication reports ambiguous residual state, inspect before changing anything:

```bash
gh release view vX.Y.Z
git ls-remote --tags origin refs/tags/vX.Y.Z
```

The Action never deletes. Only when inspection proves a partial and unannounced draft/tag may a maintainer deliberately run `gh release delete vX.Y.Z --cleanup-tag --yes` and dispatch again. Never delete a public release; supersede a bad public build with a new patch version.

</details>
