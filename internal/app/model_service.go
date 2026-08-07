// The model row's backend. Bound to the frontend as Model.
//
// A SERVICE OF ITS OWN rather than more methods on Settings, for one reason: a download takes minutes
// and Settings is the service the whole page serialises its writes through. A fifteen-minute call in
// that queue would block every other setting behind it.
package app

import (
	"github.com/Juan-Motta/loqui-go/internal/i18n"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// tr is this file's shorthand: the row's wording is decided in Go, so it is translated in Go.
func tr(locale string, key string, args map[string]string) string {
	return i18n.T(i18n.Locale(locale), key, args)
}

// ModelService owns the one downloader.
type ModelService struct {
	dl *modelDownloader
	// emit pushes progress to the window. A function, so this package does not import Wails — the same
	// rule the rest of internal/app follows.
	emit func(name string, data any)
	log  func(tag, msg string)
	// locale is a function, not a value: this service outlives any one language, and the row is
	// repainted after a language change.
	locale func() i18n.Locale
}

func NewModelService(dataDir string, emit func(string, any), log func(string, string),
	locale func() i18n.Locale) *ModelService {
	svc := &ModelService{emit: emit, log: log, locale: locale}
	svc.dl = newModelDownloader(dataDir, func() string { return loc(svc) })
	return svc
}

// ServiceName is what Wails calls this in its own logs.
func (s *ModelService) ServiceName() string { return "Model" }

// Status is what the row paints from. Bound as Model.Status().
func (s *ModelService) Status() ModelStatus { return s.described(s.dl.status()) }

// described fills in the wording, so every path that returns a status returns a complete one.
func (s *ModelService) described(st ModelStatus) ModelStatus {
	locale := ""
	if s.locale != nil {
		locale = string(s.locale())
	}
	st.StateText, st.StateClass = modelStateText(locale, st)
	return st
}

// ModelResult is the outcome of an action, with the fresh status so the row repaints from one answer.
type ModelResult struct {
	OK     bool        `json:"ok"`
	Error  string      `json:"error"`
	Status ModelStatus `json:"status"`
}

// Download fetches the model, resuming if there is a partial file. Bound as Model.Download().
//
// BLOCKS until it finishes, and that is deliberate: the page awaits it and shows a spinner, exactly
// as it does for a save. Progress arrives meanwhile as events, so the row is never a frozen bar.
func (s *ModelService) Download() ModelResult {
	if s.log != nil {
		s.log("MODEL", "download requested")
	}
	err := s.dl.download(func(p ModelProgress) {
		if s.emit != nil {
			s.emit("model:progress", p)
		}
	})
	if err != nil {
		if s.log != nil {
			// The reason, never the URL's response body: this line goes to the log.
			s.log("MODEL", "download failed: "+err.Error())
		}
		return ModelResult{Error: err.Error(), Status: s.Status()}
	}
	if s.log != nil {
		s.log("MODEL", "download complete and verified")
	}
	return ModelResult{OK: true, Status: s.Status()}
}

// Cancel stops a running download, KEEPING what has arrived. Bound as Model.Cancel().
//
// Keeping it is the point: the next Download resumes from there. A cancel that deleted the partial
// file would make "Cancel" the most expensive button in the app.
func (s *ModelService) Cancel() ModelResult {
	s.dl.cancel()
	if s.log != nil {
		s.log("MODEL", "download cancelled")
	}
	return ModelResult{OK: true, Status: s.Status()}
}

// Remove deletes the model. Bound as Model.Remove().
func (s *ModelService) Remove() ModelResult {
	// A bundled copy is not ours to delete: it shipped beside the helper, and this app cannot download
	// it back into that location — the next download would land in the data directory instead, leaving
	// the user with neither the file they had nor an explanation.
	if st := s.Status(); st.Bundled {
		return ModelResult{Error: tr(loc(s), "este modelo vino con la aplicación — no se puede eliminar desde aquí", nil), Status: st}
	}
	if err := s.dl.remove(); err != nil {
		return ModelResult{Error: err.Error(), Status: s.Status()}
	}
	if s.log != nil {
		s.log("MODEL", "model removed")
	}
	return ModelResult{OK: true, Status: s.Status()}
}

// modelStateText is the row's wording AND its class, decided here rather than in the page.
//
// The CLASS travels too, for the same reason the connection badge's does: it is what colours the line,
// and a page that worked it out from the sentence would be reimplementing this decision in another
// language. Ported from the original's modelStateText.
//
// Four problems and four different next actions, which is why they are not collapsed: missing needs a
// download, incomplete needs a RESUME — and saying "download" there would suggest starting over —
// corrupt needs a fresh one because there is nothing to resume from, and a bundled copy needs nothing.
func modelStateText(locale string, st ModelStatus) (string, string) {
	switch {
	// A bundled copy is only "ready" when it is also the RIGHT SIZE. Letting Bundled win outright
	// described a truncated development copy as ready and hid both Download and Delete, leaving the
	// row with no recovery path while Whisper failed. Review finding.
	case st.Bundled && st.Verdict.OK:
		return tr(locale, "Modelo local (repositorio)", nil), "ready"
	case st.Bundled:
		return tr(locale, "El modelo que vino con la app no es válido ({done} de {total})", map[string]string{
			"done":  store.FormatBytes(st.Bytes),
			"total": store.FormatBytes(st.Total),
		}), "warn"
	case st.Verdict.OK:
		return tr(locale, "Modelo listo", nil), "ready"
	case st.Verdict.Problem == store.ModelIncomplete:
		return tr(locale, "Descarga incompleta ({done} de {total}) — se reanudará", map[string]string{
			"done":  store.FormatBytes(st.Bytes),
			"total": store.FormatBytes(st.Total),
		}), "warn"
	case st.Verdict.Problem == store.ModelCorrupt:
		return tr(locale, "El modelo está dañado — hay que descargarlo de nuevo", nil), "warn"
	default:
		return tr(locale, "Falta el modelo ({size}, una sola vez)", map[string]string{
			"size": store.FormatBytes(st.Total),
		}), "warn"
	}
}

// loc is the service's current locale as a string, defaulting to the authored language.
func loc(s *ModelService) string {
	if s.locale == nil {
		return ""
	}
	return string(s.locale())
}
