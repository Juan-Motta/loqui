// The single source of truth for dictation control. Ported from the Electron build's
// src/shared/sessionController.ts.
//
// It owns the Machine and the Tracker, turns control events (press / release / interrupt
// / helper failure) and provider events into side effects through an injected IO, and
// contains no I/O itself — which is what makes every branch here testable without a
// window, a microphone or a network.
//
// The distinction it exists to maintain is DESIRED versus ACTUAL. What the user wants and
// what the provider is doing diverge constantly: during startup, during teardown, during
// a reconnect, and whenever a late event arrives from a session that has already ended.
// Everything below is about keeping those two straight.
package session

import (
	"sync"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/history"
	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// maxReconnects caps retry attempts. Unbounded reconnection against a cloud provider is
// a billing loop that produces no transcript, and by this point the user has long since
// noticed nothing is working.
const maxReconnects = 6

// IO is the side-effect surface the controller drives. The app supplies timers, windows,
// providers and paste; the controller supplies the decisions.
type IO interface {
	// StartEngine begins recognition for gen.
	StartEngine(gen int)
	// StopEngine ends recognition for gen. The provider must eventually report Stopped.
	StopEngine(gen int)

	// ShowOverlay / HideOverlay drive the presence pill.
	ShowOverlay()
	HideOverlay()
	// Overlay pushes a reduced display state to the pill.
	Overlay(state OverlayState)

	// DeliverFinal hands over the session's complete message, exactly once per session.
	// The app decides where it goes: pasted at the cursor, stored in history, or
	// neither if focus turned out to be a password field.
	DeliverFinal(text, language string, trigger Mode)

	// ScheduleReconnect runs fn after d for gen. Only one is ever pending.
	ScheduleReconnect(gen int, d time.Duration, fn func())
	// ReconnectExhausted reports that a retryable failure consumed the session's full budget.
	ReconnectExhausted(attempts int)
}

// Controller is the dictation controller. Safe for concurrent use: provider callbacks
// arrive on the SDK's threads while key events arrive on the helper's reader goroutine.
//
// THE MUTEX IS THE ONE STRUCTURAL ADDITION THE PORT REQUIRED, and it brought a hazard the
// Electron original could not have. JavaScript is single-threaded and that version held no
// lock, so it could call io.startEngine() from inside a locked region and let a provider
// synchronously report failure straight back into engineEvent(). Do that here and the
// second call deadlocks on the mutex the first one holds — which is exactly what happened:
// Press -> doStart -> StartEngine -> provider fails -> ProviderEvent -> deadlock, with the
// microphone never opening and no error anywhere.
//
// THE RULE THAT PREVENTS IT: decisions are made under the lock, EFFECTS RUN AFTER IT IS
// RELEASED. Nothing in this file may call into io while holding mu; the *Locked methods
// queue effects instead, and the public entry points flush them on the way out.
type Controller struct {
	io IO

	mu      sync.Mutex
	machine *Machine
	tracker *Tracker

	reconnectAttempt int

	// parts are the finals recognized in the CURRENT session. A provider emits one per
	// VAD pause, but the session is one message: they are buffered and delivered once,
	// when the user's action ends it.
	parts    []string
	language string

	overlay OverlayState

	// effects are io calls queued while the lock is held, flushed once it is released.
	// See the note above the type — this is what keeps a synchronous provider callback
	// from deadlocking against the press that started it.
	effects []func()
}

func NewController(mode Mode, io IO) *Controller {
	return &Controller{
		io:      io,
		machine: NewMachine(mode),
		tracker: NewTracker(),
		overlay: InitialOverlayState(),
	}
}

// Mode reports the current trigger mode.
func (c *Controller) Mode() Mode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.machine.Mode()
}

// Desired reports whether the user wants dictation running.
func (c *Controller) Desired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tracker.Desired()
}

// Generation is the current session's generation. Providers must tag their events with
// it, and after a reconnect it is the only way to know which one to answer.
func (c *Controller) Generation() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tracker.Generation()
}

// SetMode changes the trigger mode, but only while idle. Switching hold to toggle
// mid-session would leave the machine expecting an event that will never come — for hold
// a key-up that already happened, for toggle a second tap the user has no reason to make.
func (c *Controller) SetMode(mode Mode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tracker.Desired() {
		return
	}
	c.machine = NewMachine(mode)
}

// ---- control-plane inputs ---------------------------------------------------

// Press is a trigger key press.
func (c *Controller) Press() {
	c.mu.Lock()
	c.applyLocked(c.machine.KeyDown())
	effects := c.takeEffectsLocked()
	c.mu.Unlock()
	c.flushEffects(effects)
}

// Release is a trigger key release.
func (c *Controller) Release() {
	c.mu.Lock()
	c.applyLocked(c.machine.KeyUp())
	effects := c.takeEffectsLocked()
	c.mu.Unlock()
	c.flushEffects(effects)
}

// Interrupt is a real external keypress during a held dictation: DROP what was
// recognized. The user pressed fn+arrow or started typing; those sounds were not meant
// to become text.
func (c *Controller) Interrupt() {
	c.mu.Lock()
	c.parts, c.language = nil, ""
	c.applyLocked(c.machine.Interrupt())
	effects := c.takeEffectsLocked()
	c.mu.Unlock()
	c.flushEffects(effects)
}

// RequestStop is a normal user-requested stop (the tray item, the Home button). No
// discard, so a trailing final still drains into the message.
func (c *Controller) RequestStop() {
	c.mu.Lock()
	if !c.tracker.Desired() {
		c.mu.Unlock()
		return
	}
	c.doStopLocked()
	c.machine.EngineStopped()
	effects := c.takeEffectsLocked()
	c.mu.Unlock()
	c.flushEffects(effects)
}

// HelperFailed is the hotkey listener dying mid-session. FAIL CLOSED: stop the engine, so
// a broken listener can never leave the microphone open with no way to close it. What was
// already recognized is still the user's text, so it is delivered rather than dropped.
func (c *Controller) HelperFailed() {
	c.mu.Lock()
	c.applyLocked(c.machine.Interrupt())
	c.doStopLocked()
	c.flushLocked()
	effects := c.takeEffectsLocked()
	c.mu.Unlock()
	c.flushEffects(effects)
}

// StopByGuard is the idle watchdog forcing a stop. An inactivity stop, not a cancel: the
// text recognized before the silence is delivered, because the user did say it.
func (c *Controller) StopByGuard(gen int) {
	c.mu.Lock()
	if !c.tracker.Desired() || c.tracker.Generation() != gen {
		c.mu.Unlock()
		return
	}
	c.applyLocked(c.machine.Interrupt())
	c.doStopLocked()
	c.flushLocked()
	effects := c.takeEffectsLocked()
	c.mu.Unlock()
	c.flushEffects(effects)
}

// queue defers an io call until the lock is released. MUST be used for every io call
// made from a *Locked method — see the deadlock note on the type.
func (c *Controller) queue(fn func()) {
	c.effects = append(c.effects, fn)
}

// flushEffects runs the queued io calls. Called with the lock NOT held.
func (c *Controller) flushEffects(effects []func()) {
	for _, fn := range effects {
		fn()
	}
}

// takeEffectsLocked hands over the queue and clears it.
func (c *Controller) takeEffectsLocked() []func() {
	effects := c.effects
	c.effects = nil
	return effects
}

func (c *Controller) applyLocked(cmd Command) {
	switch cmd.Name {
	case CommandStart:
		c.doStartLocked()
	case CommandStop:
		if cmd.Discard {
			c.tracker.Discard(c.tracker.Generation())
		}
		c.doStopLocked()
	}
}

func (c *Controller) doStartLocked() {
	if c.tracker.Desired() {
		return
	}
	c.parts, c.language = nil, "" // a new session starts from an empty message
	gen := c.tracker.Start()
	c.reconnectAttempt = 0
	c.queue(func() { c.io.StartEngine(gen) })
	c.queue(c.io.ShowOverlay)
	c.setOverlayLocked(OverlayState{Status: OverlayListening})
}

func (c *Controller) doStopLocked() {
	if !c.tracker.Desired() {
		return
	}
	gen := c.tracker.Generation()
	c.tracker.Stop()
	c.queue(func() { c.io.StopEngine(gen) })
	c.queue(c.io.HideOverlay)
	c.setOverlayLocked(InitialOverlayState())
}

// ---- provider events (generation-gated) -------------------------------------

// ProviderEvent feeds one event from a provider. Safe to call from any goroutine.
func (c *Controller) ProviderEvent(evt stt.Event) {
	c.mu.Lock()
	c.providerEventLocked(evt)
	effects := c.takeEffectsLocked()
	c.mu.Unlock()
	c.flushEffects(effects)
}

func (c *Controller) providerEventLocked(evt stt.Event) {
	current := evt.Gen == c.tracker.Generation()

	switch evt.Type {
	case stt.Started:
		if !current {
			return // stale
		}
		c.applyLocked(c.machine.EngineStarted()) // honour a stop that arrived first
		if !c.tracker.Desired() {
			return // a pending or terminal stop must keep its final overlay state
		}
		c.setOverlayLocked(ReduceOverlay(c.overlay, evt))

	case stt.Partial:
		if c.tracker.Accepts(evt.Gen) {
			c.setOverlayLocked(ReduceOverlay(c.overlay, evt))
		}

	case stt.Final:
		if !c.tracker.Accepts(evt.Gen) {
			return // stale or discarded
		}
		// Buffered, not delivered: the VAD pause that produced this final is not the
		// end of the message. See history.JoinTranscript.
		if history.ShouldStore(evt.Text) {
			c.parts = append(c.parts, evt.Text)
			if evt.Language != "" {
				c.language = evt.Language
			}
		}

	case stt.Stopped:
		if !current {
			return
		}
		c.machine.EngineStopped()
		c.setOverlayLocked(ReduceOverlay(c.overlay, evt))
		// The provider confirmed teardown, INCLUDING any final it flushed on the way
		// out. That is what makes this the moment the message is complete — and why
		// the app must wait for it instead of delivering when it asked to stop.
		c.flushLocked()

	case stt.Canceled:
		if !current {
			return
		}
		c.handleCancelLocked(evt)
	}
}

// flushLocked delivers the session's buffered segments as ONE message.
//
// Idempotent by construction: it clears the buffer, so a late Stopped arriving after
// HelperFailed already flushed delivers nothing instead of pasting the text twice.
func (c *Controller) flushLocked() {
	text := history.JoinTranscript(c.parts)
	language := c.language
	c.parts, c.language = nil, ""
	if !history.ShouldStore(text) {
		return
	}
	mode := c.machine.Mode()
	c.queue(func() { c.io.DeliverFinal(text, language, mode) })
}

func (c *Controller) handleCancelLocked(evt stt.Event) {
	if !c.tracker.Desired() {
		return
	}
	class := ClassifyCancel(Cancel{ErrorCode: evt.ErrorCode, Error: evt.Error})

	retryable := ShouldReconnect(class)
	if !retryable || c.reconnectAttempt >= maxReconnects {
		if retryable {
			attempts := c.reconnectAttempt
			c.queue(func() { c.io.ReconnectExhausted(attempts) })
		}
		c.doStopLocked()
		c.machine.EngineStopped()
		c.setOverlayLocked(ReduceOverlay(c.overlay, evt)) // show WHY it stopped
		c.flushLocked()                                   // don't lose what was already said
		return
	}

	c.reconnectAttempt++
	// A new generation, so the failed recognizer's late events (its own stopped, a
	// trailing final) are stale and cannot be mistaken for the retry's.
	failedGen := c.tracker.Generation()
	gen := c.tracker.Bump()
	c.setOverlayLocked(OverlayState{Status: OverlayReconnecting})
	delay := Backoff(c.reconnectAttempt-1, BackoffOptions{Base: time.Second, Max: 30 * time.Second})
	c.queue(func() { c.io.StopEngine(failedGen) })
	c.queue(func() {
		c.io.ScheduleReconnect(gen, delay, func() {
			c.mu.Lock()
			desired := c.tracker.Desired()
			c.mu.Unlock()
			// A stop can still land between this read and StartEngine. Holding mu across IO
			// would deadlock on a synchronous provider callback. Dictation's generation
			// tombstone is the final barrier: StartEngine rejects gen after a concurrent stop.
			if desired {
				c.io.StartEngine(gen)
			}
		})
	})
}

// setOverlayLocked pushes a display state only when it actually changed, so a stream of
// partials does not repaint the pill hundreds of times.
func (c *Controller) setOverlayLocked(next OverlayState) {
	if next == c.overlay {
		return
	}
	c.overlay = next
	c.queue(func() { c.io.Overlay(next) })
}
