// The hold/toggle dictation state machine. Ported from the Electron build's
// src/shared/dictationState.ts.
//
// It translates key and engine events into commands the controller executes, and holds
// no I/O of its own so every path through it is unit-testable.
//
// States: idle -> starting -> listening -> stopping -> idle.
//
// THE INVARIANT: FAIL CLOSED. Every ambiguity — a release that arrives before the engine
// started, a key-up that never came, an interrupt mid-session — resolves toward stopping
// the microphone, never toward leaving it open. An open microphone the user believes is
// closed is the worst failure this app can have, and it is also a billing leak on the
// cloud providers.
package session

// Mode is how the trigger key drives dictation.
type Mode string

const (
	// ModeHold records while the key is held; releasing it ends the dictation. Only
	// available for triggers that report key release (in practice, only fn).
	ModeHold Mode = "hold"
	// ModeToggle starts on one tap and stops on the next.
	ModeToggle Mode = "toggle"
)

// StateName is the machine's position in the lifecycle.
type StateName string

const (
	StateIdle      StateName = "idle"
	StateStarting  StateName = "starting"
	StateListening StateName = "listening"
	StateStopping  StateName = "stopping"
)

// CommandName is what the controller should do about a transition.
type CommandName string

const (
	// CommandNone means the event changed nothing the controller must act on.
	CommandNone  CommandName = ""
	CommandStart CommandName = "start"
	CommandStop  CommandName = "stop"
)

// Command is the machine's answer to an event.
type Command struct {
	Name CommandName
	// Discard marks the session's transcript as unwanted — set only by Interrupt,
	// which means a real external keypress cancelled the dictation. A plain stop keeps
	// the text, because the user meant to say it.
	Discard bool
}

var none = Command{Name: CommandNone}

// Machine is the state machine. Not safe for concurrent use; the controller owns it and
// serialises access.
type Machine struct {
	mode  Mode
	state StateName
	// pendingStop records a stop that arrived while the engine was still starting. It
	// cannot be acted on yet (there is nothing running to stop) but it must not be
	// dropped either, or the mic opens after the user has already let go.
	pendingStop bool
}

// NewMachine builds a machine in the idle state. An unknown mode falls back to hold
// rather than failing: the mode comes from stored settings, and refusing to start
// dictation because a config file was hand-edited is worse than picking the default.
func NewMachine(mode Mode) *Machine {
	if mode != ModeHold && mode != ModeToggle {
		mode = ModeHold
	}
	return &Machine{mode: mode, state: StateIdle}
}

func (m *Machine) Mode() Mode       { return m.mode }
func (m *Machine) State() StateName { return m.state }

// KeyDown handles a trigger press.
func (m *Machine) KeyDown() Command {
	if m.state == StateIdle {
		m.state = StateStarting
		m.pendingStop = false
		return Command{Name: CommandStart}
	}

	if m.mode == ModeToggle {
		switch m.state {
		case StateListening:
			m.state = StateStopping
			return Command{Name: CommandStop}
		case StateStarting:
			m.pendingStop = true // stop as soon as it finishes starting
		}
		return none
	}

	// Hold mode: being in `listening` on a key-DOWN means the key-up was missed
	// entirely (a lost event, a Space switch, Secure Keyboard Entry swallowing it).
	// Fail closed and stop.
	if m.state == StateListening {
		m.state = StateStopping
		return Command{Name: CommandStop}
	}
	return none // duplicate press while starting/stopping
}

// KeyUp handles a trigger release. Irrelevant in toggle mode, where holding means
// nothing.
func (m *Machine) KeyUp() Command {
	if m.mode == ModeToggle {
		return none
	}
	switch m.state {
	case StateListening:
		m.state = StateStopping
		return Command{Name: CommandStop}
	case StateStarting:
		// Released before the engine came up — a very short tap. Remember it so the
		// mic is closed the moment it opens.
		m.pendingStop = true
	}
	return none
}

// EngineStarted is the provider reporting that it is live.
func (m *Machine) EngineStarted() Command {
	if m.state != StateStarting {
		return none
	}
	if m.pendingStop {
		m.pendingStop = false
		m.state = StateStopping
		return Command{Name: CommandStop}
	}
	m.state = StateListening
	return none
}

// EngineStopped is the provider reporting that teardown finished.
//
// It resets from ANY non-idle state, not just stopping. An aborted startup or an
// unexpected death must never leave the machine wedged in a state where the next key
// press does nothing — that reads to the user as "the app stopped working".
func (m *Machine) EngineStopped() Command {
	if m.state != StateIdle {
		m.state = StateIdle
		m.pendingStop = false
	}
	return none
}

// Interrupt cancels an in-progress dictation: a real keypress happened while the trigger
// was held (fn+arrow, or typing), so what was captured is not something the user meant
// to dictate.
func (m *Machine) Interrupt() Command {
	if m.state == StateStarting || m.state == StateListening {
		m.state = StateStopping
		m.pendingStop = false
		return Command{Name: CommandStop, Discard: true}
	}
	return none
}
