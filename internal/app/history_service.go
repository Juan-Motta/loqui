// The history service: what the Historial view and the Home activity card read from.
//
// FILTERING HAPPENS IN GO, not in the page. It is the same rule as the settings payload: the
// decisions live on this side, so the webview cannot disagree with them. Concretely, "Hoy" means
// since local midnight rather than the last twenty-four hours, and an unrecognised range filters
// nothing — rules that are tested in internal/history and would otherwise need a second, untested
// copy in TypeScript.
package app

import (
	"github.com/Juan-Motta/loqui-go/internal/history"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// homeRecentLimit is how many transcripts the Home activity card shows.
//
// Eight, matching the Electron original's `.slice(0, 8)`. Not a free choice: the card is sized for
// that many rows in the stylesheet this port inherited, so a smaller number leaves it looking
// short and a larger one overflows it.
const homeRecentLimit = 8

// historyReadLimit bounds what a single query reads off disk.
//
// The file is append-only and never pruned, so it grows for as long as the app is used. Without a
// bound, a heavy user's Historial would eventually spend seconds parsing JSONL to paint a list they
// scroll two screens of. 500 is well past what anyone reads in one sitting.
const historyReadLimit = 500

// HistoryEntry is one transcript as the page needs it.
type HistoryEntry struct {
	Text string `json:"text"`
	// Language is the detected locale when the provider reported one, which is worth showing: it is
	// the fastest way to see that auto-detection picked the wrong language for a bad transcript.
	Language string `json:"language,omitempty"`
	// At is a Unix millisecond timestamp. Formatting is left to the page, which is the only side
	// that knows the viewer's locale and timezone.
	At int64 `json:"at"`
}

// HistoryPage is what one query returns.
type HistoryPage struct {
	// Entries is never nil: a nil slice marshals to JSON null and every consumer would have to guard.
	Entries []HistoryEntry `json:"entries"`
	// Total is how many transcripts are stored in all, before the filter. The page needs it to tell
	// "you have nothing saved" apart from "nothing matched your search" — two states that look the
	// same on screen and mean completely different things.
	Total int `json:"total"`
}

// HistoryService is bound to the frontend as History.
type HistoryService struct {
	store *store.Store
}

func NewHistoryService(st *store.Store) *HistoryService {
	return &HistoryService{store: st}
}

// ServiceName is what Wails calls this in its logs.
func (s *HistoryService) ServiceName() string { return "History" }

// List returns the transcripts matching the Historial controls. Bound as History.List().
func (s *HistoryService) List(query string, rangeKey string) HistoryPage {
	all := s.store.ListHistory(historyReadLimit)
	filtered := history.Filter(all, history.FilterOptions{Query: query, Range: rangeKey})
	return HistoryPage{Entries: toEntries(filtered), Total: len(all)}
}

// Recent returns the newest few transcripts for the Home activity card, unfiltered.
func (s *HistoryService) Recent() HistoryPage {
	all := s.store.ListHistory(homeRecentLimit)
	return HistoryPage{Entries: toEntries(all), Total: len(all)}
}

// Clear deletes every stored transcript. Bound as History.Clear().
//
// Returns the now-empty page rather than nothing, so the list repaints from what is actually stored
// instead of the page assuming the delete worked — the same reason the settings setters return their
// payload.
func (s *HistoryService) Clear() HistoryPage {
	// A failure is reported through the empty result rather than an error: List is what the page
	// paints from either way, so a clear that did not happen shows up as records still being there.
	_ = s.store.ClearHistory()
	return s.List("", "all")
}

func toEntries(records []history.Record) []HistoryEntry {
	out := make([]HistoryEntry, 0, len(records))
	for _, rec := range records {
		out = append(out, HistoryEntry{Text: rec.Text, Language: rec.Language, At: rec.At})
	}
	return out
}
