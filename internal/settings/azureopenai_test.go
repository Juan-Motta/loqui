package settings

import "testing"

func TestNormalizeAzureOpenAIModelAcceptsBothRealtimeTranscriptionModels(t *testing.T) {
	for _, model := range []string{AzureOpenAIRealtimeWhisper, AzureOpenAILiveTranscribe} {
		got, err := NormalizeAzureOpenAIModel("  " + model + "  ")
		if err != nil {
			t.Fatalf("NormalizeAzureOpenAIModel(%q): %v", model, err)
		}
		if got != model {
			t.Errorf("NormalizeAzureOpenAIModel(%q) = %q", model, got)
		}
	}
}

func TestNormalizeAzureOpenAIModelRejectsUnknownValues(t *testing.T) {
	for _, model := range []string{"", "gpt-4o-transcribe", "gpt-live-transcribe-preview"} {
		if _, err := NormalizeAzureOpenAIModel(model); err == nil {
			t.Errorf("NormalizeAzureOpenAIModel(%q) succeeded", model)
		}
	}
}
