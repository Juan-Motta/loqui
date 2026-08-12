# E2E — Azure GPT Live Transcribe

VERDICT: PASS

- **Feature:** select, persist, test and use Azure OpenAI `gpt-live-transcribe`
- **Branch:** `feat/azure-gpt-live-transcribe`
- **Run:** 2026-08-12T17:20-17:25-05:00
- **Build:** `bin/loqui.dev.app`, built from this branch with
  `./scripts/task.sh darwin:build DEV=true` and launched through the native Wails app
- **Credential handling:** the existing `azure-openai` credential was resolved from storage. Its
  value was never read by the driver, placed in an environment variable, printed or captured.

## Why this is not Playwright

Loqui's settings UI runs inside a native Wails WKWebView and calls generated Go bindings. A
standalone browser receives neither those bindings nor the native application lifecycle, and this
repository does not depend on `@playwright/test`. The established native driver therefore emits
the same DOM change/click events as the model selector and connection buttons, while the application
reports only masked field state and settled user-visible classifications.

## UC-AZLIVE-01 — choose and persist GPT Live Transcribe: PASS

The native form selected Azure OpenAI, chose both the `gpt-live-transcribe` base model and the
owner-confirmed deployment of the same name, then invoked the real Save handler with the credential
field empty. The app classified the field as masked and blocked it from entering the request:

```text
KEY-SENT    action=save kind=masked-blocked provider=azure
CONN-CLICK  ran=set-service(openai) | set-model(live) | set-deployment(live) | save(asis)
            azureOpenAiModel=gpt-live-transcribe deploymentField=filled
UI-ACTION   action=saveConnection(azure) ok=true notice="Azure configuration saved"
ENGINE-OPTS azure-openai="Azure OpenAI GPT Live Transcribe"
```

After terminating the process and launching the app again without a write action, bootstrap and
the settled card independently restored the choice:

```text
ENGINE-OPTS azure-openai="Azure OpenAI GPT Live Transcribe"
CONN-CARD   azureService=openai azureOpenAiModel=gpt-live-transcribe
            deploymentField=filled keyField=masked
```

The Home selector visibly showed `Azure OpenAI GPT Live Transcribe`; local screenshot evidence is
kept in the ignored file `.workflow/e2e-run/azure-gpt-live-selection.png`. The resource, deployment,
base model and credential remain separate UI concepts.

## UC-AZLIVE-02 — Azure accepts the selected model session: PASS

The app was relaunched with no credential input and the real Test connection button was pressed.
The request used the persisted `gpt-live-transcribe` deployment and stored credential, completed the
Azure WebSocket handshake, submitted the Live Transcribe session update and waited for
`session.updated`:

```text
KEY-SENT    action=test kind=masked-blocked provider=azure
CONN-CLICK  ran=test(asis) azureOpenAiModel=gpt-live-transcribe keyField=masked
PROBE       slot=azure-openai resource=[configured] deployment=gpt-live-transcribe source=stored
PROBE-DONE  slot=azure-openai ok=true code=
UI-PROBE    provider=azure ok=true
CONN-CARD   status="✓ Conexión correcta: Azure OpenAI aceptó la clave y el deployment"
```

This closes the research uncertainty: although Microsoft's public model list did not yet name the
model when checked, the owner's Azure resource accepted the actual Live Transcribe session dialect.
The probe sent no audio and performed no settings write.

## UC-AZLIVE-03 — both model dialects remain valid locally: PASS

The focused Go suites, including local WebSocket fixtures, passed normally and with the race
detector. They assert that both models keep Azure's deployment in the transcription `model` field,
24 kHz PCM and manual commits while using different language shapes:

- `gpt-realtime-whisper`: optional singular `language`, never plural `languages`.
- `gpt-live-transcribe`: optional plural `languages`, never singular `language`.
- automatic language detection omits either hint; unsupported models and empty deployments fail
  before dialing.

Commands:

```text
./scripts/go.sh test -race ./internal/settings ./internal/store ./internal/stt/openai \
  ./internal/stt/azureopenai ./internal/app ./internal/i18n
./scripts/task.sh typecheck
CI=true ./scripts/task.sh check
```

The full project gate also covered all Go packages, vet, TypeScript and the macOS release-script
contract suite.

## Diagnostic observations

- `darwin:run` packages the existing `bin/loqui`; it does not rebuild it. The first diagnostic
  launch therefore exposed an older frontend asset and was rejected as evidence. The branch was
  explicitly rebuilt with `darwin:build DEV=true` before the PASS runs above.
- An initial probe showed that the user's previously stored deployment still named Realtime
  Whisper. It was not accepted as Live Transcribe evidence. The native form then saved the confirmed
  `gpt-live-transcribe` deployment and the final probe above ran against that exact value.
- The installed `/Applications/Loqui.app` was closed only to avoid Wails' single-instance conflict
  and reopened after verification; it was not modified.
