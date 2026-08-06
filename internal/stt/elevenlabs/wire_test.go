package elevenlabs

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func parseQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("URL inválida %q: %v", raw, err)
	}
	return u.Query()
}

func TestBuildURLCarriesTheModelAndFormat(t *testing.T) {
	q := parseQuery(t, BuildURL(""))
	if got := q.Get("model_id"); got != "scribe_v2_realtime" {
		t.Errorf("model_id = %q", got)
	}
	if got := q.Get("audio_format"); got != "pcm_16000" {
		t.Errorf("audio_format = %q, quería el que coincide con la captura de 16 kHz", got)
	}
	// Without this the server only transcribes when the stream closes: no partials, and for
	// hold-to-talk the whole text lands at the end.
	if got := q.Get("commit_strategy"); got != "vad" {
		t.Errorf("commit_strategy = %q, quería vad", got)
	}
}

// An empty language means "detect", and the parameter must be ABSENT rather than empty or "auto":
// "auto" is Grok's sentinel and would be read here as a language name.
func TestBuildURLOmitsTheLanguageWhenEmpty(t *testing.T) {
	q := parseQuery(t, BuildURL(""))
	if _, present := q["language_code"]; present {
		t.Errorf("language_code presente con idioma vacío: %q", q.Get("language_code"))
	}
}

func TestBuildURLSetsTheLanguageWhenGiven(t *testing.T) {
	q := parseQuery(t, BuildURL("es"))
	if got := q.Get("language_code"); got != "es" {
		t.Errorf("language_code = %q, quería es", got)
	}
}

// The credential must never reach the URL — URLs end up in logs, error messages and crash reports.
// Checked on the QUERY, not the whole string: the host itself is api.elevenlabs.io, and matching on
// "api" anywhere made this fail for the wrong reason.
func TestBuildURLNeverCarriesACredential(t *testing.T) {
	q := parseQuery(t, BuildURL("es"))
	for name := range q {
		lower := strings.ToLower(name)
		for _, forbidden := range []string{"key", "token", "secret", "auth"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("la URL lleva el parámetro %q — la clave va en la cabecera %s", name, APIKeyHeader)
			}
		}
	}
}

// The audio has to be base64 INSIDE JSON. A binary frame is ignored by this endpoint, which looks
// exactly like a connected session that transcribes nothing.
func TestBuildAudioMessageWrapsPCMAsBase64JSON(t *testing.T) {
	pcm := []byte{0x00, 0x01, 0xff, 0xfe, 0x10}
	raw, err := BuildAudioMessage(pcm, SampleRate, false)
	if err != nil {
		t.Fatalf("BuildAudioMessage: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("el mensaje no es JSON: %v (%s)", err, raw)
	}
	if got["message_type"] != "input_audio_chunk" {
		t.Errorf("message_type = %v", got["message_type"])
	}
	if got["sample_rate"] != float64(SampleRate) {
		t.Errorf("sample_rate = %v, quería %d", got["sample_rate"], SampleRate)
	}
	if got["commit"] != false {
		t.Errorf("commit = %v, quería false", got["commit"])
	}
	// Decoded back it must be the SAME bytes: a test that only checked "non-empty" would bless a
	// truncated or re-encoded chunk, and the audio would arrive as noise.
	b64, ok := got["audio_base_64"].(string)
	if !ok {
		t.Fatalf("audio_base_64 no es una cadena: %v", got["audio_base_64"])
	}
	back, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("audio_base_64 no decodifica: %v", err)
	}
	if string(back) != string(pcm) {
		t.Errorf("el PCM no sobrevive el viaje: %v -> %v", pcm, back)
	}
}

func TestBuildAudioMessageCarriesTheCommitFlag(t *testing.T) {
	raw, err := BuildAudioMessage([]byte{1, 2}, SampleRate, true)
	if err != nil {
		t.Fatalf("BuildAudioMessage: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["commit"] != true {
		t.Errorf("commit = %v con commit=true", got["commit"])
	}
}

func TestDecodeRecognisesEachEvent(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want OutcomeKind
		text string
	}{
		{"handshake", `{"message_type":"session_started"}`, Ready, ""},
		{"partial", `{"message_type":"partial_transcript","text":"hola mu"}`, Partial, "hola mu"},
		{"final", `{"message_type":"final_transcript","text":"hola mundo"}`, Final, "hola mundo"},
		{"committed", `{"message_type":"committed_transcript","text":"hola mundo"}`, Final, "hola mundo"},
		// The reason the check is a prefix and not an equality: this shape is on the wire too, and an
		// equality check would drop the final, ending the dictation with only its partials.
		{"committed con sufijo", `{"message_type":"committed_transcript_final","text":"ya"}`, Final, "ya"},
		{"error", `{"message_type":"error","error":"clave inválida"}`, Error, ""},
		{"desconocido", `{"message_type":"algo_nuevo"}`, Ignore, ""},
		// Malformed input must not be reported as a session error: aborting a working dictation over
		// one unparseable frame is worse than ignoring it.
		{"json roto", `{no es json`, Ignore, ""},
		{"vacío", ``, Ignore, ""},
	}
	for _, c := range cases {
		got := Decode([]byte(c.raw))
		if got.Kind != c.want {
			t.Errorf("%s: kind = %s, quería %s", c.name, got.Kind, c.want)
		}
		if c.text != "" && got.Text != c.text {
			t.Errorf("%s: text = %q, quería %q", c.name, got.Text, c.text)
		}
	}
}

// `type` as well as `message_type`: the documented field is the latter, but events on this endpoint
// carry the former. Accepting only one silently ignores half of them.
func TestDecodeAcceptsEitherFieldNameForTheEventType(t *testing.T) {
	if got := Decode([]byte(`{"type":"partial_transcript","text":"eh"}`)); got.Kind != Partial {
		t.Errorf("con `type` en vez de `message_type`: kind = %s", got.Kind)
	}
	// And message_type WINS when both are present, which is the documented field.
	got := Decode([]byte(`{"message_type":"session_started","type":"partial_transcript"}`))
	if got.Kind != Ready {
		t.Errorf("con ambos campos: kind = %s, quería ready (message_type manda)", got.Kind)
	}
}

// An error with no message still has to be reportable — "" would surface as a blank failure the user
// cannot act on or paste into a report.
func TestDecodeGivesAnErrorAFallbackMessage(t *testing.T) {
	got := Decode([]byte(`{"message_type":"stt_error"}`))
	if got.Kind != Error {
		t.Fatalf("kind = %s, quería error", got.Kind)
	}
	if got.Error == "" {
		t.Error("un error sin mensaje llegó vacío")
	}
}

// THE DOCUMENTED ERROR EVENTS THAT WERE FALLING THROUGH TO Ignore.
//
// Decode only treated a name as an error when it contained "error" (the old `strings.Contains`), so
// every event below was ignored: the session then reported whatever happened next — usually the socket
// closing — as a generic connection loss, and the specific cause was gone.
//
// Code carries the event NAME, because that is the machine-readable signal. Without it a caller cannot
// tell "you are out of credit" from "you have not accepted the terms", which are different things for
// the user to do.
func TestTheDocumentedErrorEventsAreRecognisedAndNamed(t *testing.T) {
	for _, name := range []string{
		"auth_error", "quota_exceeded", "unaccepted_terms", "rate_limited",
		"queue_overflow", "resource_exhausted", "commit_throttled",
		"transcriber_error", "session_time_limit_exceeded", "chunk_size_exceeded",
	} {
		t.Run(name, func(t *testing.T) {
			out := Decode([]byte(`{"message_type":"` + name + `","error":"lo que sea"}`))
			if out.Kind != Error {
				t.Errorf("Kind = %v, want Error — this event was being ignored", out.Kind)
			}
			if out.Code != name {
				t.Errorf("Code = %q, want %q — the machine-readable signal was dropped", out.Code, name)
			}
		})
	}
}

// A readiness event must NOT be mistaken for an error just because the list above grew.
func TestReadinessAndTranscriptsAreNotSweptUpAsErrors(t *testing.T) {
	for _, c := range []struct {
		raw  string
		kind OutcomeKind
	}{
		{`{"message_type":"session_started"}`, Ready},
		{`{"message_type":"partial_transcript","text":"hola"}`, Partial},
		{`{"message_type":"final_transcript","text":"hola"}`, Final},
	} {
		if out := Decode([]byte(c.raw)); out.Kind != c.kind {
			t.Errorf("%s → %v, want %v", c.raw, out.Kind, c.kind)
		}
	}
}
