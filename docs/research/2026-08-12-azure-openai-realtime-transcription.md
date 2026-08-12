# Azure OpenAI realtime transcription research

Checked: 2026-08-12

## Questions

1. Is Azure OpenAI realtime transcription currently supported, and which model is appropriate for
   Loqui's live dictation flow?
2. What endpoint, authentication, session payload, audio format, and event flow does the current GA
   API require?
3. Which parts of Loqui's existing OpenAI realtime provider can be reused safely, and which parts
   must remain Azure-specific?

## Findings

### Verified in Loqui

- The Azure service picker contains `Azure OpenAI — realtime (gpt-realtime-whisper)`, but the UI
  disables it whenever the `azure-openai` key slot is unavailable
  (`frontend/src/settings.ts:640-670`).
- The slot is deliberately absent from `availableKeySlots`; comments state that the realtime
  subservice is not ported (`internal/store/secrets.go:83-104`).
- When `provider == "azure"`, dictation always validates a Speech region, reads the
  `azure-speech` credential, and constructs the Azure Speech SDK recognizer. It never consults
  `AzureService` (`internal/app/dictation.go:271-302`).
- `RuntimeKeySlotFor("azure")` is consequently hard-coded to `azure-speech`, even though the
  settings model maps the selected OpenAI subservice to `azure-openai`
  (`internal/store/connection.go:132-156`).
- The current focused repro is deterministic: the tests explicitly confirm that an Azure OpenAI
  key is refused, no prober exists, the runtime slot is unreadable, and a stored Azure OpenAI
  configuration falls back to Whisper.

### Verified in current Microsoft documentation

- `gpt-realtime-whisper` is a streaming transcription model and is available as a Global Standard
  deployment. Microsoft says to use the deployment name created for that model, not assume the base
  model name is the deployment name. [GPT Realtime Whisper overview](https://learn.microsoft.com/en-us/azure/foundry/openai/concepts/gpt-realtime-whisper) (checked 2026-08-12).
- The GA WebSocket URL for a dedicated transcription session is
  `wss://<resource>.openai.azure.com/openai/v1/realtime?intent=transcription`. An API key can be sent
  in the pre-handshake `api-key` header. [Realtime WebSocket guide](https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/realtime-audio-websockets) (checked 2026-08-12).
- The GA transcription sample sends 24 kHz mono PCM16, puts the Azure **deployment name** in
  `session.audio.input.transcription.model`, waits for `session.updated`, appends Base64 audio, and
  commits buffered audio periodically. The completed event is
  `conversation.item.input_audio_transcription.completed`. [Realtime WebSocket guide](https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/realtime-audio-websockets) (checked 2026-08-12).
- For `gpt-realtime-whisper` transcription sessions, `turn_detection` must be `null`; VAD is not
  supported. Therefore a client must send `input_audio_buffer.commit` itself.
  [Azure OpenAI realtime REST reference](https://learn.microsoft.com/en-us/rest/api/microsoft-foundry/azureopenai/realtime) (API version `v1`, checked 2026-08-12).
- Azure follows the OpenAI Realtime event schema, but Azure requires the existing deployment name in
  the transcription model field. [Realtime API reference](https://learn.microsoft.com/en-us/azure/foundry/openai/realtime-audio-reference) (checked 2026-08-12).

### Inferences, explicitly separated

- Loqui's OpenAI provider already has the right 24 kHz resampling, nested GA session schema, audio
  append messages, transcript accumulation, and completed-event decoder. Reusing that lifecycle is
  lower risk than creating a second near-copy.
- Its authentication and turn handling cannot be reused unchanged: public OpenAI uses WebSocket
  subprotocol authentication and server VAD, while Azure requires an `api-key` header and
  `gpt-realtime-whisper` requires manual commits with VAD disabled.
- The existing free-text Azure `Deployment` field is the correct UX primitive. Replacing it with a
  fixed model dropdown would be wrong because Azure accepts the user-defined deployment name.

## Prior art

The official Microsoft Python example is the closest prior art for Loqui's push-to-talk desktop
flow. It keeps a single WebSocket open, streams 24 kHz PCM16 chunks, sends an explicit
`session.update`, waits for `session.updated`, and commits every three seconds. Loqui should borrow
the protocol boundaries, while retaining its existing bounded non-blocking audio queue and
generation-safe lifecycle.

## Implications for the design

- Treat Azure Speech and Azure OpenAI as two runtime modes behind the existing `azure` provider card.
- Generalize the existing OpenAI realtime provider only at explicit seams: endpoint, handshake
  headers/subprotocols, service name in messages, and commit policy.
- Build the Azure URL from a strictly validated resource name and keep the API key in the header,
  never in the URL or logs.
- Require resource, deployment, and Azure OpenAI key before the engine can be active.
- Wait for `session.updated` before declaring the session started.
- Disable server VAD for Azure and commit periodically plus once at stop when uncommitted audio
  remains; never commit an empty buffer.
- Add a real Azure OpenAI connection probe that uses the same endpoint and authentication as
  dictation and cannot report success before a named readiness event.
- Remove documentation that says Azure OpenAI is unported only after the runtime, storage, UI, and
  probe paths are all covered by tests.

## Open questions

- A real end-to-end verification requires an Azure OpenAI resource with a deployed
  `gpt-realtime-whisper`, its deployment name, and a valid key. Unit/integration tests can prove the
  wire contract without consulting or exposing a real credential, but the final live E2E depends on
  the user's existing configuration being complete.
- Microsoft documents configurable transcription delay values. This fix should initially use the
  service default rather than add another setting; latency tuning is a separate product decision.
