// Ported from the Electron suites test/unit/injection.test.ts, focusGuard.test.ts and
// pasteQueue.test.ts.
package inject

import (
	"sync"
	"testing"
)

func TestShouldInject(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		secure bool
		want   bool
	}{
		{"ordinary text", "hola", false, true},
		{"blank", "   ", false, false},
		{"empty", "", false, false},
		{"secure field blocks even good text", "hola", true, false},
	}
	for _, c := range cases {
		if got := ShouldInject(c.text, c.secure); got != c.want {
			t.Errorf("%s: ShouldInject(%q, %v) = %v, want %v", c.name, c.text, c.secure, got, c.want)
		}
	}
}

func TestShouldRestoreClipboardOnlyWhenUntouched(t *testing.T) {
	if !ShouldRestoreClipboard(42, 42) {
		t.Error("same change count means nobody wrote to the clipboard: restore")
	}
	if ShouldRestoreClipboard(42, 43) {
		t.Error("a moved change count means someone else copied something: do NOT overwrite it")
	}
	// Counts only ever increase, but a lower value must not be read as "ours" either.
	if ShouldRestoreClipboard(42, 41) {
		t.Error("any difference means it is not ours")
	}
}

func TestShouldInjectIntoBlocksOnlyProvenMismatch(t *testing.T) {
	cases := []struct {
		name string
		in   GuardInput
		want bool
	}{
		{
			"same app",
			GuardInput{Text: "hola", SessionApp: "com.apple.Notes", CurrentApp: "com.apple.Notes"},
			true,
		},
		{
			"proven mismatch: focus drifted to another app",
			GuardInput{Text: "hola", SessionApp: "com.apple.Notes", CurrentApp: "com.apple.Safari"},
			false,
		},
		{
			// The common case for a user who never granted Accessibility. Refusing here
			// would make dictation silently produce nothing.
			"unknown current app cannot prove a mismatch",
			GuardInput{Text: "hola", SessionApp: "com.apple.Notes", CurrentApp: ""},
			true,
		},
		{
			"unknown session app cannot prove a mismatch",
			GuardInput{Text: "hola", SessionApp: "", CurrentApp: "com.apple.Safari"},
			true,
		},
		{
			"both unknown",
			GuardInput{Text: "hola"},
			true,
		},
		{
			"secure field wins over everything",
			GuardInput{Text: "hola", SecureField: true, SessionApp: "a", CurrentApp: "a"},
			false,
		},
		{
			"blank text",
			GuardInput{Text: "  ", SessionApp: "a", CurrentApp: "a"},
			false,
		},
	}
	for _, c := range cases {
		if got := ShouldInjectInto(c.in); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestShouldStoreFinal(t *testing.T) {
	if !ShouldStoreFinal(false) {
		t.Error("ordinary dictation must be stored")
	}
	if ShouldStoreFinal(true) {
		t.Error("password-field dictation must never be persisted")
	}
}

// An app mismatch stops the paste but not the record: the words are still what the user
// said, and dropping them because focus moved would lose real content.
func TestAppMismatchBlocksPasteButNotStorage(t *testing.T) {
	in := GuardInput{Text: "hola", SessionApp: "com.apple.Notes", CurrentApp: "com.apple.Safari"}
	if ShouldInjectInto(in) {
		t.Error("paste must be blocked")
	}
	if !ShouldStoreFinal(in.SecureField) {
		t.Error("storage must still happen")
	}
}

func TestQueueRunsTasksInOrderWithoutOverlap(t *testing.T) {
	q := NewQueue()
	var mu sync.Mutex
	var order []int
	var active, maxActive int

	var wg sync.WaitGroup
	for i := 1; i <= 5; i++ {
		i := i
		wg.Add(1)
		q.Go(func() {
			defer wg.Done()
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			order = append(order, i)
			mu.Unlock()

			mu.Lock()
			active--
			mu.Unlock()
		})
	}
	wg.Wait()

	if maxActive != 1 {
		t.Errorf("max concurrent tasks = %d, want 1 — overlapping pastes corrupt the clipboard", maxActive)
	}
	for i, got := range order {
		if got != i+1 {
			t.Errorf("order = %v, want 1..5 — finals must paste in the order they arrived", order)
			break
		}
	}
}

// A panicking paste must not stop every later one: the queue would wedge and dictation
// would appear to stop working with no error anywhere.
func TestQueueSurvivesAPanickingTask(t *testing.T) {
	q := NewQueue()
	done := make(chan struct{})

	q.Go(func() {
		defer func() { _ = recover() }()
		panic("boom")
	})
	q.Go(func() { close(done) })

	<-done // hangs the test (and fails on timeout) if the queue wedged
}

func TestQueueDoBlocksUntilItsTurn(t *testing.T) {
	q := NewQueue()
	var mu sync.Mutex
	var order []string

	release := make(chan struct{})
	q.Go(func() {
		<-release
		mu.Lock()
		order = append(order, "first")
		mu.Unlock()
	})

	doDone := make(chan struct{})
	go func() {
		q.Do(func() {
			mu.Lock()
			order = append(order, "second")
			mu.Unlock()
		})
		close(doDone)
	}()

	close(release)
	<-doDone

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Errorf("order = %v, want [first second]", order)
	}
}
