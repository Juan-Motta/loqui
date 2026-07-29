// The clipboard service: copying a transcript out of the app.
//
// ITS OWN SERVICE, not a method on HistoryService. Copying is not a history operation — it just
// happens that the first caller is the Historial's copy button. About's diagnostics and the report
// view want the same thing, and hanging a general capability off whichever view needed it first is
// how a service ends up meaning nothing.
package app

import "github.com/Juan-Motta/loqui-go/internal/inject"

// ClipboardService is bound to the frontend as Clipboard.
type ClipboardService struct {
	// copy is inject.CopyText unless a test replaced it: the real one writes the developer's actual
	// clipboard, which a unit test must not do.
	copy func(string) error
}

func NewClipboardService() *ClipboardService {
	return &ClipboardService{copy: inject.CopyText}
}

// ServiceName is what Wails calls this in its logs.
func (s *ClipboardService) ServiceName() string { return "Clipboard" }

// Copy puts text on the clipboard. Bound as Clipboard.Copy().
//
// Returns a MESSAGE rather than a Go error, for the same reason the settings setters do: Wails
// discards a bound method's result when it also returns an error, and the button needs to say what
// went wrong on the button itself. Empty means it worked.
func (s *ClipboardService) Copy(text string) string {
	if err := s.copier()(text); err != nil {
		return err.Error()
	}
	return ""
}

func (s *ClipboardService) copier() func(string) error {
	if s.copy != nil {
		return s.copy
	}
	return inject.CopyText
}
