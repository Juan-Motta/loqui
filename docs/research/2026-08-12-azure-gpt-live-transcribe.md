# Azure GPT Live Transcribe research

Checked: 2026-08-12

## Questions

1. Can Loqui reuse its Azure OpenAI realtime transport for `gpt-live-transcribe`?
2. Which parts of the transcription session differ from `gpt-realtime-whisper`?
3. How should Azure's model identity and user-defined deployment name be represented?

## Findings

### Verified in Loqui

- Azure OpenAI already uses the GA transcription WebSocket endpoint, `api-key` authentication,
  24 kHz PCM, manual buffer commits, and `session.updated` readiness.
- Settings persist only `azureOpenAiDeployment`. That is sufficient to address Azure, but it is not
  sufficient to choose a model-specific request schema when a deployment has a custom name.
- The current session builder always emits singular `language`, because it was built specifically
  for `gpt-realtime-whisper`.
- Home and Settings label the Azure OpenAI product as Realtime Whisper, so selecting a different
  model would leave the visible active engine inaccurate.

### Verified in current OpenAI documentation

- `gpt-live-transcribe` is a low-latency streaming speech-to-text model intended for realtime
  transcription. It supports prompt/context, keyword hints, multiple language hints and tunable
  transcription delay. [Model reference](https://developers.openai.com/api/docs/models/gpt-live-transcribe)
  (checked 2026-08-12).
- It uses the same realtime transcription session envelope and audio buffer events as Loqui's
  existing provider: 24 kHz PCM input, `input_audio_buffer.append`, explicit commit when VAD is
  disabled, transcription deltas, and completed transcripts.
  [Realtime transcription guide](https://developers.openai.com/api/docs/guides/realtime-transcription)
  (checked 2026-08-12).
- The language contract differs: `gpt-live-transcribe` accepts `languages` as an array. The guide
  explicitly says not to send singular `language` to that model.
  [Realtime transcription guide](https://developers.openai.com/api/docs/guides/realtime-transcription)
  (checked 2026-08-12).

### Verified in current Microsoft documentation

- Azure's realtime API follows the OpenAI event schema but requires the **deployment name** in the
  transcription `model` field. The deployment name is not necessarily the same as the base model
  name. [Azure realtime API reference](https://learn.microsoft.com/en-us/azure/foundry/openai/realtime-audio-reference?view=foundry-classic)
  (checked 2026-08-12).
- Azure documents the same transcription WebSocket endpoint and manual audio-buffer flow already
  used by Loqui. [Azure realtime WebSocket guide](https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/realtime-audio-websockets)
  (checked 2026-08-12).
- Microsoft's published Azure realtime model list did not yet include `gpt-live-transcribe` when
  checked. [Azure realtime REST reference](https://learn.microsoft.com/en-us/rest/api/microsoft-foundry/azureopenai/realtime)
  (checked 2026-08-12).

### Owner-provided environment fact

- The owner confirmed that their Azure resource exposes `gpt-live-transcribe` and that its
  deployment is also named `gpt-live-transcribe`. No credential value was requested or inspected.

### Inferences, explicitly separated

- The existing Azure transport and lifecycle should be reused because the documented session,
  audio, readiness and transcript events match.
- Loqui must persist the base-model choice separately from the deployment. Inferring the model from
  a deployment string would fail as soon as an Azure deployment uses a custom name.
- Live Azure acceptance remains an external compatibility claim until a real probe or dictation
  succeeds, because Microsoft has not yet listed this model in its public Azure reference.

## Implications for the design

- Add an explicit Azure OpenAI model setting with two supported values:
  `gpt-realtime-whisper` and `gpt-live-transcribe`.
- Default an absent setting to `gpt-realtime-whisper`, preserving existing installations.
- Continue placing the Azure deployment name—not the base model name—in the wire payload.
- Emit singular `language` for Realtime Whisper and plural `languages` for Live Transcribe.
- Keep manual commits and the existing realtime lifecycle for both models.
- Show the selected model in Settings and in Home's Azure OpenAI choice.
- Leave prompt, keywords and delay for a separate feature.
