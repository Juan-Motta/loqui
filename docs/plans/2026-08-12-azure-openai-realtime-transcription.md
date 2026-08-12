# Plan: make Azure OpenAI realtime transcription usable

## Goal

Make the existing Azure card's `Azure OpenAI — realtime` service option genuinely usable with a
user's Azure OpenAI resource, `gpt-realtime-whisper` deployment, and separate Azure OpenAI key,
without changing the working Azure Speech path.

Research: `docs/research/2026-08-12-azure-openai-realtime-transcription.md`.

## Intent and constraints

- Preserve Azure Speech behavior, region, credential, and language settings.
- Treat Azure OpenAI's key as a separate credential and honor `LOQUI_AZURE_OPENAI_KEY`.
- Treat the Azure deployment as a user-defined name, not a fixed model enum.
- Keep public OpenAI's selected model separate from the Azure deployment name.
- Never place a credential in a URL, event payload, user-visible provider prose, or log.
- Keep audio production non-blocking and preserve the bounded lifecycle/reconnect behavior.
- A card must not report active or connected unless the exact selected Azure subservice has all of
  its required configuration.

## Approach comparison

| Approach | Complexity | Blast radius | Reversibility | Time to validate | Correctness / user risk |
| --- | --- | --- | --- | --- | --- |
| **A. Reuse the OpenAI realtime lifecycle through explicit Azure transport and commit seams** | Medium — one lifecycle gains bounded configuration points plus a thin Azure adapter | Medium — shared realtime code, but each new seam has provider-specific tests | High — Azure adapter and optional settings can be removed independently | Medium — fake WebSocket tests exercise both dialects locally | Low — one audio/session implementation, no duplicated fixes |
| **B. Copy the OpenAI provider into a dedicated Azure package** | High — duplicate queue, resampling, lifecycle, decode, timeouts, and cleanup | High — future fixes can land in only one copy | Medium — isolated removal is easy, divergence is not | Medium — a second full provider suite is required | High — near-identical concurrent code is likely to drift |
| **C. Remove/hide the Azure OpenAI option** | Low | Low | High | Low | High — honest UI, but it does not satisfy the requested functionality |

### Choice

Choose **A**. The protocols share the 24 kHz PCM, nested GA session payload, audio append messages,
and transcription events. The differences are narrow and testable: endpoint, handshake auth,
readiness acknowledgement, and manual commit policy. Copying the lifecycle would preserve less
behavior with more code; hiding the option only restates the current limitation.

## Implementation units

### 1. Pin the Azure wire contract in tests

- Add an `internal/stt/azureopenai` adapter with tests written first for:
  - strict resource-name validation and the exact GA `intent=transcription` WebSocket URL;
  - `api-key` handshake header with no credential in the URL or logs;
  - the user's deployment name in the nested transcription payload;
  - `turn_detection: null`, 24 kHz PCM, `session.updated`, completed/delta events, and sanitized
    error classification;
  - completed events whose text arrives as either `text` or `transcript`, plus
    `conversation.item.input_audio_transcription.failed` as a terminal provider error.
- Use a local fake WebSocket; no external network or real credential in unit tests.

### 2. Extend the shared realtime lifecycle minimally

- Add explicit configuration for service label, auth mode, readiness acknowledgement, and manual
  commit interval; defaults must preserve public OpenAI behavior.
- For Azure, wait for `session.updated` before emitting Started.
- Track whether remote audio is uncommitted. Commit only non-empty buffers every three seconds and
  once on stop; account for outstanding commits so stop cannot drop a late final. A failed
  transcription settles its own outstanding commit and ends with a sanitized error; stop with no
  uncommitted audio and no outstanding commit ends immediately instead of waiting for a timeout.
- Keep the existing bounded audio queue, resampling, read limit, stop budget, transcript
  accumulation, and error sanitization unchanged.

### 3. Make Azure runtime and storage agree

- Dispatch `provider == "azure"` by `Settings.AzureService`: the existing Speech recognizer for
  `speech`, the Azure OpenAI adapter for `openai`.
- Require resource, deployment, and the `azure-openai` key for the OpenAI subservice; use its own
  language slot and environment override.
- Make runtime key-slot resolution depend on the selected Azure service, then mark
  `azure-openai` storable only when that runtime reader exists.
- Add a separate `OpenAiModel` setting for public OpenAI so changing an Azure deployment cannot
  silently change the public OpenAI model.

### 4. Wire save, probe, and the Azure card

- Add one backend save operation for Azure OpenAI resource, deployment, and credential. Validate all
  inputs before any write; write the credential first and report partial failure precisely if the
  subsequent settings write fails.
- Add an Azure OpenAI probe that uses the same endpoint/header/payload as dictation and succeeds only
  after `session.updated`; it must distinguish missing fields, rejected keys, deployment/config
  errors, timeouts, and network failures without echoing server prose containing secrets.
- Make the service selector live. Switching it changes the visible fields, key slot/state, actions,
  and language slot. Saving persists the chosen service; repaint restores stored truth after failure.
- Wire the public OpenAI model dropdown to its own setting.
- Regenerate the checked-in Wails TypeScript bindings after adding backend methods/fields and make
  the frontend typecheck prove their signatures match.
- Extend the existing safe debug affordance with fixed, non-secret service/resource/deployment
  actions and report classifications for user-path E2E.

### 5. Verify and document

- Update English/Spanish copy and README statements only after the runtime path is green.
- Record symptom, root cause, fix, and verification in `docs/solutions/`.
- Run focused Go tests, frontend typecheck/build, the complete project check, cross-engine code
  review, and `verify-e2e`.
- If a complete existing Azure OpenAI configuration is present, run one live dictation/probe without
  printing the credential. Otherwise mark live-provider E2E blocked by missing external credentials
  while still running the full local UI/wire journey.

## TDD proof matrix

- **RED runtime:** Azure configured for OpenAI must construct a provider reading
  `azure-openai`, not Azure Speech or fallback to Whisper.
- **RED wire:** the fake server must receive `api-key`, no OpenAI subprotocol credential,
  `intent=transcription`, the exact deployment, 24 kHz PCM, and `turn_detection: null`.
- **RED lifecycle:** no Started/audio before `session.updated`; non-empty periodic commits occur;
  empty commits never occur; all outstanding final transcripts survive stop; zero-work stop returns
  immediately; both completed-text field variants and `transcription.failed` are handled.
- **RED settings:** Azure OpenAI save persists service/resource/deployment/key together; rejected
  input changes none of them; public OpenAI model remains independent.
- **RED probe:** wrong/missing resource or deployment never dials; key/config refusal cannot be a
  false green; a confirmed session is a green result.
- **RED UI contract:** the Azure OpenAI option is enabled, selection reveals the right fields, uses
  the right slot, and a failed backend action repaints stored truth.
- **GREEN neighbors:** all existing Azure Speech and public OpenAI tests remain unchanged in outcome.

## Acceptance criteria

1. The Azure service dropdown can select Azure OpenAI and no longer labels it unavailable.
2. Saving a valid resource name, deployment name, and Azure OpenAI key preserves the Azure Speech
   configuration and makes the Azure card ready.
3. Selecting Azure as the active engine with service `openai` uses the Azure OpenAI endpoint,
   deployment, language slot, and credential; it never constructs Azure Speech.
4. Dictation sends 24 kHz PCM, becomes ready only after `session.updated`, commits manually, and
   surfaces partial/final transcript events without losing the final tail on stop. A failed
   transcription is visible as an error, never as silent success.
5. Connection testing follows the same Azure OpenAI wire contract and never leaks the key.
6. Public OpenAI still uses its own key, endpoint, and selected model; Azure Speech still uses its own
   key and region.
7. The original repro is inverted: Azure OpenAI is storable, probeable, runtime-readable, and no
   longer forces fallback when fully configured.
