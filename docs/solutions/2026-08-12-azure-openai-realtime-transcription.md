# Azure OpenAI appeared in the Azure selector but could not run

## Symptom

Azure Speech worked, but `Azure OpenAI — realtime (gpt-realtime-whisper)` could not be selected and
used from the Azure card. The option existed visually, yet its credential could not be saved or
tested and active Azure dictation always constructed Azure Speech.

## Root cause

The interface intentionally preserved a placeholder for an unported Azure subservice. Four layers
still enforced that placeholder state:

1. the Azure OpenAI credential slot was unavailable;
2. save and probe handlers supported only Azure Speech;
3. runtime key-slot resolution ignored the selected Azure service;
4. `buildProvider("azure")` always returned the Speech SDK provider.

There was also a configuration collision: public OpenAI read the Azure deployment field as its model.

## Fix

- Added an `internal/stt/azureopenai` adapter for Azure's GA realtime transcription endpoint. It uses
  `api-key` during the WebSocket handshake, sends the nested transcription session with the user's
  deployment, waits for `session.updated`, streams 24 kHz PCM, and performs bounded manual commits.
- Extended the shared OpenAI realtime lifecycle through explicit transport/session/commit seams,
  preserving public OpenAI defaults while accounting for every outstanding Azure final event.
- Made Azure service selection authoritative across storage, readiness, save, probe and runtime.
  Azure Speech and Azure OpenAI retain separate credentials and configuration.
- Added a dedicated public `openAiModel` setting so its model no longer depends on the Azure
  deployment name.
- Wired the Azure selector to swap forms and credential state live, and regenerated the Wails
  bindings for the new backend methods and fields.
- Split Home's generic Azure entry into `Azure Speech` and `Azure OpenAI Realtime Whisper`. Each
  option reports its own readiness and selecting it stores the canonical Azure provider plus the
  exact Azure subservice.

## Safety properties

- Resource names are validated and converted to a fixed Azure hostname; arbitrary URLs are refused.
- Credentials travel only in the `api-key` header and are never accepted by the debug driver or
  included in provider error prose/logs.
- A probe is green only after Azure acknowledges the session, not merely after HTTP 101.
- Empty audio is never committed; stop waits for all committed final transcripts within the existing
  bounded shutdown budget.
- Invalid save input performs no partial write. Azure Speech and public OpenAI settings are covered
  by neighbor regression tests.

## Verification

- Focused Go tests and race detector for `internal/stt/azureopenai`, `internal/stt/openai`,
  `internal/store` and `internal/app`.
- Frontend TypeScript check and Wails binding generation/build.
- Native UI E2E selecting Azure OpenAI with non-secret fixed values and visually confirming both
  explicit Azure products in Home; see
  `docs/e2e/reports/2026-08-12-azure-openai-realtime-transcription.md`.
- No live Azure green result is claimed. After the owner configured the resource, deployment and
  separate API key, the final Home verification remained read-only; the Test button is the next safe
  live check.
