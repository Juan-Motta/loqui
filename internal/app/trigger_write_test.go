package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/store"
)

func TestSettingTheTriggerPersistsTheCanonicalForm(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	// Lower case and a non-canonical modifier order on the way in.
	res := svc.SetTrigger("shift+cmd+d")
	if res.Error != "" {
		t.Fatalf("SetTrigger: %s", res.Error)
	}
	if got := st.LoadSettings().TriggerKey; got != "Command+Shift+D" {
		t.Errorf("stored %q, want the canonical Command+Shift+D", got)
	}
	if got := res.Payload.Trigger.Label; got != "⌘⇧D" {
		t.Errorf("label = %q, want ⌘⇧D", got)
	}
}

// A shortcut that would never register is refused at save time, because a failed registration is
// SILENT: the user presses their key and nothing happens, with nothing in the interface to explain it.
func TestSettingAnInvalidTriggerIsRefused(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	before := st.LoadSettings().TriggerKey

	res := svc.SetTrigger("D") // a bare letter would swallow that key in every app
	if res.Error == "" {
		t.Fatal("a bare letter was accepted")
	}
	if got := st.LoadSettings().TriggerKey; got != before {
		t.Errorf("a rejected write changed the trigger to %q", got)
	}
}

// SWITCHING AWAY FROM fn MUST DOWNGRADE THE MODE. Only fn reports release, so leaving mode="hold"
// would start dictations that never end on their own.
func TestSwitchingAwayFromFnDowngradesHoldToToggle(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.TriggerKey = "fn"
		cfg.Mode = "hold"
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	svc, _ := testService(t, st)

	if res := svc.SetTrigger("Command+Shift+D"); res.Error != "" {
		t.Fatalf("SetTrigger: %s", res.Error)
	}

	got := st.LoadSettings()
	if got.Mode != "toggle" {
		t.Errorf("mode = %q, want toggle — this trigger cannot report release", got.Mode)
	}
	// And the payload says hold is not available, so the control disables it rather than offering a
	// choice that gets changed underneath the user.
	if res := svc.Load(); res.Trigger.SupportsHold {
		t.Error("the payload still claims hold is supported")
	}
}

// The RUNNING controller has to be told, not just the file: the engine reads the mode once, when it
// is constructed, so a persisted-only change takes effect at the next launch.
func TestChangingTheModeReachesTheRunningEngine(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.TriggerKey = "fn"
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	svc, _ := testService(t, st)

	var applied []string
	svc.onModeChanged = func(mode string) { applied = append(applied, mode) }

	if res := svc.SetMode("toggle"); res.Error != "" {
		t.Fatalf("SetMode: %s", res.Error)
	}
	if len(applied) != 1 || applied[0] != "toggle" {
		t.Errorf("the engine was told %v, want [toggle]", applied)
	}
	if got := st.LoadSettings().Mode; got != "toggle" {
		t.Errorf("stored mode = %q", got)
	}
}

// Asking for hold on a trigger that cannot deliver it saves toggle AND SAYS SO. Changing it silently
// would leave the user believing they have hold-to-talk.
func TestAskingForHoldOnAnIncapableTriggerIsReported(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.TriggerKey = "F13"
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	svc, _ := testService(t, st)

	res := svc.SetMode("hold")
	if res.Error == "" {
		t.Fatal("hold on an incapable trigger was reported as a plain success")
	}
	if !strings.Contains(res.Error, "Alternar") {
		t.Errorf("error = %q, want it to say what was stored instead", res.Error)
	}
	if got := st.LoadSettings().Mode; got != "toggle" {
		t.Errorf("stored mode = %q, want toggle", got)
	}
}

// The LISTENER has to be re-registered, not just the file. The fn listener is a child process started
// at launch from the stored trigger, so without this the new shortcut is saved while the old one keeps
// working — the interface and the keyboard disagreeing with nothing to suggest it.
func TestChangingTheTriggerReRegistersTheListener(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	var registered []string
	svc.onTriggerChanged = func(trigger string) error {
		registered = append(registered, trigger)
		return nil
	}

	if res := svc.SetTrigger("fn"); res.Error != "" {
		t.Fatalf("SetTrigger: %s", res.Error)
	}
	if len(registered) != 1 || registered[0] != "fn" {
		t.Errorf("the listener was told %v, want [fn]", registered)
	}
}

// A registration that FAILS must say the shortcut was saved but not registered. Reporting a plain
// failure would send the user to set it again, when the setting did take and only the listener did
// not start.
func TestAFailedRegistrationSaysTheShortcutWasStillSaved(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	svc.onTriggerChanged = func(string) error { return errors.New("no hay binario del listener") }

	res := svc.SetTrigger("fn")
	if res.Error == "" {
		t.Fatal("a failed registration was reported as success")
	}
	if !strings.Contains(res.Error, "se guardó") {
		t.Errorf("error = %q, want it to say the shortcut was saved", res.Error)
	}
	if got := st.LoadSettings().TriggerKey; got != "fn" {
		t.Errorf("stored trigger = %q — it should have been saved regardless", got)
	}
}

// The control's own state: the note has to match the trigger actually configured, and the reset
// button must be hidden when pressing it would do nothing.
func TestTheTriggerControlDescribesWhatIsConfigured(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	if res := svc.SetTrigger("fn"); res.Error != "" {
		t.Fatalf("SetTrigger(fn): %s", res.Error)
	}
	c := svc.Load().Trigger
	if !strings.Contains(c.Note, "mantener") {
		t.Errorf("fn note = %q, want it to mention hold", c.Note)
	}
	if c.ShowReset {
		t.Error("the reset button is offered while already on fn — it would be a no-op")
	}

	if res := svc.SetTrigger("Command+Shift+D"); res.Error != "" {
		t.Fatalf("SetTrigger(accel): %s", res.Error)
	}
	c = svc.Load().Trigger
	if !strings.Contains(c.Note, "Alternar") {
		t.Errorf("accelerator note = %q, want it to explain hold is unavailable", c.Note)
	}
	if !c.ShowReset {
		t.Error("the reset button is hidden while not on fn — restoring it is meaningful here")
	}

	if res := svc.SetTrigger(""); res.Error != "" {
		t.Fatalf("SetTrigger(none): %s", res.Error)
	}
	c = svc.Load().Trigger
	if c.Label != "Sin atajo" {
		t.Errorf("label = %q", c.Label)
	}
	if !strings.Contains(c.Note, "barra de menús") {
		t.Errorf("empty note = %q, want it to point at the tray", c.Note)
	}
}
