package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/history"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

func seedHistory(t *testing.T, st *store.Store, records ...history.Record) {
	t.Helper()
	for _, rec := range records {
		if err := st.AppendHistory(rec); err != nil {
			t.Fatalf("seeding history: %v", err)
		}
	}
}

func TestListingReturnsTheStoredTranscriptsNewestFirst(t *testing.T) {
	st := store.NewAt(t.TempDir())
	now := time.Now().UnixMilli()
	seedHistory(t, st,
		history.Record{Text: "la primera", At: now - 5000},
		history.Record{Text: "la última", At: now - 100},
	)
	svc := NewHistoryService(st)

	page := svc.List("", "all")

	if len(page.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(page.Entries))
	}
	if page.Entries[0].Text != "la última" {
		t.Errorf("first entry = %q, want the newest", page.Entries[0].Text)
	}
	if page.Total != 2 {
		t.Errorf("Total = %d, want 2", page.Total)
	}
}

// Total counts what is STORED, not what matched, so the page can tell "nothing saved yet" apart from
// "nothing matched your search". On screen both are an empty list; to the user they are completely
// different situations, and only one of them means their transcripts are gone.
func TestTotalCountsWhatIsStoredNotWhatMatched(t *testing.T) {
	st := store.NewAt(t.TempDir())
	now := time.Now().UnixMilli()
	seedHistory(t, st,
		history.Record{Text: "hola mundo", At: now - 100},
		history.Record{Text: "otra cosa", At: now - 200},
	)
	svc := NewHistoryService(st)

	page := svc.List("no existe en ningún sitio", "all")

	if len(page.Entries) != 0 {
		t.Errorf("got %d entries for a query that matches nothing", len(page.Entries))
	}
	if page.Total != 2 {
		t.Errorf("Total = %d, want 2 — the transcripts are still there", page.Total)
	}
}

// The filter runs in Go, so the page cannot disagree with it. This is the rule most likely to drift
// if it were duplicated: "today" is since local midnight, not the last 24 hours.
func TestListingAppliesTheDateFilterInGo(t *testing.T) {
	st := store.NewAt(t.TempDir())
	nowT := time.Now().In(time.Local)
	midnight := time.Date(nowT.Year(), nowT.Month(), nowT.Day(), 0, 0, 0, 0, time.Local)
	// An hour before local midnight: inside 24h on most runs, but never "today".
	seedHistory(t, st,
		history.Record{Text: "de hoy", At: midnight.Add(time.Minute).UnixMilli()},
		history.Record{Text: "de ayer", At: midnight.Add(-time.Hour).UnixMilli()},
	)
	svc := NewHistoryService(st)

	page := svc.List("", "today")

	if len(page.Entries) != 1 || page.Entries[0].Text != "de hoy" {
		t.Errorf("today's filter returned %v, want only the record after local midnight", page.Entries)
	}
}

// Recent is what the Home card shows, and it must be capped: the card has room for a handful.
func TestRecentIsCapped(t *testing.T) {
	st := store.NewAt(t.TempDir())
	now := time.Now().UnixMilli()
	for i := 0; i < homeRecentLimit+4; i++ {
		seedHistory(t, st, history.Record{Text: "una transcripción", At: now - int64(i)*1000})
	}
	svc := NewHistoryService(st)

	if got := len(svc.Recent().Entries); got != homeRecentLimit {
		t.Errorf("Recent returned %d entries, want the %d the card holds", got, homeRecentLimit)
	}
}

// Clearing returns the resulting page, so the list repaints from what is actually stored rather than
// from the page assuming the delete worked — the same reason the settings setters return a payload.
func TestClearingEmptiesTheListAndReportsIt(t *testing.T) {
	st := store.NewAt(t.TempDir())
	seedHistory(t, st, history.Record{Text: "bórrame", At: time.Now().UnixMilli()})
	svc := NewHistoryService(st)

	page := svc.Clear()

	if len(page.Entries) != 0 || page.Total != 0 {
		t.Errorf("after clearing: %d entries, total %d — want both zero", len(page.Entries), page.Total)
	}
	if got := len(svc.List("", "all").Entries); got != 0 {
		t.Errorf("a fresh read still returns %d entries", got)
	}
}

// Clearing an already-empty history is success, not an error: the caller wanted it gone.
func TestClearingAnEmptyHistoryIsFine(t *testing.T) {
	svc := NewHistoryService(store.NewAt(t.TempDir()))
	if page := svc.Clear(); len(page.Entries) != 0 {
		t.Errorf("got %d entries from an empty history", len(page.Entries))
	}
}

// Entries must never marshal to null, for the same reason the settings lists must not: the page
// would have to guard every one of them.
func TestAnEmptyPageMarshalsAsAListNotNull(t *testing.T) {
	svc := NewHistoryService(store.NewAt(t.TempDir()))

	raw, err := json.Marshal(svc.List("", "all"))
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	var wire struct {
		Entries []HistoryEntry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	if wire.Entries == nil {
		t.Errorf("entries marshalled as null: %s", raw)
	}
}
