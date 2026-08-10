package app

import (
	"fmt"
	"sync"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/audio"
	"github.com/Juan-Motta/loqui-go/internal/stt"
)

type captureStream interface {
	Frames() <-chan audio.Frame
	Close()
}

type captureOpener func(deviceID string, onLog func(string)) (captureStream, error)

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

type engineRun struct {
	gen          int
	provider     stt.Provider
	capture      captureStream
	pumpStop     chan struct{}
	peakLevel    float64
	idleTicker   *time.Ticker
	idleStop     chan struct{}
	lastActivity time.Time
	cleanupOnce  sync.Once
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

func (d *Dictation) isCurrentRun(run *engineRun) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.shuttingDown && d.run == run && run.gen > d.stoppedThrough
}

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

func (d *Dictation) cleanupEngineRun(run *engineRun) {
	if run == nil {
		return
	}
	run.cleanupOnce.Do(func() {
		if run.idleTicker != nil {
			run.idleTicker.Stop()
		}
		if run.idleStop != nil {
			close(run.idleStop)
		}
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
