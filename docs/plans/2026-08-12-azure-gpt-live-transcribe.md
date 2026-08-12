# Plan: add Azure GPT Live Transcribe

## Goal

Let a user select an Azure OpenAI deployment backed by `gpt-live-transcribe`, save it independently
from `gpt-realtime-whisper`, test the connection, and dictate through the existing realtime Azure
path with the correct model-specific language payload.

Research: `docs/research/2026-08-12-azure-gpt-live-transcribe.md`.

## Intent and constraints

- Preserve existing Azure Speech and Azure OpenAI Realtime Whisper configurations.
- Keep the Azure base model separate from the Azure deployment name.
- Keep credentials, endpoint validation, 24 kHz audio, readiness and manual commit behavior unchanged.
- Use plural `languages` only for `gpt-live-transcribe`; do not send singular `language` to it.
- Make Home identify the selected Azure OpenAI model accurately.
- Do not add prompt, keyword or transcription-delay settings in this iteration.
- Treat a live Azure green result as unverified until the configured resource accepts a probe or
  dictation; public Microsoft documentation does not yet list this model.

## Approach comparison

| Approach | Complexity | Blast radius | Reversibility | Time to validate | Correctness / user risk |
| --- | --- | --- | --- | --- | --- |
| **A. Persist an explicit base model beside the deployment and branch only the session language shape** | Low-medium | Settings payload, Azure adapter and two UI labels | High | Low; deterministic payload tests cover the protocol fork | Low; model and deployment retain their distinct Azure meanings |
| **B. Infer the model from the deployment name** | Low | Azure session builder only | High | Very low | High; Azure allows arbitrary deployment names, so inference is not reliable |
| **C. Create a second provider for Live Transcribe** | High | Duplicate lifecycle, probe and UI paths | Medium | High | High; near-identical realtime implementations can drift |

### Choice

Choose **A**. The transport and lifecycle are shared; only the transcription configuration differs.
An explicit base-model setting is the smallest representation that remains correct when an Azure
deployment name differs from its model.

## Implementation units

### 1. Model contract and migration

- Add Azure OpenAI model constants and validation for `gpt-realtime-whisper` and
  `gpt-live-transcribe`.
- Persist `azureOpenAiModel` separately from `azureOpenAiDeployment`.
- Default an absent model field to `gpt-realtime-whisper` so older settings retain their behavior.
- Treat an unknown persisted model as unconfigured in connection readiness and reject it before
  runtime reads a credential or constructs a provider.
- Carry the model through the bootstrap payload and generated frontend bindings.

### 2. Model-specific wire payload

- Extend the Azure session builder to receive both base model and deployment.
- Continue writing the deployment to `session.audio.input.transcription.model`.
- For Realtime Whisper, preserve optional singular `language`.
- For Live Transcribe, emit optional plural `languages` with the configured language as its only
  entry; omit it for automatic detection.
- Preserve `turn_detection: null`, 24 kHz PCM and manual commits.

### 3. Runtime and probe propagation

- Pass the stored model through dictation provider construction.
- Pass the selected model through the connection probe, using the same payload builder as runtime.
- Reject unsupported model values before writing settings, reading credentials or dialing Azure.
- Save model, resource, deployment and service in the same settings transaction.

### 4. User interface and copy

- Rename the Azure service option to model-neutral `Azure OpenAI — realtime`.
- Add an Azure model selector with both supported models while retaining the free-text deployment.
- Paint the saved model, submit it on Save and Test, and map backend validation errors to the model
  control.
- Make Home display `Azure OpenAI GPT Realtime Whisper` or
  `Azure OpenAI GPT Live Transcribe` according to the saved selection.
- Update English translations and user documentation for both Azure models.

### 5. Verification and documentation

- Run focused unit tests, race tests, frontend type checking/build and the full project check.
- Cross-review the design and implementation under the standard profile.
- Run the native user journey: select Live Transcribe, save it, verify Home's exact model label and
  use the configured Azure probe without exposing credentials.
- Record the verified result and any Azure-side limitation in the E2E report and solution note.

## TDD proof matrix

- **Migration RED:** an old settings file without `azureOpenAiModel` resolves to Realtime Whisper.
- **Validation RED:** an unknown Azure model is refused before any key or settings mutation.
- **Readiness RED:** an unknown persisted Azure model cannot be reported connected or active and
  runtime rejects it before reading a credential.
- **Wire RED:** Live Transcribe sends the deployment plus plural `languages` and no singular
  `language`; Realtime Whisper preserves the singular field.
- **Runtime RED:** Azure provider construction propagates the stored base model independently from
  the deployment.
- **Probe RED:** the probe uses the selected model's payload and rejects unknown models without a
  dial.
- **UI RED:** the Azure form exposes both models, saves/tests the chosen model, restores it from the
  payload and gives Home an exact model label.
- **GREEN neighbors:** Azure Speech, public OpenAI and existing Azure Realtime Whisper behavior stay
  unchanged.

## Acceptance criteria

1. Settings offers `gpt-realtime-whisper` and `gpt-live-transcribe` as Azure OpenAI models.
2. The base model and deployment are saved independently; existing settings default to Realtime
   Whisper.
3. Unknown model values are rejected before save, probe or runtime use and cannot appear ready.
4. Runtime and connection testing send the deployment name using the selected model's valid
   language schema.
5. Selecting Live Transcribe never sends singular `language`; selecting Realtime Whisper retains
   its current payload.
6. Home identifies the selected Azure OpenAI model rather than always saying Realtime Whisper.
7. Azure Speech and public OpenAI behavior remain unchanged.
