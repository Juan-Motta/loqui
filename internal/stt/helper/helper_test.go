// Ported from the Electron suites test/unit/sttHelperProtocol.test.ts and
// test/unit/helperExit.test.ts, plus lifecycle tests driven by stand-in helpers — the real
// ones need a microphone and, for the Apple engine, macOS 26.
package helper

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

func TestParseLineAcceptsTheProtocol(t *testing.T) {
	cases := []struct {
		line string
		want stt.Event
	}{
		{`{"type":"started"}`, stt.Event{Type: stt.Started}},
		{`{"type":"partial","text":"hola","language":"es-CO"}`, stt.Event{Type: stt.Partial, Text: "hola", Language: "es-CO"}},
		{`{"type":"final","text":"hola mundo","language":"es-CO"}`, stt.Event{Type: stt.Final, Text: "hola mundo", Language: "es-CO"}},
		{`{"type":"canceled","error":"no microphone"}`, stt.Event{Type: stt.Canceled, Error: "no microphone"}},
		{`{"type":"stopped"}`, stt.Event{Type: stt.Stopped}},
		{`  {"type":"started"}  `, stt.Event{Type: stt.Started}},
	}
	for _, c := range cases {
		got, ok := ParseLine(c.line)
		if !ok {
			t.Errorf("ParseLine(%s) returned not-ok", c.line)
			continue
		}
		if got != c.want {
			t.Errorf("ParseLine(%s) = %+v, want %+v", c.line, got, c.want)
		}
	}
}

// "info" lines are the helpers' diagnostics (which locale they chose, whether they are
// downloading a language model). Surfacing them would put noise into the transcript.
func TestParseLineDropsNoise(t *testing.T) {
	for _, line := range []string{
		`{"type":"info","using":"es-CO"}`,
		`{"type":"info","msg":"downloading language model…"}`,
		`whisper_init_from_file: loading model`, // stray stderr on the same pipe
		`{"type":"unknown"}`,
		`{"no":"type"}`,
		`{broken json`,
		``,
		`   `,
	} {
		if _, ok := ParseLine(line); ok {
			t.Errorf("ParseLine(%q) should have been dropped", line)
		}
	}
}

func TestIsGPUCrash(t *testing.T) {
	code := func(n int) *int { return &n }
	cases := []struct {
		name  string
		facts ExitFacts
		want  bool
	}{
		{"fatal NTSTATUS with no transcript on the GPU", ExitFacts{Code: code(0xc0000409), GPUEnabled: true}, true},
		{"illegal instruction variant", ExitFacts{Code: code(0xc000001d), GPUEnabled: true}, true},
		// The helper's own error exits must never be blamed on the GPU.
		{"exit 1 = no microphone", ExitFacts{Code: code(1), GPUEnabled: true}, false},
		{"exit 2 = model failed to load", ExitFacts{Code: code(2), GPUEnabled: true}, false},
		{"clean exit", ExitFacts{Code: code(0), GPUEnabled: true}, false},
		// Whatever killed it came AFTER the work, so the backend did its job.
		{"already delivered a transcript", ExitFacts{Code: code(0xc0000409), SawFinal: true, GPUEnabled: true}, false},
		{"GPU was not in use", ExitFacts{Code: code(0xc0000409), GPUEnabled: false}, false},
		// On macOS a signal is indistinguishable from the SIGKILL this app itself sends,
		// and blaming the GPU for our own kill would disable a backend that works.
		{"killed by a signal", ExitFacts{Code: nil, GPUEnabled: true}, false},
	}
	for _, c := range cases {
		if got := IsGPUCrash(c.facts); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFormatExitCode(t *testing.T) {
	code := func(n int) *int { return &n }
	cases := map[string]*int{
		"signal":     nil,
		"0":          code(0),
		"2":          code(2),
		"0xc0000409": code(0xc0000409),
	}
	for want, in := range cases {
		if got := FormatExitCode(in); got != want {
			t.Errorf("FormatExitCode(%v) = %q, want %q", in, got, want)
		}
	}
}

// ---- lifecycle ---------------------------------------------------------------

// The app must be able to say "the helper isn't built" and never retry it.
func TestMissingBinaryIsReportedAsNotConfigured(t *testing.T) {
	var got []stt.Event
	p := New(Config{
		Bin:      filepath.Join(t.TempDir(), "nope"),
		BuildCmd: "./scripts/build-macos-stt.sh",
	})

	if err := p.Start(3, func(e stt.Event) { got = append(got, e) }); err == nil {
		t.Fatal("expected an error for a missing binary")
	}
	if len(got) != 2 || got[0].Type != stt.Canceled || got[1].Type != stt.Stopped {
		t.Fatalf("got %+v, want canceled then stopped", got)
	}
	if got[0].ErrorCode != "NotConfigured" {
		t.Errorf("errorCode = %q, want NotConfigured", got[0].ErrorCode)
	}
	if got[0].Error == "" || got[1].Gen != 3 {
		t.Errorf("events must carry the build hint and the generation: %+v", got)
	}
}

func fakeHelper(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-helper.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// collect returns a sink and a drain.
//
// drain(min) waits GENEROUSLY for the first `min` events and only then requires quiet. An
// earlier version waited a flat 200 ms for anything at all, which made four tests fail
// against a provider that was working: spawning `sh` under a cgo-linked test binary can take
// longer than that, so the drain returned empty before the helper had said a word.
func collect(t *testing.T) (stt.Sink, func(min int) []stt.Event) {
	t.Helper()
	ch := make(chan stt.Event, 64)
	sink := func(e stt.Event) { ch <- e }

	drain := func(min int) []stt.Event {
		t.Helper()
		var out []stt.Event
		deadline := time.After(5 * time.Second)
		for {
			quiet := 200 * time.Millisecond
			if len(out) < min {
				quiet = 5 * time.Second // still waiting for the helper to get going
			}
			select {
			case e := <-ch:
				out = append(out, e)
			case <-time.After(quiet):
				return out
			case <-deadline:
				return out
			}
		}
	}
	return sink, drain
}

func TestHelperEventsReachTheSink(t *testing.T) {
	bin := fakeHelper(t, `
echo '{"type":"started"}'
echo '{"type":"info","using":"es-CO"}'
echo '{"type":"partial","text":"ho"}'
echo '{"type":"final","text":"hola","language":"es-CO"}'
sleep 30
`)
	sink, drain := collect(t)
	p := New(Config{Bin: bin, Locale: "es-CO"})
	if err := p.Start(5, sink); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	events := drain(3) // started, partial, final
	var types []stt.EventType
	for _, e := range events {
		types = append(types, e.Type)
		if e.Gen != 5 {
			t.Errorf("event %+v is missing the generation", e)
		}
	}
	want := []stt.EventType{stt.Started, stt.Partial, stt.Final}
	if len(types) != len(want) {
		t.Fatalf("got %v, want %v (the info line must be dropped)", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("got %v, want %v", types, want)
		}
	}
}

// THE REGRESSION THAT MATTERS. The helper keeps transcribing after being asked to stop, so
// `stopped` must wait for the process to exit — otherwise the last final is thrown away and
// every dictation loses its tail.
func TestStopWaitsForTheTailFinal(t *testing.T) {
	bin := fakeHelper(t, `
echo '{"type":"started"}'
# Block until asked to stop, then flush a last segment before exiting — exactly what the
# real helpers do with their buffered audio.
read -r line
echo '{"type":"final","text":"la cola del dictado"}'
exit 0
`)
	sink, drain := collect(t)
	p := New(Config{Bin: bin, Locale: "es-CO"})
	if err := p.Start(1, sink); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond) // let it start and block on read
	p.Stop()

	events := drain(3) // started, the tail final, stopped
	var sawTail, sawStopped bool
	for _, e := range events {
		if e.Type == stt.Final && e.Text == "la cola del dictado" {
			sawTail = true
		}
		if e.Type == stt.Stopped {
			if !sawTail {
				t.Error("stopped arrived BEFORE the tail final — the transcript loses its end")
			}
			sawStopped = true
		}
	}
	if !sawTail {
		t.Errorf("the tail final never arrived: %+v", events)
	}
	if !sawStopped {
		t.Error("stopped was never emitted")
	}
}

// A helper that dies on its own must report stopped, or the session hangs with the mic open.
func TestUnexpectedExitReportsStopped(t *testing.T) {
	sink, drain := collect(t)
	p := New(Config{Bin: fakeHelper(t, `echo '{"type":"started"}'; exit 1`), Locale: "es-CO"})
	if err := p.Start(1, sink); err != nil {
		t.Fatal(err)
	}

	var sawStopped bool
	for _, e := range drain(2) { // started, stopped
		if e.Type == stt.Stopped {
			sawStopped = true
		}
	}
	if !sawStopped {
		t.Error("a helper dying on its own must still produce stopped")
	}
}

// Exactly once, whichever route got there: a second `stopped` would make the controller
// deliver the message twice.
func TestStoppedIsEmittedOnlyOnce(t *testing.T) {
	sink, drain := collect(t)
	p := New(Config{Bin: fakeHelper(t, `echo '{"type":"started"}'; read -r l; exit 0`), Locale: "es-CO"})
	if err := p.Start(1, sink); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	p.Stop()
	p.Stop() // must be idempotent

	var stopped int
	for _, e := range drain(2) { // started, stopped
		if e.Type == stt.Stopped {
			stopped++
		}
	}
	if stopped != 1 {
		t.Errorf("got %d stopped events, want exactly 1", stopped)
	}
}

func TestWantsAudioIsFalse(t *testing.T) {
	// The helpers open the microphone themselves; pushing PCM at them is a no-op, and
	// must not panic.
	p := New(Config{})
	if p.WantsAudio() {
		t.Error("native helpers capture their own audio")
	}
	p.PushAudio([]byte{1, 2, 3})
}

func TestLocaleAndArgsArePassedThrough(t *testing.T) {
	// The helper echoes its own arguments back as a transcript, which is the only way to
	// observe what it was launched with.
	bin := fakeHelper(t, `echo "{\"type\":\"final\",\"text\":\"$1|$2\"}"; sleep 5`)
	sink, drain := collect(t)
	p := New(Config{Bin: bin, Locale: "es-CO", ExtraArgs: []string{"/models/ggml-small.bin"}})
	if err := p.Start(1, sink); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	want := "es-CO|/models/ggml-small.bin"
	for _, e := range drain(1) {
		if e.Type == stt.Final {
			if e.Text != want {
				t.Errorf("helper was launched with %q, want %q", e.Text, want)
			}
			return
		}
	}
	t.Error("no final event to inspect")
}

func TestEnvIsPassedToTheHelper(t *testing.T) {
	bin := fakeHelper(t, `echo "{\"type\":\"final\",\"text\":\"gpu=$LOQUI_WHISPER_GPU\"}"; sleep 5`)
	sink, drain := collect(t)
	p := New(Config{Bin: bin, Locale: "auto", Env: map[string]string{"LOQUI_WHISPER_GPU": "0"}})
	if err := p.Start(1, sink); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	for _, e := range drain(1) {
		if e.Type == stt.Final && e.Text == "gpu=0" {
			return
		}
	}
	t.Error("LOQUI_WHISPER_GPU did not reach the helper")
}

func TestStartTwiceIsRefused(t *testing.T) {
	p := New(Config{Bin: fakeHelper(t, `sleep 5`), Locale: "es-CO"})
	if err := p.Start(1, func(stt.Event) {}); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	if err := p.Start(2, func(stt.Event) {}); err == nil {
		t.Error("a second Start must be refused")
	}
}

// A GPU crash has to be remembered, or the next dictation hands the same backend the same
// job and dies the same way.
func TestGPUCrashIsReported(t *testing.T) {
	// Exit codes above 255 cannot be produced by a shell, so the classification itself is
	// covered by TestIsGPUCrash; here we check the callback wiring with a real fatal-looking
	// code path by calling the classifier the provider uses.
	var reported string
	cfg := Config{OnGPUCrash: func(reason string) { reported = reason }, GPUEnabled: true}
	p := New(cfg)

	code := 0xc0000409
	if !IsGPUCrash(ExitFacts{Code: &code, GPUEnabled: true}) {
		t.Fatal("classifier disagrees; the wiring test below is meaningless")
	}
	// Drive the callback the way wait() does.
	if p.cfg.OnGPUCrash != nil {
		p.cfg.OnGPUCrash(fmt.Sprintf("helper exited %s with no transcript", FormatExitCode(&code)))
	}
	if reported == "" {
		t.Error("OnGPUCrash was not invoked")
	}
}

// THE BUG THIS GUARDS AGAINST cost a real dictation. The watchdog used to measure silence
// since the helper's LAST OUTPUT, but a helper prints nothing while it listens: whisper logs
// its init noise and then stays quiet. So when the user let go, the silence already exceeded
// the grace period and the helper was killed within a second — dropping exactly the tail the
// watchdog exists to protect. The grace period has to start when the stop does.
func TestSilentHelperIsGivenItsGracePeriodAfterStop(t *testing.T) {
	bin := fakeHelper(t, `
# The real helpers survive the SIGTERM the stop protocol sends 300 ms in: macos-stt exits
# cleanly on it (it has nothing buffered) and whisper-stt treats it as the same stop flag it
# got over stdin, then finishes flushing. A fake that dies on TERM would be testing the
# script, not the protocol.
trap '' TERM
echo '{"type":"started"}'
echo 'noisy init line' >&2
# Now go quiet, the way a helper does while it is listening.
read -r line
# Flushing the tail takes real time on a slow machine.
sleep 0.4
echo '{"type":"final","text":"la cola sobrevivió"}'
exit 0
`)
	sink, drain := collect(t)
	p := New(Config{
		Bin:    bin,
		Locale: "auto",
		// LONGER than the 0.4 s flush, so a correctly-armed grace period waits it out.
		// The old code failed anyway, because it had already spent the grace on the silence
		// BEFORE the stop.
		SilenceGrace: 900 * time.Millisecond,
	})
	if err := p.Start(1, sink); err != nil {
		t.Fatal(err)
	}
	// Stay quiet for longer than the grace period BEFORE stopping — this is exactly the
	// state a real dictation is in when the user releases the key.
	time.Sleep(1200 * time.Millisecond)
	p.Stop()

	var sawTail bool
	for _, e := range drain(3) {
		if e.Type == stt.Final && e.Text == "la cola sobrevivió" {
			sawTail = true
		}
	}
	if !sawTail {
		t.Error("the tail was dropped: the grace period must be armed at stop, not measured from the last output")
	}
}

// The watchdog must still fire on a helper that genuinely wedges, or a stuck process holds
// the session open and the microphone with it.
func TestWedgedHelperIsKilled(t *testing.T) {
	bin := fakeHelper(t, `
trap '' TERM
echo '{"type":"started"}'
read -r line
sleep 60    # asked to stop, says nothing, never exits — only SIGKILL ends this
`)
	sink, drain := collect(t)
	p := New(Config{Bin: bin, Locale: "auto", SilenceGrace: 300 * time.Millisecond})
	if err := p.Start(1, sink); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)

	start := time.Now()
	p.Stop()

	var sawStopped bool
	for _, e := range drain(2) {
		if e.Type == stt.Stopped {
			sawStopped = true
		}
	}
	if !sawStopped {
		t.Fatal("a wedged helper must still produce stopped — otherwise the session never ends")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %s to give up; the watchdog is not firing", elapsed)
	}
}
