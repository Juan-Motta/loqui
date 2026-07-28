// The dictation service: the two things the UI needs to drive a dictation.
//
// A SEPARATE TYPE FROM Dictation, deliberately. Dictation's exported methods are its
// session.IO implementation — StartEngine, StopEngine, PasteText and the rest — and binding it
// directly would publish every one of them to anything running script in the settings window.
// A service's exported methods ARE its public API, so the surface has to be chosen, not inherited.
package app

// DictationControl is the slice of the session controller this service drives.
//
// An interface rather than *Dictation, and narrow on purpose. Toggling really does start the engine
// — opening the microphone and spawning a provider — so a service that depended on the whole engine
// could only be tested by standing one up. Three methods is also an honest statement of what the
// Home button is: it does not configure anything, it flips one bit of desired state.
type DictationControl interface {
	// Desired reports whether a dictation is meant to be running.
	Desired() bool
	// Press is the same input the trigger key gives.
	Press()
	// RequestStop is a deliberate stop, which drains a trailing final into the message rather than
	// discarding it. Interrupt is the other one, and it is not this: that is for the user typing
	// over a held dictation, where the sounds were never meant to become text.
	RequestStop()
}

// DictationService is what the Home view's record button talks to.
type DictationService struct {
	// control resolves the running controller at CALL time rather than construction time.
	//
	// It has to: services are registered as an application option, so this exists before
	// startDictation has built the engine — which cannot happen earlier, because the engine needs
	// the windows and the tray that the application itself creates.
	control func() DictationControl
}

func NewDictationService(control func() DictationControl) *DictationService {
	return &DictationService{control: control}
}

// ServiceName is what Wails calls this in its logs.
func (s *DictationService) ServiceName() string { return "Dictation" }

// Toggle starts dictation, or stops it if it is running. Returns whether it is now active.
//
// The same pair the tray item uses, so there is one way to start a dictation regardless of which
// control the user reached for.
func (s *DictationService) Toggle() bool {
	c := s.control()
	if c == nil {
		return false
	}
	if c.Desired() {
		c.RequestStop()
		return false
	}
	c.Press()
	return true
}

// Active reports whether a dictation is running, so a freshly loaded page can label its button
// correctly instead of assuming idle — the trigger key and the tray can both start one with this
// window closed.
func (s *DictationService) Active() bool {
	c := s.control()
	if c == nil {
		return false
	}
	return c.Desired()
}
