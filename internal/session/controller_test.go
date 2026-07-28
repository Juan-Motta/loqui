// Ported from the Electron suite (test/unit/sessionController.test.ts). These are the
// tests that were added after a regression slipped through, and they encode behaviour
// that is expensive to rediscover: what happens to a dictation's text when the network
// drops, when the user interrupts, when a late event arrives from a session that already
// ended.
package session

import (
	"reflect"
	"testing"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// fakeIO records every side effect, which is how these tests assert on decisions rather
// than on internal state.
type fakeIO struct {
	starts     []int
	stops      []int
	shows      int
	hides      int
	overlays   []OverlayState
	delivered  []delivery
	reconnects []time.Duration
	pending    []func()
}

type delivery struct {
	text     string
	language string
	trigger  Mode
}

func (f *fakeIO) StartEngine(gen int)        { f.starts = append(f.starts, gen) }
func (f *fakeIO) StopEngine(gen int)         { f.stops = append(f.stops, gen) }
func (f *fakeIO) ShowOverlay()               { f.shows++ }
func (f *fakeIO) HideOverlay()               { f.hides++ }
func (f *fakeIO) Overlay(state OverlayState) { f.overlays = append(f.overlays, state) }
func (f *fakeIO) DeliverFinal(text, language string, trigger Mode) {
	f.delivered = append(f.delivered, delivery{text, language, trigger})
}
func (f *fakeIO) ScheduleReconnect(d time.Duration, fn func()) {
	f.reconnects = append(f.reconnects, d)
	f.pending = append(f.pending, fn)
}

func (f *fakeIO) texts() []string {
	out := make([]string, 0, len(f.delivered))
	for _, d := range f.delivered {
		out = append(out, d.text)
	}
	return out
}

func newFixture(mode Mode) (*Controller, *fakeIO) {
	io := &fakeIO{}
	return NewController(mode, io), io
}

// ---- hold mode happy path ----------------------------------------------------

func TestHoldPressStartsAndReleaseStops(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	if !reflect.DeepEqual(io.starts, []int{1}) {
		t.Fatalf("starts = %v, want [1]", io.starts)
	}
	if io.shows != 1 {
		t.Errorf("shows = %d, want 1", io.shows)
	}
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.Release()
	if !reflect.DeepEqual(io.stops, []int{1}) {
		t.Errorf("stops = %v, want [1]", io.stops)
	}
	if io.hides != 1 {
		t.Errorf("hides = %d, want 1", io.hides)
	}
}

// ---- safety: fail closed -----------------------------------------------------

// The mic must not be left open by a tap so short that the release beat the engine.
func TestEarlyReleaseStopsOnceStarted(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.Release() // before 'started'
	if len(io.stops) != 0 {
		t.Fatalf("stops = %v, want none yet (nothing is running to stop)", io.stops)
	}
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	if len(io.stops) != 1 {
		t.Errorf("stops = %v, want exactly one — the mic must not stay open", io.stops)
	}
}

func TestHelperFailureFailsClosed(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.HelperFailed()
	if len(io.stops) != 1 {
		t.Errorf("stops = %v, want one: a dead listener cannot leave the mic open", io.stops)
	}
}

// A missed key-up (a lost event, Secure Keyboard Entry, a Space switch) shows up as a
// second key-DOWN while listening. Fail closed.
func TestHoldSecondPressWhileListeningStops(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.Press()
	if len(io.stops) != 1 {
		t.Errorf("stops = %v, want one", io.stops)
	}
}

// ---- generation gating -------------------------------------------------------

func TestDeliversTheSessionText(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "Hola.", Language: "es-CO"})
	c.Release()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	if want := []string{"Hola."}; !reflect.DeepEqual(io.texts(), want) {
		t.Errorf("delivered %v, want %v", io.texts(), want)
	}
}

func TestDiscardedFinalIsNotDelivered(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.Interrupt() // discards gen 1
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "ruido", Language: "es-CO"})
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	if len(io.delivered) != 0 {
		t.Errorf("delivered %v, want nothing — the user cancelled", io.delivered)
	}
}

// This is the one that puts last minute's words into this minute's window.
func TestStaleFinalFromSupersededGenerationIsIgnored(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.Press() // toggle off
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	c.Press() // gen 2
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 2})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "late", Language: "es-CO"})
	for _, d := range io.delivered {
		if d.text == "late" {
			t.Error("a final from a finished session must never be delivered")
		}
	}
}

func TestBlankFinalsAreSkipped(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "   ", Language: "es-CO"})
	c.Release()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	if len(io.delivered) != 0 {
		t.Errorf("delivered %v, want nothing for whitespace", io.delivered)
	}
}

// ---- stop semantics ----------------------------------------------------------

func TestRequestStopDrainsATrailingFinal(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.RequestStop() // user-requested, not a discard
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "drenado", Language: "es-CO"})
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	if want := []string{"drenado"}; !reflect.DeepEqual(io.texts(), want) {
		t.Errorf("delivered %v, want %v — the tail must survive a normal stop", io.texts(), want)
	}
}

func TestRecoversFromAStopIssuedWhileStarting(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.StopByGuard() // before 'started'
	if len(io.stops) != 1 {
		t.Fatalf("stops = %v, want one", io.stops)
	}
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	before := len(io.starts)
	c.Press() // the machine must be idle again, not wedged
	if len(io.starts) != before+1 {
		t.Error("a stop during startup must not wedge the machine")
	}
}

func TestSetModeOnlyAppliesWhenIdle(t *testing.T) {
	c, _ := newFixture(ModeHold)
	c.SetMode(ModeToggle)
	if c.Mode() != ModeToggle {
		t.Errorf("mode = %q, want toggle", c.Mode())
	}

	c.Press() // now mid-session
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.SetMode(ModeHold)
	if c.Mode() != ModeToggle {
		t.Error("the mode must not change mid-session: the machine would then wait for an event that never comes")
	}
}

// ---- reconnect ---------------------------------------------------------------

func TestReconnectUsesANewGeneration(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: 1, Error: "1006 network"})
	// The dead recognizer's own teardown must not be mistaken for the retry finishing.
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	if !c.Desired() {
		t.Fatal("still reconnecting, so dictation is still desired")
	}
	if len(io.pending) != 1 {
		t.Fatalf("pending reconnects = %d, want 1", len(io.pending))
	}
	io.pending[0]()
	found := false
	for _, g := range io.starts {
		if g == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("starts = %v, want one at generation 2", io.starts)
	}
}

func TestReconnectAttemptsAreCapped(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	for i := 0; i < 8; i++ {
		c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: c.Generation(), Error: "1006 network"})
	}
	if len(io.reconnects) > maxReconnects {
		t.Errorf("scheduled %d reconnects, want at most %d — unbounded retry is a billing loop",
			len(io.reconnects), maxReconnects)
	}
	if len(io.stops) < 1 {
		t.Error("after the cap it must actually stop")
	}
}

func TestBackoffGrows(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	for i := 0; i < 3; i++ {
		c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: c.Generation(), Error: "1006 network"})
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if !reflect.DeepEqual(io.reconnects, want) {
		t.Errorf("delays = %v, want %v", io.reconnects, want)
	}
}

func TestNetworkCancelSchedulesAReconnect(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: 1, Error: "StatusCode: 1006 connection"})
	if len(io.reconnects) != 1 || len(io.stops) != 0 {
		t.Errorf("reconnects=%d stops=%d, want 1 and 0", len(io.reconnects), len(io.stops))
	}
}

func TestAuthCancelStopsWithoutRetrying(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: 1, Error: "401 Unauthorized"})
	if len(io.reconnects) != 0 || len(io.stops) != 1 {
		t.Errorf("reconnects=%d stops=%d, want 0 and 1", len(io.reconnects), len(io.stops))
	}
}

// The structured code wins over the message. This is the bug that made a translated app
// reconnect for ever: network-looking prose, but the real reason is a bad key.
func TestStructuredErrorCodeBeatsTheMessage(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{
		Type: stt.Canceled, Gen: 1,
		ErrorCode: "AuthenticationFailure",
		Error:     "1006", // looks transient, is not
	})
	if len(io.reconnects) != 0 {
		t.Error("AuthenticationFailure must never be retried, whatever the message says")
	}
	if len(io.stops) != 1 {
		t.Errorf("stops = %v, want one", io.stops)
	}
}

func TestTransientErrorCodeReconnects(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: 1, ErrorCode: "ConnectionFailure"})
	if len(io.reconnects) != 1 || len(io.stops) != 0 {
		t.Errorf("reconnects=%d stops=%d, want 1 and 0", len(io.reconnects), len(io.stops))
	}
}

func TestStaleCancelIsIgnored(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	c.Press() // gen 2
	c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: 1, Error: "1006"})
	if len(io.reconnects) != 0 {
		t.Error("a cancel from a finished session must not trigger a reconnect")
	}
}

// ---- one session is one message ---------------------------------------------

func TestEveryFinalOfASessionBecomesOneDelivery(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	for _, s := range []string{"Hola.", "¿Cómo estás?", "Todo bien."} {
		c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: s, Language: "es-CO"})
	}
	c.Release()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})

	want := []string{"Hola. ¿Cómo estás? Todo bien."}
	if !reflect.DeepEqual(io.texts(), want) {
		t.Errorf("delivered %v, want %v", io.texts(), want)
	}
}

func TestNothingIsDeliveredUntilTheSessionEnds(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "primero"})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "segundo"})
	if len(io.delivered) != 0 {
		t.Fatal("a VAD pause is not the end of the message")
	}
	c.Release()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	if len(io.delivered) != 1 {
		t.Errorf("deliveries = %d, want 1", len(io.delivered))
	}
}

func TestToggleSecondPressEndsTheMessage(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "uno"})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "dos"})
	if len(io.delivered) != 0 {
		t.Fatal("delivered too early")
	}
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	if want := []string{"uno dos"}; !reflect.DeepEqual(io.texts(), want) {
		t.Errorf("delivered %v, want %v", io.texts(), want)
	}
}

func TestLanguageAndTriggerAreCarriedThrough(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "hi", Language: "en-US"})
	c.Release()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})

	if len(io.delivered) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(io.delivered))
	}
	if got := io.delivered[0]; got.language != "en-US" || got.trigger != ModeHold {
		t.Errorf("delivered %+v, want language en-US and trigger hold", got)
	}
}

func TestInterruptDropsTheWholeBufferedMessage(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "no quiero esto"})
	c.Interrupt()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	if len(io.delivered) != 0 {
		t.Errorf("delivered %v, want nothing", io.delivered)
	}
}

// Inactivity is not a cancellation: the user did say those words.
func TestIdleGuardDeliversWhatWasAlreadySaid(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "dicho antes del silencio"})
	c.StopByGuard()
	if want := []string{"dicho antes del silencio"}; !reflect.DeepEqual(io.texts(), want) {
		t.Errorf("delivered %v, want %v", io.texts(), want)
	}
}

func TestLateStoppedAfterAGuardStopDoesNotDeliverTwice(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "una vez"})
	c.StopByGuard()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	if len(io.delivered) != 1 {
		t.Errorf("deliveries = %d, want 1 — pasting twice is worse than not pasting", len(io.delivered))
	}
}

// A dropped connection mid-sentence must not split the message in two.
func TestReconnectKeepsTheMessageGoing(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "antes del corte"})
	c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: 1, Error: "1006 network"})
	if len(io.delivered) != 0 {
		t.Fatal("a reconnect is the same session; nothing should be delivered yet")
	}
	io.pending[0]()
	gen := c.Generation()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: gen})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: gen, Text: "después del corte"})
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: gen})

	want := []string{"antes del corte después del corte"}
	if !reflect.DeepEqual(io.texts(), want) {
		t.Errorf("delivered %v, want %v", io.texts(), want)
	}
}

func TestGivingUpStillDeliversWhatWasRecognized(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "algo dicho"})
	c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: 1, ErrorCode: "AuthenticationFailure"})
	if want := []string{"algo dicho"}; !reflect.DeepEqual(io.texts(), want) {
		t.Errorf("delivered %v, want %v — a failed session's words are still the user's", io.texts(), want)
	}
}

func TestANewSessionStartsFromAnEmptyMessage(t *testing.T) {
	c, io := newFixture(ModeHold)
	for i, text := range []string{"sesión uno", "sesión dos"} {
		gen := i + 1
		c.Press()
		c.ProviderEvent(stt.Event{Type: stt.Started, Gen: gen})
		c.ProviderEvent(stt.Event{Type: stt.Final, Gen: gen, Text: text})
		c.Release()
		c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: gen})
	}
	want := []string{"sesión uno", "sesión dos"}
	if !reflect.DeepEqual(io.texts(), want) {
		t.Errorf("delivered %v, want %v", io.texts(), want)
	}
}

// ---- overlay -----------------------------------------------------------------

func TestOverlayReachesListeningAndBackToIdle(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.Release()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})

	var sawListening, endedIdle bool
	for _, s := range io.overlays {
		if s.Status == OverlayListening {
			sawListening = true
		}
	}
	if len(io.overlays) > 0 {
		endedIdle = io.overlays[len(io.overlays)-1].Status == OverlayIdle
	}
	if !sawListening || !endedIdle {
		t.Errorf("overlay states = %+v, want listening then idle", io.overlays)
	}
}

// A cancel used to reach only the overlay, so pressing "Probar dictado" with a bad
// config produced a silent start/stop with no reason shown anywhere.
func TestOverlayShowsWhyItFailed(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{
		Type: stt.Canceled, Gen: 1,
		ErrorCode: "NotConfigured",
		Error:     "Configura región + clave en Ajustes",
	})

	var found bool
	for _, s := range io.overlays {
		if s.Status == OverlayError && s.Error == "Configura región + clave en Ajustes" {
			found = true
		}
	}
	if !found {
		t.Errorf("overlay states = %+v, want an error state carrying the reason", io.overlays)
	}
}

func TestOverlayReportsReconnecting(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: 1, ErrorCode: "ConnectionFailure"})

	var found bool
	for _, s := range io.overlays {
		if s.Status == OverlayReconnecting {
			found = true
		}
	}
	if !found {
		t.Errorf("overlay states = %+v, want a reconnecting state", io.overlays)
	}
}

// Partials arrive many times a second; repainting the pill for each is waste.
func TestPartialsDoNotRepaintTheOverlay(t *testing.T) {
	c, io := newFixture(ModeHold)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	before := len(io.overlays)
	for i := 0; i < 20; i++ {
		c.ProviderEvent(stt.Event{Type: stt.Partial, Gen: 1, Text: "hola"})
	}
	if len(io.overlays) != before {
		t.Errorf("overlay pushes went from %d to %d; partials must not repaint", before, len(io.overlays))
	}
}

// ---- re-entrancy ------------------------------------------------------------

// THE DEADLOCK THIS GUARDS AGAINST WAS REAL, and it only appeared when the app was wired
// to a live provider: Press took the mutex, called StartEngine inside it, the Azure
// recognizer failed on a bad key and reported Canceled synchronously, and ProviderEvent
// blocked forever on the lock Press was still holding. The microphone never opened and
// nothing was logged.
//
// It cannot happen in the Electron original — JavaScript is single-threaded and that
// version holds no lock — so it is a hazard the port introduced, and the only defence is
// that io calls run after the lock is released.
func TestSynchronousProviderFailureDoesNotDeadlock(t *testing.T) {
	io := &reentrantIO{}
	c := NewController(ModeHold, io)
	io.controller = c

	done := make(chan struct{})
	go func() {
		c.Press() // deadlocks here if an effect runs under the lock
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Press deadlocked: an io call is being made while the mutex is held")
	}

	// And the failure must have been handled, not merely survived.
	if c.Desired() {
		t.Error("after a fatal provider failure the session must not still be desired")
	}
}

// reentrantIO reports failure the way a real provider does: synchronously, from inside the
// StartEngine call itself.
type reentrantIO struct {
	controller *Controller
}

func (r *reentrantIO) StartEngine(gen int) {
	r.controller.ProviderEvent(stt.Event{
		Type: stt.Canceled, Gen: gen,
		ErrorCode: "NotConfigured", Error: "no key configured",
	})
	r.controller.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: gen})
}
func (r *reentrantIO) StopEngine(int)                          {}
func (r *reentrantIO) ShowOverlay()                            {}
func (r *reentrantIO) HideOverlay()                            {}
func (r *reentrantIO) Overlay(OverlayState)                    {}
func (r *reentrantIO) DeliverFinal(string, string, Mode)       {}
func (r *reentrantIO) ScheduleReconnect(time.Duration, func()) {}

// A provider that delivers a whole dictation synchronously from StartEngine — the local
// helpers can finish that fast on short audio — must not deadlock either.
func TestSynchronousFullSessionDoesNotDeadlock(t *testing.T) {
	io := &syncSessionIO{}
	c := NewController(ModeHold, io)
	io.controller = c

	done := make(chan struct{})
	go func() {
		c.Press()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deadlocked on a synchronous session")
	}
	if got := io.delivered; got != "hola" {
		t.Errorf("delivered %q, want %q", got, "hola")
	}
}

type syncSessionIO struct {
	controller *Controller
	delivered  string
}

func (s *syncSessionIO) StartEngine(gen int) {
	s.controller.ProviderEvent(stt.Event{Type: stt.Started, Gen: gen})
	s.controller.ProviderEvent(stt.Event{Type: stt.Final, Gen: gen, Text: "hola"})
	s.controller.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: gen})
}
func (s *syncSessionIO) StopEngine(int)       {}
func (s *syncSessionIO) ShowOverlay()         {}
func (s *syncSessionIO) HideOverlay()         {}
func (s *syncSessionIO) Overlay(OverlayState) {}
func (s *syncSessionIO) DeliverFinal(text, _ string, _ Mode) {
	s.delivered = text
}
func (s *syncSessionIO) ScheduleReconnect(time.Duration, func()) {}
