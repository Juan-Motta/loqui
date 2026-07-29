// The display model for the Permisos tab: which grants exist here, what each one is for, and what the
// button next to it can actually do.
//
// Ported from the Electron build's shared/permissions.ts. It lives on this side for the port's usual
// reason and one specific to permissions: the THREE-WAY state matters. A grant macOS will not let the
// app read is "sin verificar", not "missing" — and the Electron build learned that the hard way,
// because defaulting to granted made a denied microphone look fine while every dictation died.
package app

import (
	"runtime"

	"github.com/Juan-Motta/loqui-go/internal/permissions"
)

// PermissionState is what a row can say about one grant.
type PermissionState string

const (
	PermGranted PermissionState = "granted"
	PermMissing PermissionState = "missing"
	// PermUnknown is a grant the app cannot read. Shown as unverified rather than guessed: the two
	// wrong guesses fail in opposite directions, and one of them hides a broken microphone.
	PermUnknown PermissionState = "unknown"
)

// PermissionAction is what the row's button does.
type PermissionAction string

const (
	// PermRequest shows the native prompt. Only the microphone and speech recognition have an API
	// that triggers one.
	PermRequest PermissionAction = "request"
	// PermOpen deep-links to the right pane of System Settings.
	PermOpen PermissionAction = "open"
)

// PermissionRow is one row in the Permisos tab.
type PermissionRow struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Desc     string           `json:"desc"`
	Required bool             `json:"required"`
	State    PermissionState  `json:"state"`
	Label    string           `json:"label"`
	Action   PermissionAction `json:"action"`
}

// permissionMeta is the static description of one grant.
type permissionMeta struct {
	id, name, desc string
	required       bool
	// canRequest marks the grants with an API that shows a system prompt.
	canRequest bool
	// macOnly marks grants that only exist on macOS.
	//
	// THREE OF THESE FOUR ARE macOS TCC CONCEPTS with no Windows counterpart: Windows needs no grant
	// to synthesise keystrokes, its global shortcut needs no listener permission, and Apple's speech
	// engine does not run there. Listing them elsewhere would show rows the user can neither verify
	// nor change.
	macOnly bool
}

var permissionMetas = []permissionMeta{
	{
		id:         "microphone",
		name:       "Micrófono",
		desc:       "Captura tu voz para transcribir el audio.",
		required:   true,
		canRequest: true,
	},
	{
		id:       "accessibility",
		name:     "Accesibilidad",
		desc:     "Permite insertar el texto transcrito en cualquier app.",
		required: true,
		macOnly:  true,
	},
	{
		id:       "inputMonitoring",
		name:     "Monitoreo de entrada",
		desc:     "Detecta la tecla de atajo (fn) de forma global.",
		required: false,
		macOnly:  true,
	},
	{
		id:         "speechRecognition",
		name:       "Reconocimiento de voz",
		desc:       "Solo para el motor on-device de Apple (macOS).",
		required:   false,
		canRequest: true,
		macOnly:    true,
	},
}

// permissionState maps one grant's reading to a row state.
func permissionState(id string, p PermissionsState, inputMonitoring PermissionState) PermissionState {
	switch id {
	case "microphone":
		return fromStatus(p.Microphone)
	case "speechRecognition":
		return fromStatus(p.SpeechRecognition)
	case "accessibility":
		// AXIsProcessTrusted answers yes or no; there is no unreadable case to report.
		if p.Accessibility {
			return PermGranted
		}
		return PermMissing
	case "inputMonitoring":
		return inputMonitoring
	default:
		return PermUnknown
	}
}

func fromStatus(s permissions.Status) PermissionState {
	switch s {
	case permissions.Granted:
		return PermGranted
	case permissions.Denied, permissions.NotDetermined:
		return PermMissing
	default:
		return PermUnknown
	}
}

// PermissionRows builds the Permisos list.
//
// inputMonitoring is passed in rather than read here because there is no API to read it: the grant
// governs whether the fn listener can see key events at all, so the only evidence is whether that
// listener is producing them. Reporting it as "missing" on no evidence would tell the user to grant
// something that may already be granted.
func permissionRows(p PermissionsState, inputMonitoring PermissionState) []PermissionRow {
	mac := runtime.GOOS == "darwin"
	out := make([]PermissionRow, 0, len(permissionMetas))
	for _, m := range permissionMetas {
		if m.macOnly && !mac {
			continue
		}
		state := permissionState(m.id, p, inputMonitoring)

		label := "Recomendado"
		switch {
		case state == PermGranted:
			label = "Concedido"
		case state == PermUnknown:
			label = "Sin verificar"
		case m.required:
			label = "Requerido"
		}

		// The native prompt is offered only where it exists AND is still needed. Promising a prompt
		// that never appears is worse than sending the user to System Settings in the first place.
		action := PermOpen
		if state != PermGranted && m.canRequest && mac {
			action = PermRequest
		}

		out = append(out, PermissionRow{
			ID:       m.id,
			Name:     m.name,
			Desc:     m.desc,
			Required: m.required,
			State:    state,
			Label:    label,
			Action:   action,
		})
	}
	return out
}

// AllRequiredGranted reports whether every REQUIRED grant is in place, and names the ones that are
// not.
//
// "Unknown" counts as not granted here: the app cannot promise dictation will work on a grant it
// could not read. The row itself still says "sin verificar" rather than "missing", which is where
// that distinction belongs.
func requiredMissing(rows []PermissionRow) []string {
	var out []string
	for _, r := range rows {
		if r.Required && r.State != PermGranted {
			out = append(out, r.Name)
		}
	}
	return out
}
