# E2E — Azure OpenAI realtime transcription

VERDICT: PASS

- **Feature:** make the Azure card's Azure OpenAI realtime option selectable and runnable
- **Branch:** `fix/azure-openai-whisper-model`
- **Run:** 2026-08-12, America/Bogota
- **Build:** `bin/loqui.dev.app` built from this working tree
- **Use cases:** `docs/e2e/use-cases/azure-openai-realtime-transcription.md`

## Environment and scope

The installed `/Applications/Loqui.app` was temporarily closed because Loqui's single-instance ID
otherwise focuses the installed release and prevents the branch build from opening. It was restored
after the run. The dev server was stopped and its one pre-existing orphan helper was cleaned up.

The first settings bootstrap reported presence only, never values:

```text
azure-speech=present azure-openai=absent openai=present grok=present elevenlabs=present
```

Therefore a **live Azure call is not claimed**: at that point this machine had no stored Azure OpenAI
credential. The UI journey and complete wire/runtime journey were still executed with fixed
non-secret values and a local WebSocket server. No Save or Test action was pressed in the real user
profile.

## UC-AOAI-01 — select Azure OpenAI: PASS

The corrected native app was opened at Settings → Connections → Azure. The settled UI report was:

```text
azureService:openai
azureKeySlot:azure-openai
openaiFieldsShown:true
speechFieldsShown:false
resourceField:filled
deploymentField:filled
keyField:empty
test:shown/enabled
save:shown/enabled
```

The screenshot `.workflow/e2e-run/azure-openai-selection-final.png` was visually inspected. It shows
`Azure OpenAI — realtime (gpt-realtime-whisper)`, the Azure OpenAI resource and deployment fields,
and an unconfigured independent key field. No credential is displayed. The earlier screenshot that
showed Azure Speech was rejected as evidence: it came from the installed single instance, not the
branch build.

**Persistence:** N/A by design. The fixed resource/deployment values were unsaved form state and the
debug driver has no Save action in this run.

## UC-AOAI-02 — Azure wire and dictation lifecycle: PASS

The focused Azure/OpenAI integration suite passed normally and under the race detector. It verifies:

- exact Azure hostname/path and `intent=transcription` query;
- `api-key` handshake authentication with no secret in the URL;
- nested transcription session using the configured deployment, PCM 24 kHz and
  `turn_detection:null`;
- readiness only after `session.updated`;
- non-empty manual commits, all outstanding final events before stop, both supported final-text
  fields, and a visible sanitized transcription failure.

**Persistence:** none; local in-memory WebSocket fixture.

## UC-AOAI-03 — save, probe and runtime select the same product: PASS

Service tests with temporary stores prove the Azure OpenAI save writes only its service/resource/
deployment/key, the dedicated probe evaluates the visible unsaved values and waits for
`session.updated`, and the active Azure runtime constructs the Azure OpenAI audio provider using the
`azure-openai` slot. Neighbor tests prove Azure Speech and public OpenAI keep their own key,
region/model and runtime behavior.

**Persistence:** reloaded from temporary stores; the real profile remained untouched.

## UC-AOAI-04 — explicit Home choices: PASS

After the owner configured the independent Azure OpenAI slot, a second read-only native run opened
the Home engine picker. Presence-only diagnostics reported both Azure slots as present and the
rendered option inventory included:

```text
azure-speech=Azure Speech
azure-openai=Azure OpenAI Realtime Whisper
```

The screenshot `.workflow/e2e-run/azure-home-explicit-selection.png` was visually inspected. It
shows both choices simultaneously and a checkmark beside `Azure OpenAI Realtime Whisper`, matching
the stored Azure subservice. No credential or credential value appears. Focused service tests prove
that choosing `Azure Speech` stores `provider=azure, azureService=speech`, choosing Azure OpenAI
stores `provider=azure, azureService=openai`, and each option has independent readiness.

**Persistence:** the real picker was only opened and inspected. Selection persistence was verified
against temporary stores.

## Playwright applicability

The project UI is a native Wails WKWebView. Loading the Vite URL in a standalone browser does not
inject the Go bindings used by Settings, so Playwright cannot execute this journey faithfully and is
not installed in this frontend. The native driver dispatches the actual select/input events and
records the visible settled card state; this is the same approach used by the project's existing
connection-card E2E reports.

## Residual external validation

A real Azure OpenAI green probe/dictation remains outside this run: no live Test or dictation was
started. The endpoint, schema, event lifecycle, save, selection and error paths are deterministic
tests in this branch. With the resource, `gpt-realtime-whisper` deployment and separate API key now
configured, the UI's Test button is the safe first live check.
