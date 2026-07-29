package store

import (
	"slices"
	"strings"
	"testing"
)

func TestIsFnTrigger(t *testing.T) {
	for _, v := range []string{"fn", "FN", " Fn "} {
		if !IsFnTrigger(v) {
			t.Errorf("IsFnTrigger(%q) = false", v)
		}
	}
	for _, v := range []string{"", "fn+A", "Command+F"} {
		if IsFnTrigger(v) {
			t.Errorf("IsFnTrigger(%q) = true", v)
		}
	}
}

// ONLY fn can drive hold, because only fn reports release. Any other accelerator would start a
// dictation that never ends on its own.
func TestOnlyFnSupportsHold(t *testing.T) {
	if !SupportsHold("fn") {
		t.Error("fn does not support hold")
	}
	for _, v := range []string{"", "CommandOrControl+Shift+D", "F13"} {
		if SupportsHold(v) {
			t.Errorf("%q claims to support hold", v)
		}
	}
	if got := AllowedModes("fn"); !slices.Equal(got, []string{"hold", "toggle"}) {
		t.Errorf("fn modes = %v", got)
	}
	if got := AllowedModes("F13"); !slices.Equal(got, []string{"toggle"}) {
		t.Errorf("F13 modes = %v", got)
	}
}

// A mode the trigger cannot deliver is downgraded rather than stored as a lie.
func TestCoerceMode(t *testing.T) {
	if got := CoerceMode("F13", "hold"); got != "toggle" {
		t.Errorf("hold on F13 = %q, want toggle", got)
	}
	if got := CoerceMode("fn", "hold"); got != "hold" {
		t.Errorf("hold on fn = %q, want hold", got)
	}
	if got := CoerceMode("F13", "toggle"); got != "toggle" {
		t.Errorf("toggle on F13 = %q", got)
	}
}

// fn is macOS-only: it is not an accelerator at all, it arrives through its own listener, and there
// is no such event on other systems.
func TestFnIsRefusedOffMacOS(t *testing.T) {
	if got, err := ValidateTriggerKey("fn", "darwin"); err != nil || got != "fn" {
		t.Fatalf("fn on darwin = %q, %v", got, err)
	}
	_, err := ValidateTriggerKey("fn", "windows")
	if err == nil {
		t.Fatal("fn was accepted off macOS")
	}
	if !strings.Contains(err.Error(), "macOS") {
		t.Errorf("error = %q, want it to say why", err)
	}
	if got := DefaultTriggerKey("darwin"); got != "fn" {
		t.Errorf("default on darwin = %q", got)
	}
	// Nothing off macOS: picking a combination for the user would collide with input-method
	// switching on several systems.
	if got := DefaultTriggerKey("linux"); got != "" {
		t.Errorf("default on linux = %q, want none", got)
	}
}

// Modifiers are normalised and ORDERED, so two spellings of the same combination compare equal and
// the interface never shows them in a different order on two renders.
func TestModifiersAreNormalisedAndOrdered(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cmd+shift+d", "Command+Shift+D"},
		{"Shift+Command+D", "Command+Shift+D"},
		{"cmdorctrl+alt+k", "CommandOrControl+Alt+K"},
		{"ctrl+space", "Control+Space"},
		{"CONTROL+ESCAPE", "Control+Escape"},
	}
	for _, c := range cases {
		got, err := ValidateTriggerKey(c.in, "darwin")
		if err != nil {
			t.Errorf("ValidateTriggerKey(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ValidateTriggerKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A bare ordinary key registered globally would swallow that key in EVERY app, so it is refused.
// Function keys are the exception: they exist to be pressed alone.
func TestABareKeyNeedsAModifierUnlessItIsAFunctionKey(t *testing.T) {
	_, err := ValidateTriggerKey("D", "darwin")
	if err == nil {
		t.Fatal("a bare letter was accepted as a global shortcut")
	}
	if !strings.Contains(err.Error(), "modificador") {
		t.Errorf("error = %q, want it to say a modifier is needed", err)
	}
	for _, fk := range []string{"F13", "f5", "F24"} {
		if _, err := ValidateTriggerKey(fk, "darwin"); err != nil {
			t.Errorf("%s alone was refused: %v", fk, err)
		}
	}
	// F25 is past the range Electron accepts.
	if _, err := ValidateTriggerKey("F25", "darwin"); err == nil {
		t.Error("F25 was accepted")
	}
}

func TestTriggerValidationRejectsMalformedInput(t *testing.T) {
	cases := []struct{ in, wants string }{
		{"Command+Shift", "necesita una tecla"},
		{"Command+A+B", "una sola tecla"},
		{"Command+Command+A", "repetido"},
		{"Command+Ñ", "desconocida"},
	}
	for _, c := range cases {
		_, err := ValidateTriggerKey(c.in, "darwin")
		if err == nil {
			t.Errorf("ValidateTriggerKey(%q) was accepted", c.in)
			continue
		}
		if !strings.Contains(err.Error(), c.wants) {
			t.Errorf("ValidateTriggerKey(%q) = %q, want it to mention %q", c.in, err, c.wants)
		}
	}
}

// Empty means "no shortcut configured", which is a valid state — not an error.
func TestAnEmptyTriggerMeansNoShortcut(t *testing.T) {
	for _, v := range []string{"", "   "} {
		got, err := ValidateTriggerKey(v, "darwin")
		if err != nil {
			t.Errorf("ValidateTriggerKey(%q): %v", v, err)
		}
		if got != "" {
			t.Errorf("ValidateTriggerKey(%q) = %q, want empty", v, got)
		}
	}
}

func TestFormatTrigger(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "Sin atajo"},
		{"fn", "fn (Globe)"},
		{"Command+Shift+D", "⌘⇧D"},
		{"CommandOrControl+Alt+K", "⌘⌥K"},
		{"Control+Space", "⌃Space"},
		{"F13", "F13"},
	}
	for _, c := range cases {
		if got := FormatTrigger(c.in); got != c.want {
			t.Errorf("FormatTrigger(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
