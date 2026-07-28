// Ported from the Electron suite (test/unit/history.test.ts).
package history

import (
	"reflect"
	"testing"
)

func TestShouldStore(t *testing.T) {
	cases := map[string]bool{
		"hola":    true,
		"  hola ": true,
		"":        false,
		"   ":     false,
		"\n\t":    false,
	}
	for in, want := range cases {
		if got := ShouldStore(in); got != want {
			t.Errorf("ShouldStore(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestJoinTranscriptStitchesSegments(t *testing.T) {
	got := JoinTranscript([]string{"Hola.", "¿Cómo estás?", "Todo bien."})
	want := "Hola. ¿Cómo estás? Todo bien."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJoinTranscriptTrimsAndDropsBlanks(t *testing.T) {
	got := JoinTranscript([]string{"  uno  ", "", "   ", "dos"})
	want := "uno dos"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJoinTranscriptEmpty(t *testing.T) {
	if got := JoinTranscript(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := JoinTranscript([]string{"", "  "}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestMakeRecordTrimsText(t *testing.T) {
	got := MakeRecord("  hola  ", "es-CO", "hold", 1234)
	want := Record{Text: "hola", Language: "es-CO", Trigger: "hold", At: 1234}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSortNewestFirstDoesNotMutateTheInput(t *testing.T) {
	in := []Record{{Text: "a", At: 1}, {Text: "b", At: 3}, {Text: "c", At: 2}}
	got := SortNewestFirst(in)

	wantOrder := []string{"b", "c", "a"}
	for i, w := range wantOrder {
		if got[i].Text != w {
			t.Errorf("position %d = %q, want %q (order %v)", i, got[i].Text, w, texts(got))
		}
	}
	if in[0].Text != "a" || in[1].Text != "b" || in[2].Text != "c" {
		t.Errorf("input was mutated: %v", texts(in))
	}
}

func TestSortNewestFirstIsStableForEqualTimestamps(t *testing.T) {
	// Two finals delivered in the same millisecond must not swap order between reads;
	// the history list would appear to shuffle itself.
	in := []Record{{Text: "first", At: 5}, {Text: "second", At: 5}}
	got := SortNewestFirst(in)
	if !reflect.DeepEqual(texts(got), []string{"first", "second"}) {
		t.Errorf("got %v, want the original order preserved", texts(got))
	}
}

func texts(rs []Record) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Text)
	}
	return out
}
