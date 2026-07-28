package grok

import (
	"net/url"
	"testing"
)

func parseQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("BuildURL produced an unparseable URL %q: %v", raw, err)
	}
	if got, want := u.Scheme+"://"+u.Host+u.Path, WSEndpoint; got != want {
		t.Errorf("endpoint = %q, want %q", got, want)
	}
	return u.Query()
}

// The three fixed parameters. sample_rate has to match what the host captures
// (internal/audio) — the service resamples otherwise, and 16 kHz is the model's native rate.
func TestBuildURLCarriesTheFixedParameters(t *testing.T) {
	q := parseQuery(t, BuildURL("auto"))

	for _, tc := range []struct{ key, want string }{
		{"encoding", "pcm"},
		{"sample_rate", "16000"},
		{"interim_results", "true"},
	} {
		if got := q.Get(tc.key); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.key, got, tc.want)
		}
	}
}

// "auto" is OUR sentinel, not the API's: xAI auto-detects by receiving no language at all.
// Sending the literal string "auto" would be taken as a language code and rejected.
func TestBuildURLOmitsLanguageWhenAuto(t *testing.T) {
	for _, lang := range []string{"auto", "", "  "} {
		q := parseQuery(t, BuildURL(lang))
		if _, present := q["language"]; present {
			t.Errorf("BuildURL(%q) sent language=%q; it must be omitted entirely", lang, q.Get("language"))
		}
	}
}

func TestBuildURLSendsAConcreteLanguage(t *testing.T) {
	q := parseQuery(t, BuildURL("es"))
	if got := q.Get("language"); got != "es" {
		t.Errorf("language = %q, want %q", got, "es")
	}
}

// Grok STT has no model parameter — it is a single fixed model billed per hour. Sending one
// is how you accidentally address the Voice Agent API instead (which does take a model).
func TestBuildURLSendsNoModel(t *testing.T) {
	q := parseQuery(t, BuildURL("es"))
	if _, present := q["model"]; present {
		t.Errorf("BuildURL sent a model parameter (%q); this endpoint has none", q.Get("model"))
	}
}
