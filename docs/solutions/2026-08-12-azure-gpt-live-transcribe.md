# Azure GPT Live Transcribe model selection

## Problem

Loqui's Azure OpenAI path was built for `gpt-realtime-whisper`. It persisted only an Azure
deployment name, always emitted singular `language`, and always labeled the active Home engine as
Realtime Whisper. That made it impossible to correctly configure `gpt-live-transcribe`, whose
Realtime transcription contract uses plural `languages`.

## Solution

Persist the Azure OpenAI base model independently from the user-defined Azure deployment. Older
settings default to `gpt-realtime-whisper`; only the two supported model identifiers are considered
ready. Save, probe and runtime validate the model before reading credentials or dialing.

Both models reuse the existing Azure WebSocket transport, 24 kHz audio, explicit buffer commits,
readiness and transcript lifecycle. The session builder branches only where the protocol differs:

- Realtime Whisper writes optional singular `language`.
- GPT Live Transcribe writes optional plural `languages` and never singular `language`.
- Azure's deployment name remains the value sent in the transcription `model` field.

Settings exposes a model dropdown beside the separate deployment input, and Home names the exact
active Azure OpenAI model. Azure Speech and public OpenAI configuration remain unchanged.

## Verification

- Model migration, readiness and rejection paths are covered in settings/store tests.
- Session payloads, manual commits and local WebSocket lifecycles are covered for both dialects.
- Save, probe and runtime propagation are covered through the application service tests.
- Frontend bindings, exact labels and model restoration are covered by the UI contract tests and
  TypeScript checking.
- The native E2E run persisted the model across a restart and the owner's Azure deployment accepted
  a real `gpt-live-transcribe` probe using the stored credential without exposing it.

Evidence: `docs/e2e/reports/2026-08-12-azure-gpt-live-transcribe.md`.

## Deliberate limits

This change does not expose GPT Live Transcribe prompts, keyword hints or transcription-delay
controls. Those are independent product choices and can be added later without duplicating the
transport or inferring a base model from an arbitrary deployment name.
