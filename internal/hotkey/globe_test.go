// Ported from the Electron suite (test/unit/globeProtocol.test.ts).
package hotkey

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLine(t *testing.T) {
	cases := map[string]Event{
		"FN_DOWN":        FnDown,
		"FN_UP":          FnUp,
		"FN_INTERRUPTED": FnInterrupt,
		// Trailing whitespace is normal: the helper writes a newline and the reader may
		// hand over a carriage return on some pipes.
		"FN_DOWN\r": FnDown,
		"  FN_UP  ": FnUp,
		// Lines the helper emits for triggers Loqui does not use. Ignored on purpose, so
		// the vendored Swift stays byte-identical to upstream OpenWhispr.
		"RIGHT_MOD_DOWN:RightOption":     "",
		"MODIFIER_UP:shift":              "",
		"MOUSE_BUTTON_DOWN:MouseButton4": "",
		"":                               "",
		"nonsense":                       "",
		"fn_down":                        "", // case-sensitive: the protocol is uppercase
	}
	for line, want := range cases {
		if got := ParseLine(line); got != want {
			t.Errorf("ParseLine(%q) = %q, want %q", line, got, want)
		}
	}
}

// The app must be able to say "the fn listener isn't built" rather than appearing to
// register a trigger that will never fire.
func TestStartReportsAMissingBinary(t *testing.T) {
	_, err := Start(filepath.Join(t.TempDir(), "does-not-exist"), Handlers{})
	if err == nil {
		t.Fatal("expected an error for a missing helper binary")
	}
}

// The real helper needs Input Monitoring and an AppKit run loop, so the lifecycle is
// exercised with a stand-in that just prints the protocol.
func TestListenerReadsEventsAndStops(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-listener.sh")
	body := "#!/bin/sh\n" +
		"echo FN_DOWN\n" +
		"echo RIGHT_MOD_DOWN:RightOption\n" + // must be ignored
		"echo FN_UP\n" +
		"echo 'helper diagnostic' >&2\n" +
		"sleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	downs := make(chan struct{}, 4)
	ups := make(chan struct{}, 4)
	errs := make(chan string, 4)

	l, err := Start(script, Handlers{
		OnFnDown: func() { downs <- struct{}{} },
		OnFnUp:   func() { ups <- struct{}{} },
		OnStderr: func(s string) { errs <- s },
		OnExit:   func(error) { t.Error("OnExit fired for a stop we requested") },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Stop()

	<-downs
	<-ups
	if got := <-errs; got != "helper diagnostic" {
		t.Errorf("stderr = %q, want %q", got, "helper diagnostic")
	}
	if len(downs) != 0 || len(ups) != 0 {
		t.Error("unrecognised lines must not produce events")
	}
}

// An unexpected death has to be reported, because nothing will ever send the matching
// key-up and the session must be failed closed.
func TestListenerReportsAnUnexpectedExit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "dying-listener.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho FN_DOWN\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	exited := make(chan error, 1)
	l, err := Start(script, Handlers{OnExit: func(err error) { exited <- err }})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Stop()

	if err := <-exited; err == nil {
		t.Error("a non-zero exit must be reported as an error")
	}
}

func TestStopIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "sleeper.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := Start(script, Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	l.Stop()
	l.Stop() // must not panic or block
}
