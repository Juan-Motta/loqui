// Pure history helpers: what is worth storing, how a session's segments become one
// message, record shaping and ordering. Ported from the Electron build's
// src/shared/history.ts.
//
// Privacy: only non-empty finals are stored, so a no-match or a cancelled session
// produces no record at all. Dictation into a secure text field is suppressed further
// upstream, before anything here is reached (see internal/inject/focus.go).
package history

import (
	"sort"
	"strings"
)

// Record is one stored transcription.
type Record struct {
	Text string `json:"text"`
	// Language is the detected locale, when the provider reports one.
	Language string `json:"language,omitempty"`
	// Trigger is the mode that produced it ("hold" / "toggle"), which is useful when
	// someone reports that a particular way of dictating loses text.
	Trigger string `json:"trigger,omitempty"`
	// At is a Unix millisecond timestamp, matching the JSONL the Electron build wrote —
	// the port reads the same file.
	At int64 `json:"at"`
}

// ShouldStore reports whether a transcript is worth keeping. Blank is not.
func ShouldStore(text string) bool {
	return strings.TrimSpace(text) != ""
}

// JoinTranscript stitches a session's recognized segments into one message.
//
// This is the difference between a transcript and a mess. Providers emit one final per
// VAD pause — whisper especially, which segments aggressively — but a dictation session
// is a SINGLE message: what ends it is the user releasing the key, not a breath. Pasting
// each segment as it arrived would scatter fragments across the cursor position as the
// user is still talking.
func JoinTranscript(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, " ")
}

// MakeRecord shapes a record for storage.
func MakeRecord(text, language, trigger string, atMillis int64) Record {
	return Record{
		Text:     strings.TrimSpace(text),
		Language: language,
		Trigger:  trigger,
		At:       atMillis,
	}
}

// SortNewestFirst orders records for display without mutating the input.
func SortNewestFirst(records []Record) []Record {
	out := make([]Record, len(records))
	copy(out, records)
	sort.SliceStable(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}
