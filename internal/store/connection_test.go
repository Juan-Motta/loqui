package store

import (
	"strings"
	"testing"
)

// The six rows, in the original's order. The Ajustes view paints them top to bottom, so the order
// is part of the design rather than an implementation detail.
func TestConnectionRowsAreTheSixEnginesInOrder(t *testing.T) {
	rows := ConnectionRows(Settings{}, nil, HostCapabilities{})

	want := []string{"whisper", "macos", "azure", "openai", "grok", "elevenlabs"}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for i, id := range want {
		if rows[i].ID != id {
			t.Errorf("row %d = %q, want %q", i, rows[i].ID, id)
		}
	}
}

// An engine needing no key is AVAILABLE, not "unconfigured": there is nothing to configure.
func TestALocalEngineIsAvailableWithNoKey(t *testing.T) {
	rows := byID(ConnectionRows(Settings{Provider: "grok"}, nil, HostCapabilities{}))

	if got := rows["whisper"].State; got != ConnAvailable {
		t.Errorf("whisper = %q, want %q", got, ConnAvailable)
	}
	if got := rows["whisper"].Label; got != "Disponible" {
		t.Errorf("label = %q, want Disponible", got)
	}
}

// The SELECTED engine reads as active, and only when it is actually ready.
func TestTheSelectedEngineIsActive(t *testing.T) {
	rows := byID(ConnectionRows(Settings{Provider: "whisper"}, nil, HostCapabilities{}))
	if got := rows["whisper"].State; got != ConnActive {
		t.Errorf("whisper = %q, want %q", got, ConnActive)
	}
	if got := rows["whisper"].Label; got != "Activo" {
		t.Errorf("label = %q, want Activo", got)
	}
}

// Selected but INCOMPLETE says so plainly rather than claiming to be active. Reporting "Activo" for
// an engine that cannot dictate is the failure this whole state machine exists to avoid.
func TestASelectedButUnconfiguredEngineIsUnconfigured(t *testing.T) {
	rows := byID(ConnectionRows(Settings{Provider: "grok"}, nil, HostCapabilities{}))
	if got := rows["grok"].State; got != ConnUnconfigured {
		t.Errorf("grok with no key = %q, want %q", got, ConnUnconfigured)
	}
}

// A cloud engine with its own key, not selected, is CONNECTED — distinct from available, which is
// what a keyless engine gets.
func TestACloudEngineWithItsKeyIsConnected(t *testing.T) {
	keys := map[KeySlot]bool{SlotGrok: true}
	rows := byID(ConnectionRows(Settings{Provider: "whisper"}, keys, HostCapabilities{}))
	if got := rows["grok"].State; got != ConnConnected {
		t.Errorf("grok with a key = %q, want %q", got, ConnConnected)
	}
}

// A key stored for one provider must never make another look configured. Each cloud backend has its
// OWN slot precisely so several can be set up at once without clobbering each other.
func TestOneProvidersKeyDoesNotConfigureAnother(t *testing.T) {
	keys := map[KeySlot]bool{SlotGrok: true}
	rows := byID(ConnectionRows(Settings{Provider: "whisper"}, keys, HostCapabilities{}))
	for _, id := range []string{"openai", "elevenlabs"} {
		if rows[id].State == ConnConnected {
			t.Errorf("%s looks connected on Grok's key", id)
		}
	}
}

// AZURE IS TWO SERVICES with two keys and two different required fields, so the state depends on
// which sub-service is selected — not on the provider name alone.
func TestAzureStateDependsOnTheSelectedSubservice(t *testing.T) {
	// Speech needs a region and the azure-speech key.
	speechReady := Settings{Provider: "whisper", AzureService: "speech", Region: "eastus"}
	rows := byID(ConnectionRows(speechReady, map[KeySlot]bool{SlotAzureSpeech: true}, HostCapabilities{}))
	if got := rows["azure"].State; got != ConnConnected {
		t.Errorf("azure speech with key+region = %q, want %q", got, ConnConnected)
	}

	// The same key with no region is not enough.
	noRegion := Settings{Provider: "whisper", AzureService: "speech"}
	rows = byID(ConnectionRows(noRegion, map[KeySlot]bool{SlotAzureSpeech: true}, HostCapabilities{}))
	if got := rows["azure"].State; got != ConnUnconfigured {
		t.Errorf("azure speech without a region = %q, want %q", got, ConnUnconfigured)
	}

	// OpenAI realtime needs the RESOURCE, not a region, and its own key.
	openaiReady := Settings{Provider: "whisper", AzureService: "openai", AzureOpenAiResource: "mi-recurso", AzureOpenAiDeployment: "mi-whisper", AzureOpenAiModel: "gpt-realtime-whisper"}
	rows = byID(ConnectionRows(openaiReady, map[KeySlot]bool{SlotAzureOpenAI: true}, HostCapabilities{}))
	if got := rows["azure"].State; got != ConnConnected {
		t.Errorf("azure openai with key+resource = %q, want %q", got, ConnConnected)
	}

	// And the speech key does not satisfy the openai sub-service.
	rows = byID(ConnectionRows(openaiReady, map[KeySlot]bool{SlotAzureSpeech: true}, HostCapabilities{}))
	if got := rows["azure"].State; got != ConnUnconfigured {
		t.Errorf("azure openai on the speech key = %q, want %q", got, ConnUnconfigured)
	}
}

func TestAzureOpenAIWithAnUnknownModelIsUnconfigured(t *testing.T) {
	settings := Settings{
		Provider:              "azure",
		AzureService:          "openai",
		AzureOpenAiResource:   "resource",
		AzureOpenAiDeployment: "deployment",
		AzureOpenAiModel:      "not-a-supported-model",
	}
	keys := map[KeySlot]bool{SlotAzureOpenAI: true}
	if got := byID(ConnectionRows(settings, keys, HostCapabilities{}))["azure"].State; got != ConnUnconfigured {
		t.Errorf("Azure OpenAI with an unknown model reported %q", got)
	}
}

func TestAzureOpenAIRequiresBothResourceAndDeployment(t *testing.T) {
	key := map[KeySlot]bool{SlotAzureOpenAI: true}
	for _, settings := range []Settings{
		{AzureService: "openai", AzureOpenAiResource: "resource"},
		{AzureService: "openai", AzureOpenAiDeployment: "deployment"},
	} {
		if got := byID(ConnectionRows(settings, key, HostCapabilities{}))["azure"].State; got != ConnUnconfigured {
			t.Errorf("incomplete Azure OpenAI settings reported %q", got)
		}
	}
}

func TestRuntimeKeySlotFollowsTheAzureSubservice(t *testing.T) {
	for service, want := range map[string]KeySlot{"speech": SlotAzureSpeech, "openai": SlotAzureOpenAI} {
		got, ok := RuntimeKeySlotFor("azure", service)
		if !ok || got != want {
			t.Errorf("RuntimeKeySlotFor(azure,%s) = %q,%v; want %q", service, got, ok, want)
		}
	}
}

// The kind line describes what the engine IS, and for Azure it follows the sub-service.
func TestTheKindLineFollowsTheAzureSubservice(t *testing.T) {
	rows := byID(ConnectionRows(Settings{AzureService: "speech"}, nil, HostCapabilities{}))
	if got := rows["azure"].Kind; got != "Nube · Speech multilingüe" {
		t.Errorf("kind = %q", got)
	}
	rows = byID(ConnectionRows(Settings{AzureService: "openai"}, nil, HostCapabilities{}))
	if got := rows["azure"].Kind; got != "Nube · Azure OpenAI realtime" {
		t.Errorf("kind = %q", got)
	}
	if got := rows["whisper"].Kind; got != "Local · sin conexión ni clave" {
		t.Errorf("whisper kind = %q", got)
	}
}

func TestAzureHintFollowsTheSelectedSubservice(t *testing.T) {
	speech := ProviderHint("azure", "speech")
	if !strings.Contains(speech, "región + clave") {
		t.Errorf("Azure Speech hint = %q", speech)
	}
	openai := ProviderHint("azure", "openai")
	for _, want := range []string{"recurso", "deployment", "clave"} {
		if !strings.Contains(openai, want) {
			t.Errorf("Azure OpenAI hint = %q; missing %q", openai, want)
		}
	}
	if strings.Contains(openai, "región") {
		t.Errorf("Azure OpenAI hint still asks for a Speech region: %q", openai)
	}
}

// UNSUPPORTED beats every amount of configuration: no key makes a missing native helper work, and
// the user must not be offered an engine that cannot run here.
func TestAMissingHelperMakesAnEngineUnsupported(t *testing.T) {
	caps := HostCapabilities{Helpers: map[string]bool{"whisper-stt": false}}
	rows := byID(ConnectionRows(Settings{Provider: "whisper"}, nil, caps))
	if got := rows["whisper"].State; got != ConnUnsupported {
		t.Errorf("whisper with no helper = %q, want %q", got, ConnUnsupported)
	}
	if got := rows["whisper"].Label; got != "No disponible en este sistema" {
		t.Errorf("label = %q", got)
	}
}

// The Apple engine needs macOS 26 for SpeechAnalyzer: the same build installs on older systems,
// where the framework simply is not there. That is a RUNTIME question about this machine.
func TestTheAppleEngineNeedsMacOS26(t *testing.T) {
	old := HostCapabilities{OSMajor: 15}
	if got := byID(ConnectionRows(Settings{}, nil, old))["macos"].State; got != ConnUnsupported {
		t.Errorf("macos on macOS 15 = %q, want %q", got, ConnUnsupported)
	}
	new26 := HostCapabilities{OSMajor: 26}
	if got := byID(ConnectionRows(Settings{}, nil, new26))["macos"].State; got == ConnUnsupported {
		t.Error("macos on macOS 26 reported as unsupported")
	}
}

// An UNKNOWN condition must never disqualify an engine. A caller that cannot look — a pure test, a
// page rendering before the host has answered — has to behave exactly as it did before.
func TestAnUnknownCapabilityDoesNotDisqualify(t *testing.T) {
	rows := byID(ConnectionRows(Settings{}, nil, HostCapabilities{}))
	for _, id := range []string{"whisper", "macos"} {
		if rows[id].State == ConnUnsupported {
			t.Errorf("%s was disqualified by an unknown capability", id)
		}
	}
}

func TestKeySlotForProvider(t *testing.T) {
	cases := []struct {
		provider, azureService string
		want                   KeySlot
		none                   bool
	}{
		{provider: "azure", azureService: "speech", want: SlotAzureSpeech},
		{provider: "azure", azureService: "openai", want: SlotAzureOpenAI},
		{provider: "azure", azureService: "", want: SlotAzureSpeech}, // speech is the default
		{provider: "openai", want: SlotOpenAI},
		{provider: "grok", want: SlotGrok},
		{provider: "elevenlabs", want: SlotElevenLabs},
		{provider: "whisper", none: true},
		{provider: "macos", none: true},
		// Unrecognised returns none: better to report "no key" than to read someone else's secret.
		{provider: "dragon", none: true},
	}
	for _, c := range cases {
		got, ok := KeySlotFor(c.provider, c.azureService)
		if c.none {
			if ok {
				t.Errorf("KeySlotFor(%q) returned %q, want none", c.provider, got)
			}
			continue
		}
		if !ok || got != c.want {
			t.Errorf("KeySlotFor(%q,%q) = %q,%v; want %q", c.provider, c.azureService, got, ok, c.want)
		}
	}
}

func byID(rows []ConnectionRow) map[string]ConnectionRow {
	out := make(map[string]ConnectionRow, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

// The row carries the KEY slot for the same reason it carries LangSlot: Azure's slot depends on its
// sub-service, so a page deriving it would store the credential under the wrong service — and the
// wizard, which offers a key field for the engine you just chose, is a second place that would have
// to reimplement the rule.
func TestConnectionRowsCarryTheKeySlotAndLeaveItEmptyForLocalEngines(t *testing.T) {
	s := DefaultSettings()
	s.AzureService = "speech"
	rows := ConnectionRows(s, map[KeySlot]bool{}, HostCapabilities{})

	bySlot := map[string]string{}
	for _, r := range rows {
		bySlot[r.ID] = r.KeySlot
	}

	// A local engine needs no credential at all: an empty slot is what tells the page not to ask.
	for _, local := range []string{"whisper", "macos"} {
		if got := bySlot[local]; got != "" {
			t.Errorf("%s trae keySlot %q, quería vacío — no usa clave", local, got)
		}
	}
	for id, want := range map[string]string{
		"azure":      "azure-speech",
		"openai":     "openai",
		"grok":       "grok",
		"elevenlabs": "elevenlabs",
	} {
		if got := bySlot[id]; got != want {
			t.Errorf("%s trae keySlot %q, quería %q", id, got, want)
		}
	}

	// The sub-service is the whole point: with Azure on OpenAI realtime the row must edit the OTHER
	// slot, or the key lands where dictation will never read it.
	s.AzureService = "openai"
	for _, r := range ConnectionRows(s, map[KeySlot]bool{}, HostCapabilities{}) {
		if r.ID == "azure" && r.KeySlot != "azure-openai" {
			t.Errorf("azure con subservicio openai trae keySlot %q, quería azure-openai", r.KeySlot)
		}
	}
}
