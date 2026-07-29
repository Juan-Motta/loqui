// The activation shortcut: what counts as one, how it reads, and which dictation modes it can
// actually drive.
//
// Ported from the Electron build's shared/triggerKey.ts. It belongs on this side for the port's usual
// reason, and for one specific to it: the accelerator is VALIDATED before being registered, and a
// shortcut that fails to register does so silently — the user presses their key and nothing happens,
// with nothing in the interface to explain it. Catching a bad accelerator at save time is the only
// place the failure is legible.
package store

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// FnTrigger is the Globe/fn key, addressed by name because it is not an accelerator at all — it
// arrives through its own listener.
const FnTrigger = "fn"

// modifiers are the accelerator modifiers accepted, normalised to one canonical spelling so two
// ways of writing the same combination compare equal.
var modifiers = map[string]string{
	"command":          "Command",
	"cmd":              "Command",
	"control":          "Control",
	"ctrl":             "Control",
	"commandorcontrol": "CommandOrControl",
	"cmdorctrl":        "CommandOrControl",
	"alt":              "Alt",
	"option":           "Alt",
	"altgr":            "AltGr",
	"shift":            "Shift",
	"super":            "Super",
	"meta":             "Super",
}

// modifierOrder is the canonical order, so the interface never shows "Shift+Command" one time and
// the reverse the next.
var modifierOrder = []string{"CommandOrControl", "Command", "Control", "Alt", "AltGr", "Shift", "Super"}

// namedKeys are the non-modifier keys accepted. Kept EXPLICIT so a typo fails at save time rather
// than silently failing to register later, which is the failure with no visible cause.
var namedKeys = []string{
	"Space", "Backspace", "Delete", "Insert", "Return", "Enter", "Tab", "Escape",
	"Up", "Down", "Left", "Right", "Home", "End", "PageUp", "PageDown",
	"Plus", "CapsLock", "NumLock", "ScrollLock", "PrintScreen",
}

var modifierSymbols = map[string]string{
	"Command":          "⌘",
	"CommandOrControl": "⌘",
	"Control":          "⌃",
	"Alt":              "⌥",
	"AltGr":            "⌥",
	"Shift":            "⇧",
	"Super":            "❖",
}

var (
	functionKeyRe = regexp.MustCompile(`^F(\d{1,2})$`)
	singleCharRe  = regexp.MustCompile(`^[a-zA-Z0-9]$`)
)

func isFunctionKey(k string) bool {
	m := functionKeyRe.FindStringSubmatch(k)
	if m == nil {
		return false
	}
	n, err := strconv.Atoi(m[1])
	return err == nil && n >= 1 && n <= 24
}

func canonicalKey(raw string) (string, bool) {
	k := strings.TrimSpace(raw)
	if k == "" {
		return "", false
	}
	if singleCharRe.MatchString(k) {
		return strings.ToUpper(k), true
	}
	for _, named := range namedKeys {
		if strings.EqualFold(named, k) {
			return named, true
		}
	}
	if up := strings.ToUpper(k); isFunctionKey(up) {
		return up, true
	}
	return "", false
}

// IsFnTrigger reports whether a stored value is the Globe key.
func IsFnTrigger(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), FnTrigger)
}

// DefaultTriggerKey is what a fresh install gets.
//
// NOTHING off macOS, deliberately: fn does not exist as an OS-level event there, and picking
// something like Ctrl+Shift+Space would collide with input-method switching on several systems. The
// interface asks the user to choose instead.
func DefaultTriggerKey(platform string) string {
	if platform == "darwin" {
		return FnTrigger
	}
	return ""
}

// SupportsHold reports whether a trigger can drive hold-to-talk.
//
// ONLY fn can, because only fn reports RELEASE. Any other accelerator gives a press and nothing
// else, so "hold" would start a dictation that never ends on its own.
func SupportsHold(trigger string) bool { return IsFnTrigger(trigger) }

// AllowedModes is the dictation modes a trigger can deliver.
func AllowedModes(trigger string) []string {
	if SupportsHold(trigger) {
		return []string{"hold", "toggle"}
	}
	return []string{"toggle"}
}

// CoerceMode downgrades a mode the chosen trigger cannot deliver.
//
// Silent downgrade is the right behaviour HERE — the alternative is refusing to save a shortcut
// because of an unrelated setting — but the interface must still disable the control rather than let
// the user pick something that gets changed underneath them.
func CoerceMode(trigger, mode string) string {
	if mode == "hold" && !SupportsHold(trigger) {
		return "toggle"
	}
	return mode
}

// ValidateTriggerKey returns the canonical accelerator, or "" for "no shortcut configured".
func ValidateTriggerKey(value, platform string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", nil
	}

	if strings.EqualFold(raw, FnTrigger) {
		if platform != "darwin" {
			return "", fmt.Errorf("la tecla fn solo existe como evento en macOS; elige otro atajo")
		}
		return FnTrigger, nil
	}

	var mods, keys []string
	for _, part := range strings.Split(raw, "+") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if mod, ok := modifiers[strings.ToLower(part)]; ok {
			if slices.Contains(mods, mod) {
				return "", fmt.Errorf("modificador repetido: %s", mod)
			}
			mods = append(mods, mod)
			continue
		}
		keys = append(keys, part)
	}

	if len(keys) == 0 {
		return "", fmt.Errorf("el atajo necesita una tecla además de los modificadores")
	}
	if len(keys) > 1 {
		return "", fmt.Errorf("el atajo admite una sola tecla (recibí %d)", len(keys))
	}

	key, ok := canonicalKey(keys[0])
	if !ok {
		return "", fmt.Errorf("tecla desconocida: %s", keys[0])
	}

	// A bare ordinary key registered globally would swallow that key in EVERY app. Function keys are
	// the exception: they exist to be pressed alone.
	if len(mods) == 0 && !isFunctionKey(key) {
		return "", fmt.Errorf("%s necesita al menos un modificador (por ejemplo ⌘ o ⌃)", key)
	}

	slices.SortFunc(mods, func(a, b string) int {
		return slices.Index(modifierOrder, a) - slices.Index(modifierOrder, b)
	})
	return strings.Join(append(mods, key), "+"), nil
}

// TriggerNote is the sentence under the shortcut control.
//
// It has to match the trigger the user ACTUALLY has: telling someone to hold a key that cannot
// report release, or to press one when no shortcut is configured, is worse than saying nothing.
func TriggerNote(trigger string) string {
	if SupportsHold(trigger) {
		return "fn admite mantener y alternar."
	}
	if strings.TrimSpace(trigger) == "" {
		return "Sin atajo configurado: usa el ícono de la barra de menús o “Probar dictado”."
	}
	return "Este atajo solo funciona en modo Alternar: el sistema no avisa cuándo lo sueltas."
}

// FormatTrigger is the short label the interface shows.
func FormatTrigger(trigger string) string {
	if strings.TrimSpace(trigger) == "" {
		return "Sin atajo"
	}
	if IsFnTrigger(trigger) {
		return "fn (Globe)"
	}
	parts := strings.Split(trigger, "+")
	if len(parts) == 0 {
		return trigger
	}
	key := parts[len(parts)-1]
	var out strings.Builder
	for _, mod := range parts[:len(parts)-1] {
		if sym, ok := modifierSymbols[mod]; ok {
			out.WriteString(sym)
			continue
		}
		out.WriteString(mod + "+")
	}
	out.WriteString(key)
	return out.String()
}
