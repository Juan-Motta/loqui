package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Azure card is a single DOM form for two independent products. This source-level contract keeps
// the generated binding names and the dynamic slot decision attached to the controls a user clicks;
// backend tests alone cannot detect a dropdown whose handlers still captured azure-speech at startup.
func TestAzureOpenAIControlsUseTheirDedicatedBindingsAndDynamicSlot(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "settings.ts"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		"function keySlotForProvider(",
		"Settings.SaveAzureConnection(",
		"Settings.TestAzureOpenAIConnection(",
		"Settings.SaveOpenAIConnection(",
		"azureOpenAiResource",
		"azureOpenAiDeployment",
		"azureOpenAiModel",
		`case "set-service":`,
		`case "set-resource":`,
		`case "set-deployment":`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("settings UI is missing %q", required)
		}
	}
	if strings.Contains(source, "const slot = KEY_SLOT_BY_PROVIDER[provider]") {
		t.Error("connection handlers still capture Azure Speech's slot before the user changes the subservice")
	}
	deploymentCaseStart := strings.Index(source, `case "set-deployment":`)
	if deploymentCaseStart < 0 {
		t.Fatal("Azure debug driver is missing its deployment action")
	}
	modelCaseOffset := strings.Index(source[deploymentCaseStart:], `case "set-model":`)
	if modelCaseOffset < 0 || !strings.Contains(
		source[deploymentCaseStart:deploymentCaseStart+modelCaseOffset],
		`live: "gpt-live-transcribe"`,
	) {
		t.Error("Azure debug driver cannot select the real gpt-live-transcribe deployment")
	}
}

func TestHomeNamesBothAzureProductsInsteadOfCallingThemBothAzureSpeech(t *testing.T) {
	index, err := os.ReadFile(filepath.Join("..", "..", "frontend", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	markup := string(index)
	for _, required := range []string{
		`value="azure-speech"`,
		`value="azure-openai"`,
		`id="azureOpenAiModel"`,
		`value="gpt-realtime-whisper"`,
		`value="gpt-live-transcribe"`,
	} {
		if !strings.Contains(markup, required) {
			t.Errorf("Home engine picker is missing %q", required)
		}
	}

	source, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "settings.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"prov.state",
		"prov.selected",
		"p.azureOpenAiModel",
		"azureOpenAiHomeLabel",
		"Settings.SaveAzureConnection(service, regionValue, resource, deployment, model, secret)",
		"Settings.TestAzureOpenAIConnection(resource, deployment, model, secret)",
	} {
		if !strings.Contains(string(source), required) {
			t.Errorf("Home picker does not consume backend-owned selection metadata %q", required)
		}
	}
}
