package app

import "testing"

// fakeControl records what the service asked of the controller, without a controller.
//
// The real one really does start dictation on Press — the microphone opens and a provider spawns —
// so a test that used it would be exercising the whole engine to check three lines of routing.
type fakeControl struct {
	desired  bool
	presses  int
	stops    int
	dropped  bool // set if the service ever asked for something contradictory
	lastCall string
}

func (f *fakeControl) Desired() bool { return f.desired }

func (f *fakeControl) Press() {
	if f.desired {
		f.dropped = true // pressing while already running is what RequestStop is for
	}
	f.presses++
	f.desired = true
	f.lastCall = "Press"
}

func (f *fakeControl) RequestStop() {
	if !f.desired {
		f.dropped = true
	}
	f.stops++
	f.desired = false
	f.lastCall = "RequestStop"
}

// The service must survive being called before the engine exists.
//
// Not hypothetical: services are registered as an application option, so this object is constructed
// BEFORE startDictation builds the engine — which cannot happen earlier, because the engine needs
// the windows and tray the application itself creates. A page that loads fast enough to call in
// between must get a plain answer rather than crash the window.
func TestTheDictationServiceToleratesNoEngineYet(t *testing.T) {
	svc := NewDictationService(func() DictationControl { return nil })

	if svc.Active() {
		t.Error("Active reported true with no engine")
	}
	if svc.Toggle() {
		t.Error("Toggle reported started with no engine")
	}
}

// Toggle starts when idle and stops when running: one control, two outcomes, and the returned value
// is what the button's label is drawn from.
func TestTogglingStartsThenStops(t *testing.T) {
	fake := &fakeControl{}
	svc := NewDictationService(func() DictationControl { return fake })

	if svc.Active() {
		t.Fatal("a fresh controller reports itself as already dictating")
	}

	if !svc.Toggle() {
		t.Fatal("Toggle did not report a started dictation")
	}
	if fake.presses != 1 || fake.lastCall != "Press" {
		t.Errorf("starting used %d presses and last called %q, want one Press", fake.presses, fake.lastCall)
	}
	if !svc.Active() {
		t.Error("Active disagrees with the Toggle that just started it")
	}

	if svc.Toggle() {
		t.Error("Toggle did not report the dictation stopped")
	}
	// RequestStop, not Interrupt: a deliberate stop must let a trailing final drain into the message
	// instead of discarding what was already recognised.
	if fake.stops != 1 || fake.lastCall != "RequestStop" {
		t.Errorf("stopping used %d stops and last called %q, want one RequestStop", fake.stops, fake.lastCall)
	}
	if svc.Active() {
		t.Error("still active after being stopped")
	}
	if fake.dropped {
		t.Error("the service sent an input the controller was not in a state for")
	}
}

// Toggling repeatedly must keep alternating rather than stacking presses — the button can be clicked
// as fast as the user likes.
func TestTogglingRepeatedlyAlternates(t *testing.T) {
	fake := &fakeControl{}
	svc := NewDictationService(func() DictationControl { return fake })

	for i := 0; i < 6; i++ {
		want := i%2 == 0 // starts on the even rounds
		if got := svc.Toggle(); got != want {
			t.Fatalf("round %d: Toggle = %v, want %v", i, got, want)
		}
	}
	if fake.presses != 3 || fake.stops != 3 {
		t.Errorf("presses=%d stops=%d, want 3 and 3", fake.presses, fake.stops)
	}
	if fake.dropped {
		t.Error("the service sent an input the controller was not in a state for")
	}
}
