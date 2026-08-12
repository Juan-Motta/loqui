package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

// The key rides in the second subprotocol. A test that only counted them would pass with the key
// missing, so this asserts the exact value.
func TestBuildSubprotocolsCarriesTheKeyInTheSecondEntry(t *testing.T) {
	got := BuildSubprotocols("sk-test-123")
	want := []string{"realtime", "openai-insecure-api-key.sk-test-123"}
	if len(got) != len(want) {
		t.Fatalf("subprotocolos = %v, quería %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("subprotocolo %d = %q, quería %q", i, got[i], want[i])
		}
	}
}

// A stray space in a subprotocol gets the handshake rejected with a bare 400 that says nothing about
// the cause, so no entry may contain whitespace.
func TestBuildSubprotocolsHaveNoWhitespace(t *testing.T) {
	for _, p := range BuildSubprotocols("sk-test-123") {
		if strings.ContainsAny(p, " \t\r\n") {
			t.Errorf("el subprotocolo %q contiene espacios", p)
		}
	}
}

// The credential goes in the handshake, never in the URL — a URL reaches logs and error messages.
func TestTheRealtimeURLCarriesNoCredential(t *testing.T) {
	lower := strings.ToLower(RealtimeURL)
	for _, forbidden := range []string{"key=", "token=", "secret=", "authorization"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("la URL %q lleva %q", RealtimeURL, forbidden)
		}
	}
}

func sessionOf(t *testing.T, model, language string) map[string]any {
	t.Helper()
	raw, err := BuildSessionUpdate(model, language)
	if err != nil {
		t.Fatalf("BuildSessionUpdate: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("el payload no es JSON: %v (%s)", err, raw)
	}
	return got
}

// The NESTED v1 schema shared by public OpenAI and Azure OpenAI GA realtime transcription.
func TestSessionUpdateUsesTheNestedSchema(t *testing.T) {
	got := sessionOf(t, "", "")
	if got["type"] != "session.update" {
		t.Fatalf("type = %v", got["type"])
	}
	session, ok := got["session"].(map[string]any)
	if !ok {
		t.Fatalf("session no es un objeto: %v", got["session"])
	}
	if session["type"] != "transcription" {
		t.Errorf("session.type = %v", session["type"])
	}
	audio, ok := session["audio"].(map[string]any)
	if !ok {
		t.Fatalf("session.audio ausente: %v", session)
	}
	input, ok := audio["input"].(map[string]any)
	if !ok {
		t.Fatalf("session.audio.input ausente: %v", audio)
	}
	format, ok := input["format"].(map[string]any)
	if !ok {
		t.Fatalf("format ausente: %v", input)
	}
	// 24 kHz, not the 16 kHz the app captures. Declaring a rate the audio does not match does not
	// fail — it transcribes a sped-up voice badly, which is worse than an error.
	if format["rate"] != float64(SampleRate) || SampleRate != 24000 {
		t.Errorf("format.rate = %v, quería %d", format["rate"], SampleRate)
	}
	if _, ok := input["turn_detection"].(map[string]any); !ok {
		t.Errorf("falta turn_detection: sin VAD del servidor no se cierran las frases")
	}
}

func TestSessionUpdateDefaultsTheModelAndTrimsIt(t *testing.T) {
	for _, given := range []string{"", "   "} {
		input := sessionOf(t, given, "")["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)
		tr := input["transcription"].(map[string]any)
		if tr["model"] != DefaultModel {
			t.Errorf("con modelo %q -> %v, quería %s", given, tr["model"], DefaultModel)
		}
	}
	input := sessionOf(t, "  gpt-4o-transcribe  ", "")["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)
	if got := input["transcription"].(map[string]any)["model"]; got != "gpt-4o-transcribe" {
		t.Errorf("modelo = %v, quería recortado", got)
	}
}

// An absent language must be ABSENT, not an empty string: an empty hint is a value the API reads, and
// it is not the same request as omitting it.
func TestSessionUpdateOmitsTheLanguageWhenEmpty(t *testing.T) {
	input := sessionOf(t, "", "")["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)
	tr := input["transcription"].(map[string]any)
	if _, present := tr["language"]; present {
		t.Errorf("language presente con idioma vacío: %v", tr["language"])
	}
	input = sessionOf(t, "", "es")["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)
	tr = input["transcription"].(map[string]any)
	if tr["language"] != "es" {
		t.Errorf("language = %v, quería es", tr["language"])
	}
}

func TestDecodeRecognisesEachEvent(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		want  OutcomeKind
		delta string
		text  string
	}{
		{
			"delta",
			`{"type":"conversation.item.input_audio_transcription.delta","delta":"ho"}`,
			PartialDelta, "ho", "",
		},
		{
			"completed",
			`{"type":"conversation.item.input_audio_transcription.completed","transcript":"hola mundo"}`,
			Final, "", "hola mundo",
		},
		{
			"completed con el campo text que usa Azure",
			`{"type":"conversation.item.input_audio_transcription.completed","text":"hola desde azure"}`,
			Final, "", "hola desde azure",
		},
		{"error", `{"type":"error","error":{"message":"clave inválida"}}`, Error, "", ""},
		{"transcription.failed", `{"type":"conversation.item.input_audio_transcription.failed","error":{"code":"audio_invalid","message":"audio inválido"}}`, Error, "", ""},
		// session.created was listed here as "desconocido" — the old reading, and it was wrong. It is
		// the service's session confirmation, and for a probe it is the ONLY positive proof the
		// credential was accepted, because an invalid key still gets an HTTP 101 from this service
		// (measured — docs/research/2026-08-06-where-realtime-stt-auth-fails.md).
		{"session.created es la confirmación de sesión", `{"type":"session.created"}`, Ready, "", ""},
		{"session.updated confirma la configuración", `{"type":"session.updated"}`, Configured, "", ""},
		{"desconocido de verdad", `{"type":"algo.que.no.existe"}`, Ignore, "", ""},
		{"json roto", `{no`, Ignore, "", ""},
	}
	for _, c := range cases {
		got := Decode([]byte(c.raw))
		if got.Kind != c.want {
			t.Errorf("%s: kind = %s, quería %s", c.name, got.Kind, c.want)
		}
		if c.delta != "" && got.Delta != c.delta {
			t.Errorf("%s: delta = %q, quería %q", c.name, got.Delta, c.delta)
		}
		if c.text != "" && got.Text != c.text {
			t.Errorf("%s: text = %q, quería %q", c.name, got.Text, c.text)
		}
	}
}

func TestDecodePreservesTheTranscriptionFailureCode(t *testing.T) {
	got := Decode([]byte(`{"type":"conversation.item.input_audio_transcription.failed","error":{"code":"audio_invalid","message":"audio inválido"}}`))
	if got.Kind != Error || got.Code != "audio_invalid" || got.Error != "audio inválido" {
		t.Errorf("outcome = %+v", got)
	}
}

// SUFFIX matching, ported as such: the container prefix has changed across revisions of this API while
// the tail has not. An exact match would make the provider go silent — connected, no error, no text —
// the next time OpenAI renames it.
func TestDecodeMatchesTheEventTailNotTheWholeName(t *testing.T) {
	got := Decode([]byte(`{"type":"some.future.container.transcription.delta","delta":"x"}`))
	if got.Kind != PartialDelta {
		t.Errorf("un contenedor renombrado dejó de reconocerse: kind = %s", got.Kind)
	}
	got = Decode([]byte(`{"type":"other.prefix.transcription.completed","transcript":"y"}`))
	if got.Kind != Final {
		t.Errorf("final con otro prefijo: kind = %s", got.Kind)
	}
}

// The message lives at error.message here, not at the top level, and an error with neither still has
// to say something the user can paste into a report.
func TestDecodeFindsTheErrorMessageOrFallsBack(t *testing.T) {
	if got := Decode([]byte(`{"type":"error","error":{"message":"nested"}}`)); got.Error != "nested" {
		t.Errorf("error anidado = %q", got.Error)
	}
	if got := Decode([]byte(`{"type":"error","message":"plano"}`)); got.Error != "plano" {
		t.Errorf("error plano = %q", got.Error)
	}
	got := Decode([]byte(`{"type":"error"}`))
	if got.Kind != Error || got.Error == "" {
		t.Errorf("error sin mensaje: kind=%s error=%q", got.Kind, got.Error)
	}
}

// Deltas are FRAGMENTS. This is the rule a provider built on this package has to honour, so it is
// pinned here: three deltas make one growing phrase, not three separate partials.
func TestDeltasAreFragmentsThatConcatenate(t *testing.T) {
	var built strings.Builder
	for _, raw := range []string{
		`{"type":"conversation.item.input_audio_transcription.delta","delta":"ho"}`,
		`{"type":"conversation.item.input_audio_transcription.delta","delta":"la "}`,
		`{"type":"conversation.item.input_audio_transcription.delta","delta":"mundo"}`,
	} {
		out := Decode([]byte(raw))
		if out.Kind != PartialDelta {
			t.Fatalf("kind = %s", out.Kind)
		}
		built.WriteString(out.Delta)
	}
	if built.String() != "hola mundo" {
		t.Errorf("los deltas concatenados dan %q", built.String())
	}
}

// THE MACHINE-READABLE CODE MUST SURVIVE DECODING.
//
// wireError kept only `message`, throwing away `type` and `code`. The real service returns
// `code: "invalid_api_key"` (measured — docs/research/2026-08-06-where-realtime-stt-auth-fails.md), and
// discarding it leaves only prose to classify from, which is the failure internal/session/policy.go:36
// documents. It also means the probe has nothing short and non-prose to show the user.
func TestAnErrorEventKeepsItsCodeAndType(t *testing.T) {
	raw := `{"type":"error","error":{"type":"invalid_request_error","code":"invalid_api_key",` +
		`"message":"Incorrect API key provided: sk-proj-****ueba"}}`

	out := Decode([]byte(raw))

	if out.Kind != Error {
		t.Fatalf("Kind = %v, want Error", out.Kind)
	}
	if out.Code != "invalid_api_key" {
		t.Errorf("Code = %q, want invalid_api_key — the machine-readable signal was dropped", out.Code)
	}
	if out.Error == "" {
		t.Error("the prose was dropped too; it is still wanted for the log")
	}
}

// An error with a TYPE but no CODE falls back to the type, and this case exists because a mutation
// showed the fallback was unreachable from the suite: the "no code" test below also had no type, so
// deleting the fallback changed nothing. OpenAI documents families like "server_error" that arrive
// without a specific code, and the family is still worth showing.
func TestAnErrorEventFallsBackToItsTypeWhenThereIsNoCode(t *testing.T) {
	out := Decode([]byte(`{"type":"error","error":{"type":"server_error","message":"algo pasó"}}`))
	if out.Kind != Error {
		t.Fatalf("Kind = %v, want Error", out.Kind)
	}
	if out.Code != "server_error" {
		t.Errorf("Code = %q, want server_error — the family is the only signal there was", out.Code)
	}
}

// An error with no code still decodes, and says so rather than inventing one.
func TestAnErrorEventWithoutACodeIsStillAnError(t *testing.T) {
	out := Decode([]byte(`{"type":"error","error":{"message":"algo pasó"}}`))
	if out.Kind != Error {
		t.Fatalf("Kind = %v, want Error", out.Kind)
	}
	if out.Code != "" {
		t.Errorf("Code = %q, want empty — nothing was reported", out.Code)
	}
}
