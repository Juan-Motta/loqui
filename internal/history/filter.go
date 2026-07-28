// Search and date filtering for the Historial view. Pure, so it is unit-tested: the page owns only
// the painting, and what to show is decided here.
//
// Ported from the Electron build's src/shared/historyFilter.ts, keeping its semantics on purpose —
// the two apps read the same history.jsonl, and a user moving between them should not find that
// "Hoy" means something different.
package history

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

const dayMillis = 86_400_000

// FilterOptions is what the Historial controls add up to.
type FilterOptions struct {
	// Query is a case-insensitive substring of the transcript.
	Query string
	// Range is "all", "today", or a number of days ("7", "30").
	Range string
	// Now is the reference instant in Unix milliseconds, injected so the date rules are testable
	// without waiting for a clock. Zero means "the real now".
	Now int64
}

// RangeStart is the inclusive lower bound for a range, and whether there is one at all.
//
// AN UNRECOGNISED RANGE HAS NO BOUND, deliberately. A bug in the filter must never hide the whole
// list: someone staring at an empty Historial cannot tell "nothing matched" from "the app lost my
// transcripts", and the second reading is far worse when the file on disk is fine.
func RangeStart(rangeKey string, now int64) (start int64, bounded bool) {
	if rangeKey == "today" {
		// LOCAL MIDNIGHT, not now-24h. A dictation from this morning is today's; one from 23:00
		// yesterday is not, even though it falls inside twenty-four hours.
		t := time.UnixMilli(now).In(time.Local)
		midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
		return midnight.UnixMilli(), true
	}
	days, err := strconv.Atoi(rangeKey)
	if err != nil || days <= 0 {
		return 0, false
	}
	return now - int64(days)*dayMillis, true
}

// Filter returns the records to show, newest first. Never nil.
//
// It does not touch the caller's slice: the store hands out a slice it may reuse, so sorting in
// place would change what the next reader sees.
func Filter(items []Record, opts FilterOptions) []Record {
	now := opts.Now
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	query := strings.ToLower(strings.TrimSpace(opts.Query))
	start, bounded := RangeStart(opts.Range, now)

	out := make([]Record, 0, len(items))
	for _, rec := range items {
		if query != "" && !strings.Contains(strings.ToLower(rec.Text), query) {
			continue
		}
		// With a date filter on, an undated record cannot be SHOWN to be in range, so it is left
		// out. Treating it as "now" would float a record of unknown age to the top of "Hoy".
		if bounded && (rec.At == 0 || rec.At < start) {
			continue
		}
		out = append(out, rec)
	}
	// Sorted on the copy, newest first. STABLE, so records sharing a timestamp keep the order the
	// file gave them — appends within the same millisecond are otherwise shuffled on every repaint.
	slices.SortStableFunc(out, func(a, b Record) int {
		switch {
		case a.At > b.At:
			return -1
		case a.At < b.At:
			return 1
		default:
			return 0
		}
	})
	return out
}
