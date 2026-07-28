package hotkey

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// Handlers are the callbacks a Listener invokes. All are optional.
//
// OnExit and OnError matter as much as the key events: if the helper dies mid-dictation
// nothing will ever report the key coming up, so the session must be failed closed rather
// than left with an open microphone and no way to close it.
type Handlers struct {
	OnFnDown      func()
	OnFnUp        func()
	OnFnInterrupt func()
	// OnStderr receives the helper's diagnostics (it complains here when Input
	// Monitoring is missing).
	OnStderr func(string)
	// OnError is a failure to run the helper at all.
	OnError func(error)
	// OnExit is the helper terminating, expectedly or not.
	OnExit func(err error)
}

// Listener is a running fn-key helper.
type Listener struct {
	cmd *exec.Cmd

	mu      sync.Mutex
	stopped bool
}

// Start launches the helper at binPath and streams its events.
//
// A missing binary is reported as an error rather than silently ignored: the app still
// works through the tray and the "Probar dictado" button, and the user needs to be told
// which of those two worlds they are in.
func Start(binPath string, h Handlers) (*Listener, error) {
	if _, err := os.Stat(binPath); err != nil {
		return nil, fmt.Errorf("hotkey: fn listener not built at %s (run scripts/build-globe-listener.sh): %w", binPath, err)
	}

	cmd := exec.Command(binPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("hotkey: cannot start the fn listener: %w", err)
	}

	l := &Listener{cmd: cmd}

	go func() {
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			switch ParseLine(scan.Text()) {
			case FnDown:
				if h.OnFnDown != nil {
					h.OnFnDown()
				}
			case FnUp:
				if h.OnFnUp != nil {
					h.OnFnUp()
				}
			case FnInterrupt:
				if h.OnFnInterrupt != nil {
					h.OnFnInterrupt()
				}
			}
		}
	}()

	go func() {
		scan := bufio.NewScanner(stderr)
		for scan.Scan() {
			if h.OnStderr != nil {
				h.OnStderr(scan.Text())
			}
		}
	}()

	go func() {
		err := cmd.Wait()
		l.mu.Lock()
		expected := l.stopped
		l.mu.Unlock()
		// A stop we asked for is not a failure; only an unexpected death is, and only
		// that one may fail the session closed.
		if !expected && h.OnExit != nil {
			h.OnExit(err)
		}
	}()

	return l, nil
}

// Stop terminates the helper. Safe to call twice.
func (l *Listener) Stop() {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return
	}
	l.stopped = true
	l.mu.Unlock()

	if l.cmd != nil && l.cmd.Process != nil {
		// The helper installs a SIGTERM handler that removes its event monitors and
		// exits cleanly, which matters: a global event tap left behind by a killed
		// process can wedge input handling for other apps.
		_ = l.cmd.Process.Signal(os.Interrupt)
		_ = l.cmd.Process.Kill()
	}
}
