// Ported from the Electron suite (test/unit/azureConfig.test.ts) case for case.
package settings

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeRegionStripsSpacesAndLowercases(t *testing.T) {
	got, err := NormalizeRegion("East US 2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "eastus2" {
		t.Errorf("got %q, want %q", got, "eastus2")
	}
}

func TestNormalizeRegionPassesCanonicalIDThrough(t *testing.T) {
	got, err := NormalizeRegion("brazilsouth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "brazilsouth" {
		t.Errorf("got %q, want %q", got, "brazilsouth")
	}
}

func TestNormalizeRegionRejectsEmptyAndInvalid(t *testing.T) {
	for _, in := range []string{"", "   ", "east/us", "east_us"} {
		if _, err := NormalizeRegion(in); err == nil {
			t.Errorf("NormalizeRegion(%q): expected an error", in)
		} else if !strings.Contains(strings.ToLower(err.Error()), "region") {
			t.Errorf("NormalizeRegion(%q): error should mention the region, got %v", in, err)
		}
	}
}

func TestBuildV2Endpoint(t *testing.T) {
	const want = "wss://eastus2.stt.speech.microsoft.com/speech/universal/v2"
	for _, in := range []string{"eastus2", "East US 2"} {
		got, err := BuildV2Endpoint(in)
		if err != nil {
			t.Fatalf("BuildV2Endpoint(%q): unexpected error: %v", in, err)
		}
		if got != want {
			t.Errorf("BuildV2Endpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildV2EndpointPropagatesRegionError(t *testing.T) {
	if _, err := BuildV2Endpoint(""); err == nil {
		t.Error("expected an error for an empty region")
	}
}

func TestBaseLanguage(t *testing.T) {
	cases := map[string]string{"es-CO": "es", "EN-us": "en", "es": "es"}
	for in, want := range cases {
		if got := BaseLanguage(in); got != want {
			t.Errorf("BaseLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateCandidatesAcceptsOneLocalePerLanguage(t *testing.T) {
	in := []string{"es-CO", "en-US"}
	got, err := ValidateCandidates(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("got %v, want %v", got, in)
	}
}

func TestValidateCandidatesRejectsDuplicateBaseLanguage(t *testing.T) {
	_, err := ValidateCandidates([]string{"es-CO", "es-ES"})
	if err == nil {
		t.Fatal("expected an error for two Spanish locales")
	}
	if !strings.Contains(err.Error(), "base language") {
		t.Errorf("error should explain the base-language rule, got %v", err)
	}
}

func TestValidateCandidatesRejectsEmpty(t *testing.T) {
	if _, err := ValidateCandidates(nil); err == nil {
		t.Error("expected an error for an empty list")
	}
}

func TestValidateCandidatesRejectsMoreThanTen(t *testing.T) {
	many := make([]string, 11)
	for i := range many {
		many[i] = fmt.Sprintf("x%d-XX", i)
	}
	_, err := ValidateCandidates(many)
	if err == nil {
		t.Fatal("expected an error for 11 candidates")
	}
	if !strings.Contains(err.Error(), "at most 10") {
		t.Errorf("error should state the limit, got %v", err)
	}
}

func TestValidateCandidatesRejectsMalformedLocale(t *testing.T) {
	for _, bad := range []string{"es", "e-US", "spanish", "es_CO"} {
		if _, err := ValidateCandidates([]string{bad}); err == nil {
			t.Errorf("ValidateCandidates([%q]): expected an error", bad)
		}
	}
}
