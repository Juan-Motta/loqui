package azure

import (
	"testing"

	"github.com/Microsoft/cognitive-services-speech-sdk-go/common"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// The reconnect policy branches on these exact strings, so a change in how the SDK
// renders them is a behaviour change dressed up as a dependency bump: "AuthenticationFailure"
// silently becoming something else would turn a fatal auth error into an endless retry
// loop against a key that will never work.
func TestErrorCodeNameMatchesTheNamesThePolicyExpects(t *testing.T) {
	cases := map[common.CancellationErrorCode]string{
		common.NoError:               "NoError",
		common.AuthenticationFailure: "AuthenticationFailure",
		common.BadRequest:            "BadRequest",
		common.TooManyRequests:       "TooManyRequests",
		common.Forbidden:             "Forbidden",
		common.ConnectionFailure:     "ConnectionFailure",
		common.ServiceTimeout:        "ServiceTimeout",
		common.ServiceError:          "ServiceError",
		common.ServiceUnavailable:    "ServiceUnavailable",
		common.RuntimeError:          "RuntimeError",
	}
	for code, want := range cases {
		if got := errorCodeName(code); got != want {
			t.Errorf("errorCodeName(%d) = %q, want %q", int(code), got, want)
		}
	}
}

// A code this SDK version has never heard of must still produce something, not panic.
func TestErrorCodeNameHandlesUnknownCodes(t *testing.T) {
	if got := errorCodeName(common.CancellationErrorCode(999)); got == "" {
		t.Error("expected a non-empty rendering for an unknown code")
	}
}

func TestRecognizerWantsAudio(t *testing.T) {
	if !New(Config{}).WantsAudio() {
		t.Error("the Azure provider is fed by the host's capture, so it must want audio")
	}
}

// A misconfigured start must not merely return an error: the session controller drives
// its state machine off events, so it has to SEE the failure and the teardown, or it
// sits there believing a dictation is live.
func TestStartWithoutTokensEmitsCanceledThenStopped(t *testing.T) {
	var got []stt.Event
	r := New(Config{Region: "eastus", Candidates: []string{"es-CO", "en-US"}}) // Tokens nil

	if err := r.Start(7, func(e stt.Event) { got = append(got, e) }); err == nil {
		t.Fatal("expected an error with no token service configured")
	}

	if len(got) != 2 {
		t.Fatalf("got %d events (%+v), want canceled then stopped", len(got), got)
	}
	if got[0].Type != stt.Canceled || got[1].Type != stt.Stopped {
		t.Errorf("got %v then %v, want canceled then stopped", got[0].Type, got[1].Type)
	}
	// NotConfigured is what tells the policy never to retry: only the user can fix it.
	if got[0].ErrorCode != "NotConfigured" {
		t.Errorf("errorCode = %q, want NotConfigured", got[0].ErrorCode)
	}
	for i, e := range got {
		if e.Gen != 7 {
			t.Errorf("event %d has gen %d, want 7 — untagged events cannot be filtered as stale", i, e.Gen)
		}
	}
}

// The candidate rules are Azure's, and breaking them degrades detection silently, so the
// provider must refuse before opening a session rather than dictate badly.
func TestStartRejectsInvalidCandidates(t *testing.T) {
	var got []stt.Event
	r := New(Config{Region: "eastus", Candidates: []string{"es-CO", "es-ES"}})

	if err := r.Start(1, func(e stt.Event) { got = append(got, e) }); err == nil {
		t.Fatal("expected an error for two locales of the same base language")
	}
	if len(got) == 0 || got[0].Type != stt.Canceled {
		t.Fatalf("got %+v, want a canceled event first", got)
	}
}

func TestStartRejectsAnInvalidRegion(t *testing.T) {
	r := New(Config{Region: "east/us", Candidates: []string{"en-US"}})
	if err := r.Start(1, func(stt.Event) {}); err == nil {
		t.Error("expected an error for an invalid region")
	}
}

func TestStartTwiceIsRefused(t *testing.T) {
	r := New(Config{Region: "east/us", Candidates: []string{"en-US"}})
	_ = r.Start(1, func(stt.Event) {})
	if err := r.Start(2, func(stt.Event) {}); err == nil {
		t.Error("a recognizer is single-use; a second Start must be refused")
	}
}

// Stop before Start, and PushAudio with nothing open, are both things the session
// controller can legitimately do when a dictation is cancelled during startup.
func TestStopAndPushAreSafeBeforeStart(t *testing.T) {
	r := New(Config{})
	r.PushAudio([]byte{1, 2, 3, 4})
	r.Stop()
}
