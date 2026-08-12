package settings

import (
	"fmt"
	"strings"
)

const (
	AzureOpenAIRealtimeWhisper = "gpt-realtime-whisper"
	AzureOpenAILiveTranscribe  = "gpt-live-transcribe"
)

// azureOpenAIModels is the explicit base-model choice. Azure's deployment name remains a separate
// user-defined value and is the value sent in session.audio.input.transcription.model.
var azureOpenAIModels = [...]string{AzureOpenAIRealtimeWhisper, AzureOpenAILiveTranscribe}

// NormalizeAzureOpenAIModel validates the model contract before any credential or settings write.
func NormalizeAzureOpenAIModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	for _, candidate := range azureOpenAIModels {
		if model == candidate {
			return model, nil
		}
	}
	return "", fmt.Errorf("modelo de Azure OpenAI desconocido: %q", model)
}

func IsKnownAzureOpenAIModel(model string) bool {
	_, err := NormalizeAzureOpenAIModel(model)
	return err == nil
}
