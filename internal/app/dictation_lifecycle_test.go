package app

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/audio"
	"github.com/Juan-Motta/loqui-go/internal/store"
	"github.com/Juan-Motta/loqui-go/internal/stt"
)

type lifecycleProvider struct {
	mu           sync.Mutex
	wantsAudio   bool
	startEntered chan struct{}
	startRelease chan struct{}
	starts       int
	stops        int
	audioPushes  int
}

func (p *lifecycleProvider) Start(int, stt.Sink) error {
	p.mu.Lock()
	p.starts++
	entered, release := p.startEntered, p.startRelease
	p.mu.Unlock()
	if entered != nil {
		close(entered)
	}
	if release != nil {
		<-release
	}
	return nil
}

func (p *lifecycleProvider) PushAudio([]byte) {
	p.mu.Lock()
	p.audioPushes++
	p.mu.Unlock()
}

func (p *lifecycleProvider) Stop() {
	p.mu.Lock()
	p.stops++
	p.mu.Unlock()
}

func (p *lifecycleProvider) WantsAudio() bool { return p.wantsAudio }

func (p *lifecycleProvider) counts() (starts, stops int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.starts, p.stops
}

func (p *lifecycleProvider) pushCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.audioPushes
}

type fakeCapture struct {
	mu         sync.Mutex
	frames     chan audio.Frame
	closeCount int
}

type fakeTimer struct{}

func (*fakeTimer) Stop() bool { return true }

type manualAfter struct {
	mu  sync.Mutex
	fns []func()
}

func (a *manualAfter) scheduleFn(_ time.Duration, fn func()) timerStopper {
	a.mu.Lock()
	a.fns = append(a.fns, fn)
	a.mu.Unlock()
	return &fakeTimer{}
}

func (a *manualAfter) fire(index int) {
	a.mu.Lock()
	fn := a.fns[index]
	a.mu.Unlock()
	fn()
}

func newFakeCapture() *fakeCapture {
	return &fakeCapture{frames: make(chan audio.Frame)}
}

func (c *fakeCapture) Frames() <-chan audio.Frame { return c.frames }

func (c *fakeCapture) Close() {
	c.mu.Lock()
	c.closeCount++
	count := c.closeCount
	c.mu.Unlock()
	if count == 1 {
		close(c.frames)
	}
}

func (c *fakeCapture) closes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCount
}

func newLifecycleDictation(t *testing.T, factory func(int) (stt.Provider, error)) *Dictation {
	t.Helper()
	st := store.NewAt(t.TempDir())
	d := NewDictation(st, &silentUI{})
	d.providerFactory = factory
	return d
}

func newTimerDictation(t *testing.T) (*Dictation, *manualAfter) {
	t.Helper()
	st := store.NewAt(t.TempDir())
	d := NewDictation(st, &silentUI{})
	after := &manualAfter{}
	d.scheduleAfter = after.scheduleFn
	return d, after
}

func waitLifecycleDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle operation deadlocked")
	}
}

func TestStoppedGenerationCannotStartLate(t *testing.T) {
	built := 0
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		built++
		return &lifecycleProvider{}, nil
	})

	d.StopEngine(2)
	d.StartEngine(2)

	if built != 0 {
		t.Fatalf("providers built = %d, want 0 for a stopped generation", built)
	}
}

func TestProviderReturnedAfterStopIsClosedWithoutStarting(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	provider := &lifecycleProvider{}
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		close(entered)
		<-release
		return provider, nil
	})
	done := make(chan struct{})
	go func() {
		d.StartEngine(1)
		close(done)
	}()
	<-entered
	d.StopEngine(1)
	close(release)
	waitLifecycleDone(t, done)

	starts, stops := provider.counts()
	if starts != 0 || stops != 1 {
		t.Fatalf("provider starts=%d stops=%d, want 0 and 1", starts, stops)
	}
}

func TestStopDuringProviderStartOwnsTheProviderCleanup(t *testing.T) {
	provider := &lifecycleProvider{
		startEntered: make(chan struct{}),
		startRelease: make(chan struct{}),
	}
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		return provider, nil
	})
	done := make(chan struct{})
	go func() {
		d.StartEngine(1)
		close(done)
	}()
	<-provider.startEntered
	d.StopEngine(1)
	close(provider.startRelease)
	waitLifecycleDone(t, done)

	starts, stops := provider.counts()
	if starts != 1 || stops != 1 {
		t.Fatalf("provider starts=%d stops=%d, want 1 and 1", starts, stops)
	}
}

func TestStaleStopDoesNotCloseNewerProvider(t *testing.T) {
	provider := &lifecycleProvider{}
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		return provider, nil
	})
	d.StartEngine(2)
	d.StopEngine(1)
	_, stops := provider.counts()
	if stops != 0 {
		t.Fatalf("provider stops=%d, want 0 after stale stop", stops)
	}
	d.StopEngine(2)
	_, stops = provider.counts()
	if stops != 1 {
		t.Fatalf("provider stops=%d, want 1 after current stop", stops)
	}
}

func TestDuplicateStopClosesProviderOnce(t *testing.T) {
	provider := &lifecycleProvider{}
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		return provider, nil
	})
	d.StartEngine(1)
	d.StopEngine(1)
	d.StopEngine(1)
	_, stops := provider.counts()
	if stops != 1 {
		t.Fatalf("provider stops=%d, want 1", stops)
	}
}

func TestReplacementOpensOnlyAfterPreviousCaptureCloses(t *testing.T) {
	first := newFakeCapture()
	second := newFakeCapture()
	providers := []*lifecycleProvider{
		{wantsAudio: true},
		{wantsAudio: true},
	}
	built := 0
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		provider := providers[built]
		built++
		return provider, nil
	})
	opens := 0
	d.captureOpener = func(string, func(string)) (captureStream, error) {
		opens++
		if opens == 1 {
			return first, nil
		}
		if first.closes() != 1 {
			t.Fatalf("replacement opened before old capture closed; closes=%d", first.closes())
		}
		return second, nil
	}

	d.StartEngine(1)
	d.StopEngine(1)
	d.StartEngine(2)
	if opens != 2 || first.closes() != 1 || second.closes() != 0 {
		t.Fatalf("opens=%d first closes=%d second closes=%d, want 2, 1, 0",
			opens, first.closes(), second.closes())
	}
	d.StopEngine(2)
}

func TestCaptureReturnedAfterStopIsClosedWithoutPump(t *testing.T) {
	provider := &lifecycleProvider{wantsAudio: true}
	capture := newFakeCapture()
	entered := make(chan struct{})
	release := make(chan struct{})
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		return provider, nil
	})
	d.captureOpener = func(string, func(string)) (captureStream, error) {
		close(entered)
		<-release
		return capture, nil
	}
	done := make(chan struct{})
	go func() {
		d.StartEngine(1)
		close(done)
	}()
	<-entered
	d.StopEngine(1)
	close(release)
	waitLifecycleDone(t, done)
	_, providerStops := provider.counts()
	if capture.closes() != 1 || providerStops != 1 {
		t.Fatalf("capture closes=%d provider stops=%d, want 1 and 1",
			capture.closes(), providerStops)
	}
}

func TestDetachedRunDropsBufferedFrameBeforeProviderPush(t *testing.T) {
	provider := &lifecycleProvider{}
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		return provider, nil
	})
	d.StartEngine(1)
	d.mu.Lock()
	run := d.run
	d.mu.Unlock()
	d.StopEngine(1)

	if d.pushFrame(run, provider, audio.Frame{PCM: []byte{1}, Level: 0.5}) {
		t.Fatal("detached run accepted a buffered audio frame")
	}
	if got := provider.pushCount(); got != 0 {
		t.Fatalf("provider audio pushes=%d, want 0 after detach", got)
	}
}

func TestStaleStopDoesNotCloseNewerCapture(t *testing.T) {
	provider := &lifecycleProvider{wantsAudio: true}
	capture := newFakeCapture()
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		return provider, nil
	})
	d.captureOpener = func(string, func(string)) (captureStream, error) {
		return capture, nil
	}
	d.StartEngine(2)
	d.StopEngine(1)
	if capture.closes() != 0 {
		t.Fatalf("capture closes=%d, want 0 after stale stop", capture.closes())
	}
	d.StopEngine(2)
	if capture.closes() != 1 {
		t.Fatalf("capture closes=%d, want 1 after current stop", capture.closes())
	}
}

func TestDuplicateStopClosesCaptureOnce(t *testing.T) {
	provider := &lifecycleProvider{wantsAudio: true}
	capture := newFakeCapture()
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		return provider, nil
	})
	d.captureOpener = func(string, func(string)) (captureStream, error) {
		return capture, nil
	}
	d.StartEngine(1)
	d.StopEngine(1)
	d.StopEngine(1)
	if capture.closes() != 1 {
		t.Fatalf("capture closes=%d, want 1", capture.closes())
	}
}

func TestOldGenerationLevelCannotSeedNewPeak(t *testing.T) {
	ui := &silentUI{}
	st := store.NewAt(t.TempDir())
	d := NewDictation(st, ui)
	d.providerFactory = func(int) (stt.Provider, error) {
		return &lifecycleProvider{}, nil
	}
	d.StartEngine(2)
	d.noteLevel(1, 0.9)
	d.noteLevel(2, 0.2)
	d.StopEngine(2)

	joined := strings.Join(ui.lines, "\n")
	if !strings.Contains(joined, "MIC peak level this session: 0.20") ||
		strings.Contains(joined, "MIC peak level this session: 0.90") {
		t.Fatalf("logs = %q, want generation-2 peak 0.20 only", joined)
	}
}

func TestOldGenerationActivityCannotResetNewIdleClock(t *testing.T) {
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		return &lifecycleProvider{}, nil
	})
	d.StartEngine(1)
	d.mu.Lock()
	oldRun := d.run
	d.mu.Unlock()
	d.StopEngine(1)
	d.StartEngine(2)
	want := time.Unix(123, 0)
	d.mu.Lock()
	newRun := d.run
	newRun.lastActivity = want
	d.mu.Unlock()

	d.noteActivity(oldRun)

	d.mu.Lock()
	got := newRun.lastActivity
	d.mu.Unlock()
	d.StopEngine(2)
	if !got.Equal(want) {
		t.Fatalf("last activity=%v, want %v after stale generation update", got, want)
	}
}

func TestStoppedGenerationRejectsTimerEvenIfItFires(t *testing.T) {
	d, after := newTimerDictation(t)
	called := 0
	d.ScheduleReconnect(2, time.Second, func() { called++ })
	d.StopEngine(2)
	after.fire(0)
	if called != 0 {
		t.Fatalf("callback calls=%d, want 0 for stopped generation", called)
	}
}

func TestStaleStopDoesNotCancelNewerTimer(t *testing.T) {
	d, after := newTimerDictation(t)
	called := 0
	d.ScheduleReconnect(2, time.Second, func() { called++ })
	d.StopEngine(1)
	after.fire(0)
	if called != 1 {
		t.Fatalf("callback calls=%d, want 1 after stale stop", called)
	}
}

func TestSupersededTimerCannotFire(t *testing.T) {
	d, after := newTimerDictation(t)
	firstCalls, secondCalls := 0, 0
	d.ScheduleReconnect(2, time.Second, func() { firstCalls++ })
	d.ScheduleReconnect(2, 2*time.Second, func() { secondCalls++ })
	after.fire(0)
	after.fire(1)
	if firstCalls != 0 || secondCalls != 1 {
		t.Fatalf("first calls=%d second calls=%d, want 0 and 1", firstCalls, secondCalls)
	}
}

func TestStaleStopDoesNotCancelNewerIdleGuard(t *testing.T) {
	d := newLifecycleDictation(t, func(int) (stt.Provider, error) {
		return &lifecycleProvider{}, nil
	})
	d.StartEngine(2)

	d.mu.Lock()
	run := d.run
	idleStop := run.idleStop
	d.mu.Unlock()

	d.StopEngine(1)
	select {
	case <-idleStop:
		t.Fatal("stale stop canceled the newer generation's idle guard")
	default:
	}
	d.StopEngine(2)
}
