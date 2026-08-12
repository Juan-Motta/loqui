# Use cases — Azure OpenAI realtime transcription

Interface: **UI** (Ajustes → Conexiones → Azure) plus the provider's WebSocket boundary.

The native interface is a Wails WKWebView. A browser-only Playwright launch cannot exercise the Go
bindings injected into that view, so the established native driver is used:
`LOQUI_DEBUG_CONN_CLICK` dispatches the same DOM events as the user's controls and
`LOQUI_DEBUG_CONN_REPORT` records the settled, user-visible state. Fixed resource and deployment
sentinels are used; the driver cannot accept or report a credential.

## UC-AOAI-01 — choose Azure OpenAI and see the configuration it actually needs

- **Actor:** a macOS user who has an Azure OpenAI realtime transcription deployment.
- **Scenario:** the user opens the Azure connection and changes Service from Azure Speech to Azure
  OpenAI.
- **Interface:** UI. App root: `frontend`; native development view served internally from
  `http://127.0.0.1:9245` with Wails bindings.
- **Intent:** selecting the option must reveal the Azure OpenAI resource and deployment fields, use
  its independent key slot, and hide the unrelated Speech region controls.
- **Setup:** build this branch, close the installed single instance temporarily, and launch the dev
  app with fixed non-secret debug field values. Do not press Save or Test.
- **Steps:**
  1. Open Settings → Connections → Azure.
  2. Select `Azure OpenAI — realtime (gpt-realtime-whisper)`.
  3. Fill the fixed resource `loqui-debug-resource` and deployment `loqui-debug-whisper`.
- **Verification:** the selector shows Azure OpenAI; resource and deployment are visible; the report
  says `azureService:openai`, `azureKeySlot:azure-openai`, `openaiFieldsShown:true`,
  `speechFieldsShown:false`, and both fields are classified `filled`. The Test and Save actions are
  enabled. No key value appears in the screenshot or report.
- **Persistence:** N/A. This journey deliberately does not save, so the user's existing Azure Speech
  configuration remains unchanged.

## UC-AOAI-02 — the runtime speaks the Azure protocol, not the Azure Speech protocol

- **Actor:** a user who selects Azure OpenAI and starts dictation after configuring it.
- **Scenario:** Loqui opens an Azure OpenAI realtime transcription session and delivers final text.
- **Interface:** WebSocket boundary exercised by the provider integration test.
- **Intent:** prove the selected service uses Azure OpenAI's endpoint, authentication, session schema,
  audio format, commit lifecycle, and final-event handling.
- **Setup:** a local fake WebSocket server; no external account or credential.
- **Steps:**
  1. Construct the Azure OpenAI provider with a fixed deployment and test API key.
  2. Start it, send audio, stop it, and let the server acknowledge the session and return final
     transcription events.
- **Verification:** the request carries `api-key`, never a credential in the URL; the session uses
  the requested deployment, 24 kHz PCM and `turn_detection:null`; audio does not start before
  `session.updated`; non-empty buffers are committed; all outstanding finals are emitted before
  stop completes. Rejection and transcription-failure events return sanitized errors.
- **Persistence:** none; the fake server and provider are in-memory.

## UC-AOAI-03 — save and probe stay on the Azure OpenAI credential slot

- **Actor:** a user configuring Azure OpenAI without wanting to overwrite working Azure Speech.
- **Scenario:** the user saves or tests the Azure OpenAI form.
- **Interface:** Wails service boundary exercised with a temporary store, plus the UI source
  contract for the real Save/Test handlers.
- **Intent:** the service, resource, deployment, model language and secret must remain independent of
  Azure Speech and public OpenAI.
- **Setup:** a temporary settings directory containing an Azure Speech region/key and a public OpenAI
  model; fixed test credentials only.
- **Steps:**
  1. Save Azure OpenAI through `SaveAzureConnection` and reload the temporary store.
  2. Probe unsaved visible resource/deployment values through `TestAzureOpenAIConnection`.
  3. Build the active Azure provider and inspect which credential slot and adapter it uses.
- **Verification:** `azureService=openai`, resource/deployment and `azure-openai` secret persist;
  Azure Speech region/key and public `openAiModel` remain unchanged; validation failures write
  nothing; the probe succeeds only on `session.updated`; runtime constructs the Azure OpenAI audio
  provider rather than Azure Speech.
- **Persistence:** verified by reopening the temporary store. No real user setting is modified.

## UC-AOAI-04 — Home names the two Azure products explicitly

- **Actor:** a user choosing a dictation engine from Home.
- **Scenario:** the user opens the engine picker after configuring one or both Azure products.
- **Interface:** native Wails UI plus the Settings service boundary.
- **Intent:** avoid making the user infer whether a generic `Azure` choice means Speech or Azure
  OpenAI Realtime Whisper.
- **Setup:** open the branch build on Home. Service tests use temporary stores with independent
  Speech and Azure OpenAI configurations.
- **Steps:**
  1. Open the engine picker on Home.
  2. Inspect the `Azure Speech` and `Azure OpenAI Realtime Whisper` choices.
  3. Select each configured choice through the Settings service boundary.
- **Verification:** both products have their own visible option; the active one has the picker
  checkmark; readiness is evaluated from that product's own region/resource/deployment/key; an
  unconfigured inactive product is disabled; selecting either writes canonical `provider=azure`
  plus the matching `azureService=speech|openai` value atomically.
- **Persistence:** verified by reloading the temporary store. The visual inspection is read-only and
  does not change the real user profile.
