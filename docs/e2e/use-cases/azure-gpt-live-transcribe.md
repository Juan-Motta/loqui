# Use cases — Azure GPT Live Transcribe

The settings interface is a native Wails WKWebView. A standalone browser does not receive its Go
bindings, so the repository's native connection-card driver dispatches the same select and button
events a user reaches and reports only settled visible classifications. It cannot accept or report
a credential value.

## UC-AZLIVE-01 — choose and persist GPT Live Transcribe

- **ID:** UC-AZLIVE-01
- **Actor:** a macOS user with an Azure OpenAI `gpt-live-transcribe` deployment.
- **Scenario:** the user changes the Azure OpenAI base model while keeping the deployment name as a
  separate field.
- **Interface:** UI. App root: `frontend`. App URL: N/A — the production-shaped native build serves
  embedded assets inside Wails' WKWebView. Persistence mechanism: `server` (Loqui's settings service
  and persisted store).
- **Intent:** make the model choice explicit and ensure Home names the selected Azure model.
- **Setup:** build this branch, temporarily close the installed single instance, and launch the
  development app against the owner's existing Azure configuration. Do not supply a credential to
  the driver.
- **Steps:**
  1. Open Settings → Connections → Azure and select `Azure OpenAI — realtime`.
  2. Select `gpt-live-transcribe` from Model and press Save through the real form handler.
  3. Close and relaunch the development app, then inspect Azure Settings and Home.
- **Verification:** the Azure card shows `gpt-live-transcribe`; resource and deployment remain
  separate fields; the settled report says `azureOpenAiModel:gpt-live-transcribe`; Home shows
  `Azure OpenAI GPT Live Transcribe`; no credential value appears in logs or screenshots.
- **Persistence:** after a full native app relaunch, the UI shows `gpt-live-transcribe` again from
  the backend settings payload.

## UC-AZLIVE-02 — Azure accepts the selected model session

- **ID:** UC-AZLIVE-02
- **Actor:** the same user verifying the configured Azure deployment before dictating.
- **Scenario:** the user presses Test connection with Live Transcribe selected.
- **Interface:** UI plus the Azure WebSocket reached by the real Wails binding. App root: `frontend`.
  App URL: N/A — the native build uses embedded WKWebView assets. Persistence mechanism: `server`
  (the previously saved model remains selected).
- **Intent:** prove the configured Azure resource accepts the Live Transcribe session payload, not
  merely that the local form can store the choice.
- **Setup:** UC-AZLIVE-01 has persisted the model; the existing stored Azure credential is resolved
  by the app. The driver sends an empty key field and never reads or logs the stored value.
- **Steps:**
  1. Relaunch the branch app and open Azure OpenAI Settings with the saved Live Transcribe model.
  2. Press Test connection through the real button handler.
  3. Wait for the bounded probe to settle and inspect the visible status plus card report.
- **Verification:** the UI shows a successful Azure OpenAI connection verdict; the card still shows
  `gpt-live-transcribe`; logs classify the submitted key field as empty/masked rather than exposing
  a value; no raw credential or server prose is captured.
- **Persistence:** after the probe and another app repaint, the model remains
  `gpt-live-transcribe`; the probe itself writes nothing.

## UC-AZLIVE-03 — both model dialects remain valid locally

- **ID:** UC-AZLIVE-03
- **Actor:** a user switching between an existing Realtime Whisper deployment and Live Transcribe.
- **Scenario:** Loqui configures the same Azure realtime lifecycle with the language shape required
  by the selected base model.
- **Interface:** CLI test runner against a local WebSocket server.
- **Intent:** preserve Realtime Whisper while adding Live Transcribe and keep Azure's deployment name
  independent from the model choice.
- **Setup:** local in-memory WebSocket server and fixed non-secret values; no external credential.
- **Steps:**
  1. Run the Azure adapter suite for Realtime Whisper and inspect its session update.
  2. Run it for Live Transcribe, wait for `session.updated`, and inspect the same session boundary.
- **Verification:** both runs use the supplied deployment and 24 kHz/manual-commit lifecycle;
  Realtime Whisper emits singular `language`; Live Transcribe emits plural `languages` and never
  singular `language`; an unknown model or empty deployment is rejected before dialing.
- **Persistence:** N/A; the local server and provider are in-memory and write no user state.
