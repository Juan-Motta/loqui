// The overlay's display state. Ported from the Electron build's
// src/shared/overlayState.ts, which ran in the overlay renderer; here it runs in Go and
// the frontend receives the result (see frontend/src/overlay.ts).
package session

import "github.com/Juan-Motta/loqui-go/internal/stt"

// OverlayStatus is what the pill shows.
type OverlayStatus string

const (
	OverlayIdle         OverlayStatus = "idle"
	OverlayListening    OverlayStatus = "listening"
	OverlayReconnecting OverlayStatus = "reconnecting"
	OverlayError        OverlayStatus = "error"
)

// OverlayState is the whole payload the overlay window needs.
type OverlayState struct {
	Status OverlayStatus `json:"status"`
	Error  string        `json:"error,omitempty"`
}

// InitialOverlayState is the resting state.
func InitialOverlayState() OverlayState {
	return OverlayState{Status: OverlayIdle}
}

// ReduceOverlay folds an event into the display state.
//
// Partial and Final are deliberately ignored. The overlay is a PRESENCE indicator, not a
// transcript view: it says "we are capturing your voice" with level bars and nothing
// more. The words themselves are read where they are injected — at the cursor, in the app
// the user is actually working in — so mirroring them in a floating pill would only put
// the same text in two places and invite the user to look away from where they are typing.
func ReduceOverlay(state OverlayState, evt stt.Event) OverlayState {
	switch evt.Type {
	case stt.Started:
		return OverlayState{Status: OverlayListening}
	case EventReconnecting:
		return OverlayState{Status: OverlayReconnecting}
	case stt.Canceled:
		msg := evt.Error
		if msg == "" {
			msg = "cancelado"
		}
		return OverlayState{Status: OverlayError, Error: msg}
	case stt.Stopped:
		return OverlayState{Status: OverlayIdle}
	default:
		return state
	}
}

// EventReconnecting is emitted by the controller rather than by a provider: it is a
// statement about what Loqui is doing between attempts, which no provider can make.
const EventReconnecting stt.EventType = "reconnecting"
