# Reconnect Capture Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every superseded provider/capture/pump before reconnecting and make a user stop reject any reconnect start or resource that arrives late.

**Architecture:** `session.Controller` keeps retry policy and orders teardown before backoff. `app.Dictation` owns a single generation-scoped `engineRun`; a monotonic stopped-through boundary and generation-tagged timers prevent stale or late work from publishing resources. Provider, microphone, time, and network behavior stay behind narrow existing or app-owned seams so concurrency tests remain deterministic.

**Tech Stack:** Go 1.24, Wails v3 application layer, `internal/session`, `internal/app`, `internal/audio`, standard-library mutexes/channels/timers, Go test and race detector.

## Global Constraints

- Do not record or buffer audio during reconnect backoff; close the microphone for the entire wait.
- Preserve the cumulative six-retry budget and delays of 1, 2, 4, 8, 16, and 30 seconds.
- Preserve transcript accumulation, generation gating, overlays, terminal classification, and provider-facing error text.
- Do not make OpenAI or ElevenLabs runtime errors retryable.
- Do not hold `session.Controller.mu` or `Dictation.mu` across provider, capture, UI, or controller callbacks; synchronous provider callbacks must remain deadlock-free.
- Do not change the public `stt.Provider` contract.
- Codex executes inline per `shared/rules/execution.md`; do not dispatch implementation subagents.
- The repository ship gate takes precedence over per-task commit defaults: record each task's red/green evidence in `.workflow/state.md`, but make the implementation commit only after every required gate is green.

## File map

- Modify `internal/session/controller.go`: stop the failed generation before scheduling a retry and pass the replacement generation to the timer owner.
- Modify `internal/session/controller_test.go`: prove teardown order and retain all reconnect-policy coverage with the new stop semantics.
- Create `internal/app/dictation_lifecycle.go`: define generation-owned runs, capture/timer seams, attachment barriers, and idempotent cleanup.
- Create `internal/app/dictation_lifecycle_test.go`: deterministic provider, capture, and timer interleaving regressions.
- Modify `internal/app/dictation.go`: route start, stop, capture, metering, timers, provider construction, and shutdown through the lifecycle owner.
- Modify `internal/app/provider_test.go`: pass a generation into provider construction after level callbacks become generation-scoped.
- Modify `internal/stt/grok/provider_failures_test.go`: update the local `session.IO` adapter for the generation-tagged reconnect signature.
- Create `docs/solutions/reconnect-capture-lifecycle.md`: durable symptom, cause, fix, tradeoffs, and verification record.
- Modify `docs/CHANGELOG.md`: add the user-visible reconnect lifecycle fix under Unreleased.
- Modify `CONTINUITY.md`: replace the completed lifecycle item with the next actionable project step.
- Update `.workflow/state.md`: phase, red/green evidence, review iterations, E2E disposition, and final gate evidence.

---

### Task 1: Stop the failed controller generation before backoff

**Files:**
- Modify: `internal/session/controller.go:337-376`
- Test: `internal/session/controller_test.go:15-50,226-506`

**Interfaces:**
- Consumes: existing `IO.StopEngine(gen int)` and `IO.ScheduleReconnect(time.Duration, func())`.
- Produces: retry side-effect order `StopEngine(failedGen)` then `ScheduleReconnect`; no interface signature change yet.

- [ ] **Step 1: Add observable effect order to the controller fake**

Add `effects []string` to `fakeIO`, import `fmt`, and make only the two relevant methods append order markers while preserving their existing recordings:

```go
type fakeIO struct {
	starts     []int
	stops      []int
	shows      int
	hides      int
	overlays   []OverlayState
	delivered  []delivery
	reconnects []time.Duration
	pending    []func()
	exhausted  []int
	effects    []string
}

func (f *fakeIO) StopEngine(gen int) {
	f.stops = append(f.stops, gen)
	f.effects = append(f.effects, fmt.Sprintf("stop:%d", gen))
}

func (f *fakeIO) ScheduleReconnect(d time.Duration, fn func()) {
	f.reconnects = append(f.reconnects, d)
	f.pending = append(f.pending, fn)
	f.effects = append(f.effects, "schedule")
}
```

- [ ] **Step 2: Write the failing controller regression**

Add this test at the start of the reconnect section:

```go
func TestRetryStopsFailedEngineBeforeSchedulingReplacement(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	io.effects = nil

	c.ProviderEvent(stt.Event{
		Type: stt.Canceled, Gen: 1,
		ErrorCode: "ConnectionFailure",
	})

	want := []string{"stop:1", "schedule"}
	if !reflect.DeepEqual(io.effects, want) {
		t.Fatalf("retry effects = %v, want %v", io.effects, want)
	}
	if !c.Desired() || io.hides != 0 || len(io.delivered) != 0 {
		t.Fatalf("retry ended the dictation: desired=%t hides=%d deliveries=%v",
			c.Desired(), io.hides, io.delivered)
	}
}
```

The production change that must make this test fail is removal or reordering of the retry-path `StopEngine` effect.

- [ ] **Step 3: Run the focused test and confirm RED**

Run:

```bash
./scripts/go.sh test ./internal/session -run TestRetryStopsFailedEngineBeforeSchedulingReplacement -count=1
```

Expected: FAIL with `retry effects = [schedule], want [stop:1 schedule]`.

- [ ] **Step 4: Implement the minimal controller ordering**

In `handleCancelLocked`, capture the failed generation before bumping and queue its stop before the existing schedule effect:

```go
c.reconnectAttempt++
failedGen := c.tracker.Generation()
gen := c.tracker.Bump()
c.setOverlayLocked(OverlayState{Status: OverlayReconnecting})
delay := Backoff(c.reconnectAttempt-1, BackoffOptions{Base: time.Second, Max: 30 * time.Second})
c.queue(func() { c.io.StopEngine(failedGen) })
c.queue(func() {
	c.io.ScheduleReconnect(delay, func() {
		c.mu.Lock()
		desired := c.tracker.Desired()
		c.mu.Unlock()
		if desired {
			c.io.StartEngine(gen)
		}
	})
})
```

Keep the current explanation that controller IO runs outside its mutex; replace the obsolete residual-TOCTOU comment only after the app barrier exists in Task 4.

- [ ] **Step 5: Update existing reconnect assertions to the new contract**

Use literal expectations rather than relaxing assertions:

| Test | Expected retry-path stops |
| --- | --- |
| `TestReconnectUsesANewGeneration` | `[]int{1}` after the first cancel |
| `TestReconnectAttemptsAreCapped` | `[]int{1, 2, 3, 4, 5, 6, 7}` |
| `TestReconnectBudgetSurvivesShortLivedSuccessfulConnections` | `[]int{1, 2, 3, 4, 5, 6, 7}` |
| `TestNewDictationGetsAFreshReconnectBudget` | seven additional stops in the second dictation |
| `TestStaleCancelDoesNotConsumeReconnectBudget` | one stop after current+duplicate-stale cancel; seven after exhaustion |
| `TestBackoffGrows` | `[]int{1, 2, 3}` |
| `TestNetworkCancelSchedulesAReconnect` | one reconnect and `[]int{1}` |
| `TestTransientErrorCodeReconnects` | one reconnect and `[]int{1}` |

Do not change terminal auth/config expectations: those still stop exactly once and never schedule.

- [ ] **Step 6: Run the controller package and confirm GREEN**

Run:

```bash
./scripts/go.sh test ./internal/session -count=1
```

Expected: PASS, including transcript continuity, exact delays, exhaustion, stale events, and synchronous reentrancy.

- [ ] **Step 7: Record the task checkpoint without committing**

Set `.workflow/state.md` phase to `tdd` and record the exact red message and green command. Leave production and tests uncommitted until the final ship gate.

---

### Task 2: Reject stopped generations and own providers by run

**Files:**
- Create: `internal/app/dictation_lifecycle.go`
- Create: `internal/app/dictation_lifecycle_test.go`
- Modify: `internal/app/dictation.go:53-179,599-613`

**Interfaces:**
- Consumes: `stt.Provider`, monotonically increasing controller generations, existing `Dictation.buildProvider()`.
- Produces: `engineRun`, `beginEngineRun(gen int)`, `attachProvider(*engineRun, stt.Provider) bool`, `isCurrentRun(*engineRun) bool`, `detachRunThrough(gen int) *engineRun`, and test seam `providerFactory func(int) (stt.Provider, error)`.

- [ ] **Step 1: Add deterministic lifecycle test doubles**

Create `internal/app/dictation_lifecycle_test.go` with a fake provider whose start can be barrier-controlled:

```go
package app

import (
	"sync"
	"testing"
	"time"

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

func (p *lifecycleProvider) PushAudio([]byte) {}

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

func newLifecycleDictation(t *testing.T, factory func(int) (stt.Provider, error)) *Dictation {
	t.Helper()
	st := store.NewAt(t.TempDir())
	d := NewDictation(st, &silentUI{})
	d.providerFactory = factory
	return d
}
```

- [ ] **Step 2: Write the complete provider-lifecycle RED suite**

```go
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
```

Before running or writing production code, also add the four barrier-controlled test bodies in the
`Provider-lifecycle RED test bodies required by Step 2` section below. That section is placed beside
the behavior it documents, but it is part of this RED step: all five tests must exist first.

Run:

```bash
./scripts/go.sh test ./internal/app -run 'Test(StoppedGeneration|ProviderReturned|StopDuringProvider|StaleStop|DuplicateStop)' -count=1
```

Expected: FAIL to compile because `providerFactory` does not exist. This is the wished-for app boundary; adding only a test helper instead would not protect production.

- [ ] **Step 3: Add the provider ownership primitives**

Create `internal/app/dictation_lifecycle.go` with:

```go
package app

import "github.com/Juan-Motta/loqui-go/internal/stt"

type engineRun struct {
	gen      int
	provider stt.Provider
}

func (d *Dictation) beginEngineRun(gen int) (*engineRun, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.shuttingDown || gen <= d.stoppedThrough || d.run != nil {
		return nil, false
	}
	run := &engineRun{gen: gen}
	d.run = run
	return run, true
}

func (d *Dictation) attachProvider(run *engineRun, provider stt.Provider) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.shuttingDown || d.run != run || run.gen <= d.stoppedThrough {
		return false
	}
	run.provider = provider
	return true
}

func (d *Dictation) isCurrentRun(run *engineRun) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.shuttingDown && d.run == run && run.gen > d.stoppedThrough
}

func (d *Dictation) detachRunThrough(gen int) *engineRun {
	d.mu.Lock()
	defer d.mu.Unlock()
	if gen > d.stoppedThrough {
		d.stoppedThrough = gen
	}
	if d.run == nil || d.run.gen > gen {
		return nil
	}
	run := d.run
	d.run = nil
	return run
}
```

Replace the top-level provider field with:

```go
run             *engineRun
stoppedThrough  int
shuttingDown    bool
providerFactory func(int) (stt.Provider, error)
```

Add a production fallback:

```go
func (d *Dictation) providerFor(gen int) (stt.Provider, error) {
	if d.providerFactory != nil {
		return d.providerFactory(gen)
	}
	return d.buildProvider()
}
```

- [ ] **Step 4: Route start, stop, and shutdown through the run**

At the beginning of `StartEngine`, refuse invalid generations before any timer, meter, focus, provider, or microphone side effect:

```go
run, ok := d.beginEngineRun(gen)
if !ok {
	return
}
```

Build with `providerFor(gen)`, attach before `Provider.Start`, and close a provider that lost the attachment race:

```go
provider, err := d.providerFor(gen)
if err != nil {
	d.ui.Log("STT-ERR", err.Error())
	d.controller.ProviderEvent(stt.Event{
		Type: stt.Canceled, Gen: gen,
		ErrorCode: "NotConfigured", Error: err.Error(),
	})
	d.controller.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: gen})
	return
}
if !d.attachProvider(run, provider) {
	provider.Stop()
	return
}
if err := provider.Start(gen, d.controller.ProviderEvent); err != nil {
	d.ui.Log("STT-ERR", fmt.Sprintf("provider start failed: %v", err))
	return
}
if !d.isCurrentRun(run) {
	return
}
```

Make `StopEngine` invalidate and detach before existing cleanup, then stop only the detached provider:

```go
func (d *Dictation) StopEngine(gen int) {
	d.ui.Log("CTRL", fmt.Sprintf("stopEngine gen=%d", gen))
	run := d.detachRunThrough(gen)
	d.clearTimers()
	d.stopCapture()
	if run != nil && run.provider != nil {
		run.provider.Stop()
	}
}
```

Make `Shutdown` set `shuttingDown`, detach the current run, and stop its provider outside `d.mu`. Task 4 will replace broad timer cleanup; Task 3 will move capture cleanup into the run.

- [ ] **Step 5: Confirm the provider lifecycle suite is GREEN**

Run the focused command from Step 2. Expected: PASS with zero provider constructions for the stopped generation, exactly-once cleanup, and no stale stop affecting the newer provider.

**Provider-lifecycle RED test bodies required by Step 2**

Add a completion helper; the timeout is only a deadlock guard, while the interleaving is controlled by explicit barriers:

```go
func waitLifecycleDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle operation deadlocked")
	}
}
```

Add the late-factory test:

```go
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
```

Add the in-flight-start test:

```go
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
```

Add stale and duplicate stop tests:

```go
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
```

- [ ] **Step 6: Run provider lifecycle and package tests**

Run:

```bash
./scripts/go.sh test ./internal/app -run 'Test(StoppedGeneration|ProviderReturned|StopDuringProvider|StaleStop|DuplicateStop)' -count=1
./scripts/go.sh test ./internal/app -count=1
```

Expected: PASS. Record every red/green transition in `.workflow/state.md`; do not commit yet.

---

### Task 3: Move capture, pump, and metering into the generation-owned run

**Files:**
- Modify: `internal/app/dictation_lifecycle.go`
- Modify: `internal/app/dictation_lifecycle_test.go`
- Modify: `internal/app/dictation.go:63-90,104-179,249-455,458-545,599-613`
- Modify: `internal/app/provider_test.go:40-95`

**Interfaces:**
- Consumes: Task 2's `engineRun` and attachment checks, `audio.Frame`, `audio.StartCapture`.
- Produces: `captureStream`, `captureOpener`, `attachCapture`, generation-scoped `noteLevel(gen, level)`, `startCapture(run, provider) (bool, error)`, and `cleanupEngineRun(run)`.

- [ ] **Step 1: Write the old-capture replacement regression**

Define a capture fake that implements the real boundary:

```go
type fakeCapture struct {
	mu         sync.Mutex
	frames     chan audio.Frame
	closeCount int
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
```

Before running or changing production, also add the four test bodies in the `Capture and meter RED
test bodies required by Step 1` section below. All five regressions must be present before the first
Task 3 implementation change.

Add this test; import `internal/audio` for the fake above:

```go
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
```

Run:

```bash
./scripts/go.sh test ./internal/app -run 'Test(Replacement|CaptureReturned|StaleStop|DuplicateStop|OldGeneration)' -count=1
```

Expected: FAIL to compile because `captureOpener` does not exist; current concrete `*audio.Capture` storage cannot express or test ownership.

- [ ] **Step 2: Add the capture boundary and final run fields**

In `dictation_lifecycle.go` add:

```go
type captureStream interface {
	Frames() <-chan audio.Frame
	Close()
}

type captureOpener func(deviceID string, onLog func(string)) (captureStream, error)

type engineRun struct {
	gen         int
	provider    stt.Provider
	capture     captureStream
	pumpStop    chan struct{}
	peakLevel   float64
	cleanupOnce sync.Once
}
```

Add `captureOpener captureOpener` to `Dictation` and the fallback:

```go
func (d *Dictation) openCapture(deviceID string, onLog func(string)) (captureStream, error) {
	if d.captureOpener != nil {
		return d.captureOpener(deviceID, onLog)
	}
	return audio.StartCapture(deviceID, onLog)
}
```

Remove the top-level `capture`, `pumpDone`, `peakLevel`, and `metering` fields only after all references use `engineRun`.

- [ ] **Step 3: Attach capture transactionally and close over run-owned objects**

Add:

```go
func (d *Dictation) attachCapture(run *engineRun, capture captureStream, pumpStop chan struct{}) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.shuttingDown || d.run != run || run.gen <= d.stoppedThrough {
		return false
	}
	run.capture = capture
	run.pumpStop = pumpStop
	return true
}
```

Replace `startCapture` with:

```go
func (d *Dictation) startCapture(run *engineRun, provider stt.Provider) (bool, error) {
	capture, err := d.openCapture(d.store.LoadSettings().InputDeviceID, nil)
	if err != nil {
		if !d.isCurrentRun(run) {
			return false, nil
		}
		return false, fmt.Errorf("microphone: %w", err)
	}
	pumpStop := make(chan struct{})
	if !d.attachCapture(run, capture, pumpStop) {
		capture.Close()
		return false, nil
	}

	go func(gen int, capture captureStream, pumpStop <-chan struct{}) {
		for {
			select {
			case <-pumpStop:
				return
			case frame, ok := <-capture.Frames():
				if !ok {
					return
				}
				provider.PushAudio(frame.PCM)
				d.noteLevel(gen, frame.Level)
				if frame.Level > 0 {
					d.noteActivity()
				}
			}
		}
	}(run.gen, capture, pumpStop)
	return true, nil
}
```

In `StartEngine`, treat `started == false && err == nil` as concurrent invalidation and return silently. Preserve the current `NotConfigured` cancel path only for a real microphone error.

- [ ] **Step 4: Make level and peak state generation-scoped**

Change `noteLevel` to accept a generation and update only the current run:

```go
func (d *Dictation) noteLevel(gen int, level float64) {
	d.mu.Lock()
	if d.run == nil || d.run.gen != gen || gen <= d.stoppedThrough {
		d.mu.Unlock()
		return
	}
	if level > d.run.peakLevel {
		d.run.peakLevel = level
	}
	d.mu.Unlock()
	d.ui.EmitLevel(level)
}
```

Change `buildProvider()` to `buildProvider(gen int)`, pass `gen` from `providerFor`, and bind helper levels explicitly:

```go
OnLevel: func(level float64) { d.noteLevel(gen, level) },
```

Apply that closure to Apple and Whisper. Update host capture to call `noteLevel(run.gen, frame.Level)`. Update provider-construction tests to call `buildProvider(1)`.

- [ ] **Step 5: Centralize idempotent run cleanup**

Add one cleanup function and use it from `StopEngine`, displaced/failed attachment paths, and `Shutdown`:

```go
func (d *Dictation) cleanupEngineRun(run *engineRun) {
	if run == nil {
		return
	}
	run.cleanupOnce.Do(func() {
		if run.pumpStop != nil {
			close(run.pumpStop)
		}
		if run.capture != nil {
			run.capture.Close()
		}
		d.ui.EmitLevel(0)
		d.ui.Log("MIC", fmt.Sprintf("peak level this session: %.2f", run.peakLevel))
		if run.provider != nil {
			run.provider.Stop()
		}
	})
}
```

Detaching `d.run` under `d.mu` establishes single ownership; `cleanupOnce` is the final defense against duplicate channel close or provider stop if a future caller violates that ownership. Remove `stopCapture` and the top-level provider cleanup after every caller uses `cleanupEngineRun`.

**Capture and meter RED test bodies required by Step 1**

Add the late capture test:

```go
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
```

Add stale and duplicate capture tests:

```go
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
```

Add a generation-scoped meter test; import `strings`:

```go
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
```

The mutations these tests must catch are removal of the attachment validity check, unconditional cleanup of the current run, duplicate channel close, and loss of the generation predicate in `noteLevel`.

- [ ] **Step 6: Run the app lifecycle suite and race detector**

Run:

```bash
./scripts/go.sh test ./internal/app -run 'Test(Replacement|CaptureReturned|StaleStop|DuplicateStop|OldGeneration)' -count=1
./scripts/go.sh test ./internal/app -count=1
./scripts/go.sh test -race ./internal/app -run 'Test(StoppedGeneration|ProviderReturned|StopDuringProvider|Replacement|CaptureReturned|StaleStop|DuplicateStop|OldGeneration)' -count=1
```

Expected: PASS with no race report. Record evidence in `.workflow/state.md`; do not commit yet.

---

### Task 4: Bind reconnect and idle timers to generations

**Files:**
- Modify: `internal/session/controller.go:28-50,362-374`
- Modify: `internal/session/controller_test.go:15-50,774-834`
- Modify: `internal/app/dictation_lifecycle.go`
- Modify: `internal/app/dictation_lifecycle_test.go`
- Modify: `internal/app/dictation.go:72-76,232-240,547-613`
- Modify: `internal/stt/grok/provider_failures_test.go:590-620`

**Interfaces:**
- Consumes: Task 2's `stoppedThrough`, Task 3's `engineRun`, controller replacement generation.
- Produces: `IO.ScheduleReconnect(gen int, delay time.Duration, fn func())`, `Controller.StopByGuard(gen int)`, generation-owned idle guard, `timerStopper`, `reconnectWait`, and `scheduleAfter` seam.

- [ ] **Step 1: Write timer invalidation regressions with a manual scheduler**

Add test doubles:

```go
type fakeTimer struct {
	mu      sync.Mutex
	stopped int
}

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	t.stopped++
	t.mu.Unlock()
	return true
}

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
```

Add a constructor that installs the manual scheduler:

```go
func newTimerDictation(t *testing.T) (*Dictation, *manualAfter) {
	t.Helper()
	st := store.NewAt(t.TempDir())
	d := NewDictation(st, &silentUI{})
	after := &manualAfter{}
	d.scheduleAfter = after.scheduleFn
	return d, after
}
```

Add the stopped and stale generation tests:

```go
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
```

Add the replacement identity test:

```go
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
```

Run the three tests. Expected: FAIL to compile because reconnect scheduling has no generation or injectable timer boundary.

- [ ] **Step 2: Define the timer owner without synchronous callback hazards**

Add:

```go
type timerStopper interface {
	Stop() bool
}

type reconnectWait struct {
	gen     int
	mu      sync.Mutex
	timer   timerStopper
	stopped bool
}

type scheduleAfterFunc func(time.Duration, func()) timerStopper

func realScheduleAfter(delay time.Duration, fn func()) timerStopper {
	return time.AfterFunc(delay, fn)
}

func (w *reconnectWait) attachTimer(timer timerStopper) {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		timer.Stop()
		return
	}
	w.timer = timer
	w.mu.Unlock()
}

func (w *reconnectWait) stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	timer := w.timer
	w.timer = nil
	w.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
}
```

Replace `reconnect *time.Timer` with `reconnect *reconnectWait` and add `scheduleAfter scheduleAfterFunc` to `Dictation`. Use `realScheduleAfter` when the seam is nil.

Implement schedule publication and a callback validity gate:

```go
func (d *Dictation) ScheduleReconnect(gen int, delay time.Duration, fn func()) {
	d.ui.Log("RECONNECT", fmt.Sprintf("retry in %s", delay))
	wait := &reconnectWait{gen: gen}

	d.mu.Lock()
	if d.shuttingDown || gen <= d.stoppedThrough {
		d.mu.Unlock()
		return
	}
	previous := d.reconnect
	d.reconnect = wait
	d.mu.Unlock()
	if previous != nil {
		previous.stop()
	}

	schedule := d.scheduleAfter
	if schedule == nil {
		schedule = realScheduleAfter
	}
	timer := schedule(delay, func() {
		d.mu.Lock()
		valid := !d.shuttingDown && d.reconnect == wait && gen > d.stoppedThrough
		if d.reconnect == wait {
			d.reconnect = nil
		}
		d.mu.Unlock()
		if valid {
			fn()
		}
	})
	wait.attachTimer(timer)
}
```

The wait-local mutex handles a concurrent stop between publishing the wait and receiving the timer
handle. `stop` either stops an attached timer or leaves a tombstone that makes `attachTimer` stop the
late handle. The callback independently checks wait identity and the generation tombstone.

- [ ] **Step 3: Tag the session IO call with the replacement generation**

Change the interface and controller call:

```go
ScheduleReconnect(gen int, delay time.Duration, fn func())
```

```go
c.io.ScheduleReconnect(gen, delay, func() {
	c.mu.Lock()
	desired := c.tracker.Desired()
	c.mu.Unlock()
	if desired {
		c.io.StartEngine(gen)
	}
})
```

Update `fakeIO`, `reentrantIO`, `syncSessionIO`, `Dictation`, and Grok's test `fakeIO`. Add `reconnectGens []int` to the controller fake and assert the first retry targets generation 2. Keep the desired-state check as an early filter; update its comment to identify `Dictation`'s tombstone as the final race barrier.

- [ ] **Step 4: Cancel only timers covered by a stop**

Replace `detachRunThrough` with a combined ownership transfer:

```go
func (d *Dictation) detachThrough(gen int) (*engineRun, *reconnectWait) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if gen > d.stoppedThrough {
		d.stoppedThrough = gen
	}
	var run *engineRun
	if d.run != nil && d.run.gen <= gen {
		run = d.run
		d.run = nil
	}
	var wait *reconnectWait
	if d.reconnect != nil && d.reconnect.gen <= gen {
		wait = d.reconnect
		d.reconnect = nil
	}
	return run, wait
}
```

Use it without external calls under `d.mu`:

```go
func (d *Dictation) StopEngine(gen int) {
	d.ui.Log("CTRL", fmt.Sprintf("stopEngine gen=%d", gen))
	run, wait := d.detachThrough(gen)
	if wait != nil {
		wait.stop()
	}
	d.cleanupEngineRun(run)
}
```

A stale `StopEngine(1)` leaves generation-2 run and wait installed. `Shutdown` must set `shuttingDown`, detach any run/wait regardless of generation, release `d.mu`, call `wait.stop()`, and call `cleanupEngineRun`.

- [ ] **Step 5: Move the idle guard into `engineRun`**

Extend the run:

```go
idleTicker *time.Ticker
idleStop   chan struct{}
```

Change `startIdleGuard` to attach only to the current run:

```go
func (d *Dictation) startIdleGuard(run *engineRun) bool {
	ticker := time.NewTicker(5 * time.Second)
	stop := make(chan struct{})
	d.mu.Lock()
	if d.shuttingDown || d.run != run || run.gen <= d.stoppedThrough {
		d.mu.Unlock()
		ticker.Stop()
		close(stop)
		return false
	}
	run.idleTicker = ticker
	run.idleStop = stop
	d.mu.Unlock()

	go func() {
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if !d.isCurrentRun(run) {
					return
				}
				d.mu.Lock()
				last := d.lastActivity
				d.mu.Unlock()
				if d.controller.Desired() && session.IsIdleExpired(last, time.Now(), idleLimit) {
					d.ui.Log("IDLE", "auto-stop after 60s of silence (billing safety)")
					d.controller.StopByGuard(run.gen)
					return
				}
			}
		}
	}()
	return true
}
```

At the start of `cleanupEngineRun`, stop `run.idleTicker` and close `run.idleStop` when non-nil, before closing capture/provider resources. Remove global `idleTicker`, `idleStop`, and `clearTimers`. In `StartEngine`, return without logging success if `startIdleGuard(run)` returns false.

Make the controller perform the final generation check atomically with its stop decision:

```go
func (c *Controller) StopByGuard(gen int) {
	c.mu.Lock()
	if !c.tracker.Desired() || c.tracker.Generation() != gen {
		c.mu.Unlock()
		return
	}
	c.applyLocked(c.machine.Interrupt())
	c.doStopLocked()
	c.flushLocked()
	effects := c.takeEffectsLocked()
	c.mu.Unlock()
	c.flushEffects(effects)
}
```

Update all three existing controller-test calls to pass `c.Generation()`. Add:

```go
func TestStaleIdleGuardCannotStopNewGeneration(t *testing.T) {
	c, io := newFixture(ModeToggle)
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})
	c.Press()
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 2})
	stops := len(io.stops)

	c.StopByGuard(1)

	if !c.Desired() || len(io.stops) != stops {
		t.Fatalf("stale idle guard stopped generation 2: desired=%t stops=%v",
			c.Desired(), io.stops)
	}
}
```

- [ ] **Step 6: Confirm timer and neighboring lifecycle behavior is GREEN**

Run:

```bash
./scripts/go.sh test ./internal/session ./internal/app ./internal/stt/grok -count=1
./scripts/go.sh test -race ./internal/session ./internal/app -count=1
```

Expected: PASS with no race report. Verify exact reconnect delays, six retry schedules, timer target generations, no late start after stop, and no stale stop affecting newer resources. Record evidence; do not commit yet.

---

### Task 5: Document, cross-review, verify, and ship the fix

**Files:**
- Create: `docs/solutions/reconnect-capture-lifecycle.md`
- Modify: `docs/CHANGELOG.md`
- Modify: `CONTINUITY.md`
- Update: `.workflow/state.md`
- Review: every file changed since commit `4811b86`

**Interfaces:**
- Consumes: all green behavior from Tasks 1-4 and the approved design spec.
- Produces: durable diagnosis, clean cross-engine review, completed standard ship gate, and one implementation commit.

- [ ] **Step 1: Run formatter and focused mutation checks**

Run `gofmt` on every changed Go file. Then temporarily make and restore each mutation one at a time:

1. remove retry-path `StopEngine(failedGen)`; the controller ordering test must fail;
2. remove `gen <= stoppedThrough` from `beginEngineRun`; stopped-before-start must fail;
3. allow capture attachment after `d.run != run`; late-capture cleanup must fail;
4. detach the current run without `run.gen <= gen`; stale-stop provider/capture tests must fail;
5. remove reconnect wait identity/tombstone validation; stopped or superseded timer tests must fail.

After restoring each mutation, rerun its focused test and confirm PASS. Record mutation names and outcomes in `.workflow/state.md`.

- [ ] **Step 2: Write the durable solution and changelog entry**

Create `docs/solutions/reconnect-capture-lifecycle.md` with these exact sections:

- `## Symptom`: reconnect overwrote the only provider/capture/pump handles and a stop could precede a late start.
- `## Root cause`: controller generations gated events but app resources and timers had no generation ownership.
- `## Fix`: stop-before-backoff ordering, generation-owned run, stopped-through tombstone, transactional attachments, and generation-tagged timers.
- `## Tradeoffs`: no audio during backoff, asynchronous provider stop, added lifecycle state, unchanged retry/error policy.
- `## Verification`: red messages, mutation checks, race results, code-review rounds, full gate result.

Add an Unreleased changelog bullet stating that reconnects now close the previous microphone/provider resources and a stop cannot be undone by a late timer. Rewrite `CONTINUITY.md` so this fix is complete and its next step is the highest-priority unresolved owner or provider item discovered from the current repo, not the now-finished capture leak.

- [ ] **Step 3: Run self-review against the approved design**

Check every goal and non-goal in `docs/superpowers/specs/2026-08-07-reconnect-capture-lifecycle-design.md`. Run:

```bash
git diff --check
git diff --stat 4811b86
rg -n "provider|capture|pump|stoppedThrough|ScheduleReconnect|reconnect" internal/app internal/session --glob '*.go'
```

Confirm there is one runtime owner for each provider/capture/pump, no app resource field can be overwritten outside `engineRun`, and all `session.IO` implementations compile with the same signature.

- [ ] **Step 4: Cross-review the complete diff with the other engine**

Invoke the repository `review` skill using Claude Opus/high in read-only mode. Ask it to inspect teardown ordering, synchronous callback reentrancy, stop/start and timer races, stale generations, duplicate close/channel panic, provider-stop idempotency, meter/idle behavior, transcript preservation, and test honesty. Record P0-P3 counts and dispositions in `.workflow/state.md`; repeat until no P0/P1/P2 remain.

- [ ] **Step 5: Run simplify and verification skills**

Invoke `simplify` for a behavior-preserving cleanup pass, then `verify-e2e`. Record E2E as:

```text
E2E verified — N/A: reconnect resource ownership is internal and no sanctioned API, CLI, or UI journey can deterministically force the required stop/start interleavings; controller/app lifecycle regressions and the race detector are the applicable evidence.
```

- [ ] **Step 6: Run the final exact-tree verification**

Run:

```bash
./scripts/go.sh test ./internal/session ./internal/app ./internal/stt/... -count=1
./scripts/go.sh test -race ./internal/session ./internal/app -count=1
./scripts/task.sh check
git diff --check
sh shared/scripts/check-gates.sh
```

Expected: every command exits 0. Existing macOS deployment-target/rpath linker warnings may appear; any new warning, race, test failure, vet failure, or typecheck failure blocks completion.

- [ ] **Step 7: Make the single gated implementation commit**

Only after all six standard boxes are checked, stage the intended files explicitly and inspect `git diff --cached --check` plus `git diff --cached --stat`. Commit without a coauthor:

```bash
git commit -m "fix(app): own reconnect resources by generation"
```

Do not push or create a PR until the user selects an integration option through the finishing workflow.
