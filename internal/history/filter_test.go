package history

import (
	"testing"
	"time"
)

// at builds a timestamp from a local wall-clock time, which is what the range rules are expressed
// in: "today" means since local midnight, not "the last 24 hours".
func at(t *testing.T, layout, value string) int64 {
	t.Helper()
	parsed, err := time.ParseInLocation(layout, value, time.Local)
	if err != nil {
		t.Fatalf("parsing %q: %v", value, err)
	}
	return parsed.UnixMilli()
}

func TestRangeStartToday(t *testing.T) {
	now := at(t, "2006-01-02 15:04", "2026-07-28 14:30")
	got, bounded := RangeStart("today", now)
	if !bounded {
		t.Fatal("today must have a lower bound")
	}
	// Local midnight of the SAME DAY, not now-24h. A dictation from this morning belongs to today;
	// one from 23:00 yesterday does not, even though it is within 24 hours.
	want := at(t, "2006-01-02 15:04", "2026-07-28 00:00")
	if got != want {
		t.Errorf("RangeStart(today) = %d, want local midnight %d", got, want)
	}
}

func TestRangeStartDays(t *testing.T) {
	now := at(t, "2006-01-02 15:04", "2026-07-28 14:30")
	got, bounded := RangeStart("7", now)
	if !bounded {
		t.Fatal("a day count must have a lower bound")
	}
	if want := now - 7*86_400_000; got != want {
		t.Errorf("RangeStart(7) = %d, want %d", got, want)
	}
}

// An unrecognised range must NOT filter anything.
//
// Ported deliberately from the Electron module, which says why: a bug in the filter should never
// hide the whole list. Someone looking at an empty Historial cannot tell "nothing matched" from
// "the app is broken", and the second is far worse when the transcripts are still on disk.
func TestAnUnknownRangeDoesNotFilter(t *testing.T) {
	now := at(t, "2006-01-02 15:04", "2026-07-28 14:30")
	for _, r := range []string{"all", "", "banana", "0", "-5"} {
		if _, bounded := RangeStart(r, now); bounded {
			t.Errorf("RangeStart(%q) applied a bound; an unrecognised range must not filter", r)
		}
	}
}

func TestFilterMatchesTextCaseInsensitively(t *testing.T) {
	now := at(t, "2006-01-02 15:04", "2026-07-28 14:30")
	items := []Record{
		{Text: "Hola Hola Hola.", At: now - 1000},
		{Text: "Esto es una prueba", At: now - 2000},
	}

	got := Filter(items, FilterOptions{Query: "HOLA", Now: now})
	if len(got) != 1 || got[0].Text != "Hola Hola Hola." {
		t.Errorf("Filter(HOLA) = %v, want the Hola record — the user types in whatever case they like", got)
	}

	// Whitespace around the query is the user's, not their intent.
	if got := Filter(items, FilterOptions{Query: "  prueba  ", Now: now}); len(got) != 1 {
		t.Errorf("a padded query matched %d records, want 1", len(got))
	}
}

func TestAnEmptyQueryKeepsEverything(t *testing.T) {
	now := at(t, "2006-01-02 15:04", "2026-07-28 14:30")
	items := []Record{{Text: "uno", At: now}, {Text: "dos", At: now - 1}}
	if got := Filter(items, FilterOptions{Now: now}); len(got) != 2 {
		t.Errorf("an empty query kept %d of 2 records", len(got))
	}
}

func TestFilterAppliesTheDateBound(t *testing.T) {
	now := at(t, "2006-01-02 15:04", "2026-07-28 14:30")
	items := []Record{
		{Text: "esta mañana", At: at(t, "2006-01-02 15:04", "2026-07-28 09:00")},
		{Text: "anoche", At: at(t, "2006-01-02 15:04", "2026-07-27 23:00")},
	}

	got := Filter(items, FilterOptions{Range: "today", Now: now})
	if len(got) != 1 || got[0].Text != "esta mañana" {
		t.Errorf("today's filter returned %v — 23:00 yesterday is inside 24h but is not today", got)
	}
}

// A record with no timestamp cannot be SHOWN to be in range, so a date filter must exclude it —
// while no date filter must still include it. Silently treating undated as "now" would float a
// record of unknown age to the top of "today".
func TestAnUndatedRecordIsExcludedOnlyByADateFilter(t *testing.T) {
	now := at(t, "2006-01-02 15:04", "2026-07-28 14:30")
	items := []Record{{Text: "sin fecha"}, {Text: "hoy", At: now - 1000}}

	if got := Filter(items, FilterOptions{Range: "today", Now: now}); len(got) != 1 {
		t.Errorf("a date filter kept %d records, want only the dated one", len(got))
	}
	if got := Filter(items, FilterOptions{Range: "all", Now: now}); len(got) != 2 {
		t.Errorf("with no date filter, kept %d of 2 — an undated record is still a record", len(got))
	}
}

func TestFilterReturnsNewestFirst(t *testing.T) {
	now := at(t, "2006-01-02 15:04", "2026-07-28 14:30")
	items := []Record{
		{Text: "vieja", At: now - 5000},
		{Text: "nueva", At: now - 100},
		{Text: "media", At: now - 2000},
	}

	got := Filter(items, FilterOptions{Now: now})
	want := []string{"nueva", "media", "vieja"}
	for i, w := range want {
		if got[i].Text != w {
			t.Errorf("position %d = %q, want %q", i, got[i].Text, w)
		}
	}
}

// Filtering must not reorder or otherwise disturb the caller's slice: the store hands out a slice it
// may reuse, and a filter that sorted in place would change what the next reader sees.
func TestFilterDoesNotMutateTheInput(t *testing.T) {
	now := at(t, "2006-01-02 15:04", "2026-07-28 14:30")
	items := []Record{
		{Text: "vieja", At: now - 5000},
		{Text: "nueva", At: now - 100},
	}

	Filter(items, FilterOptions{Now: now})

	if items[0].Text != "vieja" || items[1].Text != "nueva" {
		t.Errorf("the input was reordered: %v", items)
	}
}

// Never nil: a nil slice marshals to JSON null and every consumer in the webview would have to
// guard for it — the same rule the settings payload follows for its lists.
func TestFilterNeverReturnsNil(t *testing.T) {
	now := at(t, "2006-01-02 15:04", "2026-07-28 14:30")
	if got := Filter(nil, FilterOptions{Now: now}); got == nil {
		t.Error("Filter(nil) returned nil, want an empty slice")
	}
	if got := Filter([]Record{{Text: "x", At: now}}, FilterOptions{Query: "nada", Now: now}); got == nil {
		t.Error("a query matching nothing returned nil, want an empty slice")
	}
}
