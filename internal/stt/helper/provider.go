package helper

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// The stop protocol's timings. Read the comment on Stop before touching them.
const (
	silenceGrace = 10 * time.Second
	exitCap      = 180 * time.Second
	signalAfter  = 300 * time.Millisecond
)

// Config describes one native helper.
type Config struct {
	// Bin is the compiled helper.
	Bin string
	// BuildCmd is what the user should run when Bin is missing. Shown verbatim, so it has
	// to be the command that actually builds it in THIS project.
	BuildCmd string
	// Locale is argv[1]. whisper understands "auto"; the Apple engine needs a real locale
	// because it cannot auto-detect.
	Locale string
	// ExtraArgs follow the locale (whisper takes the model path).
	ExtraArgs []string
	// Env is added to the helper's environment (whisper reads LOQUI_WHISPER_GPU).
	Env map[string]string
	// GPUEnabled records whether this run was asked to use the GPU, for crash attribution.
	GPUEnabled bool
	// Log receives diagnostics. Never called with transcript text.
	Log func(tag, msg string)
	// OnGPUCrash is called when the GPU backend took the process down, so the caller can
	// remember it and stop offering that backend on this machine.
	OnGPUCrash func(reason string)

	// SilenceGrace and ExitCap override the stop-protocol timings. Zero means the defaults.
	// Only the tests set them — a 10 s wait per case would make the suite unusable.
	SilenceGrace time.Duration
	ExitCap      time.Duration
}

func (c Config) silenceGrace() time.Duration {
	if c.SilenceGrace > 0 {
		return c.SilenceGrace
	}
	return silenceGrace
}

func (c Config) exitCap() time.Duration {
	if c.ExitCap > 0 {
		return c.ExitCap
	}
	return exitCap
}

// Provider runs a native STT helper as a child process.
//
// These helpers capture their OWN audio — the Apple engine through AVAudioEngine, whisper
// through SDL — so unlike the cloud providers they are not fed PCM. That is the one place
// the single-capture design of this port does not apply, and it is deliberate: rewiring the
// helpers to accept stdin audio would mean maintaining a fork of code that is currently
// vendored unchanged.
type Provider struct {
	cfg Config

	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	gen      int
	sink     stt.Sink
	sawFinal bool
	// stopping marks a teardown WE asked for, so the exit handler knows not to report
	// `stopped` itself — Stop owns that, after waiting for the flush.
	stopping bool
	settled  bool
	// lastOutput is bumped on every byte the helper prints, which is what lets the
	// watchdog measure silence instead of duration. See Stop.
	lastOutput time.Time
	done       chan struct{}
}

func New(cfg Config) *Provider {
	if cfg.Log == nil {
		cfg.Log = func(string, string) {}
	}
	return &Provider{cfg: cfg}
}

// WantsAudio: no. The helper opens the microphone itself.
func (p *Provider) WantsAudio() bool { return false }

// PushAudio is a no-op, for the same reason.
func (p *Provider) PushAudio([]byte) {}

// Start launches the helper.
func (p *Provider) Start(gen int, sink stt.Sink) error {
	p.mu.Lock()
	if p.cmd != nil {
		p.mu.Unlock()
		return fmt.Errorf("helper: already started")
	}
	p.gen = gen
	p.sink = sink
	p.done = make(chan struct{})
	p.mu.Unlock()

	if _, err := os.Stat(p.cfg.Bin); err != nil {
		// Say what is missing and how to fix it. A helper that isn't compiled is a
		// configuration problem, never a transient one, so it must not be retried.
		err = fmt.Errorf("helper nativo no compilado — corre `%s`", p.cfg.BuildCmd)
		p.emit(stt.Event{Type: stt.Canceled, ErrorCode: "NotConfigured", Error: err.Error()})
		p.emit(stt.Event{Type: stt.Stopped})
		return err
	}

	cmd := exec.Command(p.cfg.Bin, append([]string{p.cfg.Locale}, p.cfg.ExtraArgs...)...)
	cmd.Env = os.Environ()
	for k, v := range p.cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// stdin is PIPED, not ignored: it is how the helper is asked to stop portably. Signals
	// are not an option on every platform, and the whisper helper listens for a line.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		p.emit(stt.Event{Type: stt.Canceled, Error: err.Error()})
		p.emit(stt.Event{Type: stt.Stopped})
		return err
	}

	p.mu.Lock()
	p.cmd, p.stdin = cmd, stdin
	p.lastOutput = time.Now()
	p.mu.Unlock()

	go p.readStdout(stdout)
	go p.readStderr(stderr)
	go p.wait(cmd)

	return nil
}

func (p *Provider) readStdout(r io.Reader) {
	scan := bufio.NewScanner(r)
	// Whisper can emit a long line for a long utterance; the default 64 KB limit would
	// truncate a transcript mid-sentence and produce invalid JSON.
	scan.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scan.Scan() {
		p.noteOutput()
		line := scan.Text()
		evt, ok := ParseLine(line)
		if !ok {
			// Log what we dropped instead of discarding it silently. These are the
			// helper's own diagnostics — which locale it settled on, that it is
			// downloading a language model, why it refused to start — and they are exactly
			// what explains a session that produced no transcript. The Electron build
			// swallowed them and left nothing to look at.
			//
			// Safe to log: a real transcript parses as an event and never reaches here.
			if t := strings.TrimSpace(line); t != "" {
				p.cfg.Log("STT-INFO", truncate(t, 300))
			}
			continue
		}
		if evt.Type == stt.Final {
			p.mu.Lock()
			p.sawFinal = true
			p.mu.Unlock()
		}
		p.emit(evt)
	}
}

func (p *Provider) readStderr(r io.Reader) {
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		p.noteOutput() // progress chatter counts as being alive — see Stop
		p.cfg.Log("STT-ERR", scan.Text())
	}
}

func (p *Provider) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	code := exitCode(err)

	p.mu.Lock()
	sawFinal, stopping := p.sawFinal, p.stopping
	p.mu.Unlock()

	p.cfg.Log("STT", fmt.Sprintf("helper exited (%s)", FormatExitCode(code)))

	// A GPU backend that takes the process down leaves NOTHING behind — no event, no
	// message, just a vanished helper. Say so, and stop handing dictation to that backend
	// on this machine; whisper then runs on the CPU, slower but alive.
	if IsGPUCrash(ExitFacts{Code: code, SawFinal: sawFinal, GPUEnabled: p.cfg.GPUEnabled}) {
		reason := fmt.Sprintf("helper exited %s with no transcript", FormatExitCode(code))
		p.cfg.Log("STT-ERR", "GPU backend crashed ("+reason+") — disabling it on this machine")
		if p.cfg.OnGPUCrash != nil {
			p.cfg.OnGPUCrash(reason)
		}
		p.emit(stt.Event{
			Type:  stt.Canceled,
			Error: "La GPU falló al transcribir; se usará el procesador. Vuelve a intentarlo.",
		})
	}

	if p.done != nil {
		close(p.done)
	}
	// Only an UNEXPECTED exit reports stopped from here; a requested stop is owned by Stop,
	// which waits for this same exit so the tail is not cut off.
	if !stopping {
		p.finish("died")
	}
}

// Stop asks the helper to end and waits for it to actually exit.
//
// WHY IT WAITS. Asking the helper to stop does not end the work: it then transcribes the
// audio it still has buffered and emits one last final. Emitting `stopped` immediately
// flushed the transcript BEFORE that final arrived, which silently cut the tail off every
// dictation.
//
// THE WATCHDOG MEASURES SILENCE, NOT DURATION. A fixed deadline cannot work here: the final
// pass takes under a second on Metal but 10-15 s on a CPU-only machine, and minutes for a
// long dictation, since the buffer holds up to 300 s of audio. A 4 s deadline killed the
// helper mid-flush on every dictation — the transcript was correct and thrown away. The
// helper prints a progress line while it works, and each one re-arms this timer, so what
// gets killed is a process that has genuinely stopped saying anything.
func (p *Provider) Stop() {
	p.mu.Lock()
	if p.cmd == nil || p.stopping {
		p.mu.Unlock()
		return
	}
	p.stopping = true
	// ARM THE GRACE PERIOD NOW, not from whatever the helper last said.
	//
	// This was a real bug in the port: the watchdog compared against the last output
	// timestamp, but a helper prints NOTHING while it listens — whisper logs its init noise
	// and then stays quiet for the whole dictation. So by the time the user let go, the
	// "silence" already exceeded the grace period and the helper was killed within a
	// second, dropping precisely the tail this watchdog exists to protect. The Electron
	// original armed its timer at stop time; so does this.
	p.lastOutput = time.Now()
	cmd, stdin, done := p.cmd, p.stdin, p.done
	p.mu.Unlock()

	// 1. Ask over stdin. Portable, and the only mechanism that works everywhere: a kill
	//    would cut the tail flush short.
	if stdin != nil {
		_, _ = stdin.Write([]byte("stop\n"))
		_ = stdin.Close()
	}

	go func() {
		// 2. POSIX fallback for helpers that only listen for signals (the Apple one).
		//    Harmless for whisper, which is already stopping.
		sigTimer := time.AfterFunc(signalAfter, func() {
			if !p.isSettled() && cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
		})
		defer sigTimer.Stop()

		capAt := time.After(p.cfg.exitCap())
		for {
			select {
			case <-done:
				p.finish("exit")
				return
			case <-capAt:
				// A helper stuck in a progress loop must not hold the session open for
				// ever. Costs the tail, never the session.
				p.kill()
				p.finish("helper still running after the cap — tail dropped")
				return
			case <-time.After(pollEvery(p.cfg.silenceGrace())):
				if time.Since(p.lastOutputAt()) > p.cfg.silenceGrace() {
					p.kill()
					p.finish("helper went silent — tail dropped")
					return
				}
			}
		}
	}()
}

func (p *Provider) kill() {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// finish emits `stopped` exactly once, whatever route got us here.
func (p *Provider) finish(why string) {
	p.mu.Lock()
	if p.settled {
		p.mu.Unlock()
		return
	}
	p.settled = true
	p.mu.Unlock()

	if why != "exit" && why != "died" {
		p.cfg.Log("STT", "stop: "+why)
	}
	p.emit(stt.Event{Type: stt.Stopped})
}

func (p *Provider) isSettled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.settled
}

func (p *Provider) noteOutput() {
	p.mu.Lock()
	p.lastOutput = time.Now()
	p.mu.Unlock()
}

func (p *Provider) lastOutputAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastOutput
}

func (p *Provider) emit(evt stt.Event) {
	p.mu.Lock()
	sink, gen := p.sink, p.gen
	p.mu.Unlock()
	if sink == nil {
		return
	}
	evt.Gen = gen
	sink(evt)
}

// pollEvery keeps the silence check finer-grained than the grace period it is measuring, so
// a short grace (the tests) is not overshot by a whole poll interval.
func pollEvery(grace time.Duration) time.Duration {
	if step := grace / 5; step < time.Second {
		if step < 10*time.Millisecond {
			return 10 * time.Millisecond
		}
		return step
	}
	return time.Second
}

// truncate keeps a log line readable without hiding the start of it.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// exitCode extracts the process exit code, nil when it was killed by a signal.
func exitCode(err error) *int {
	if err == nil {
		zero := 0
		return &zero
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		return nil
	}
	status, ok := ee.Sys().(syscall.WaitStatus)
	if !ok {
		code := ee.ExitCode()
		return &code
	}
	if status.Signaled() {
		return nil // a signal, not an exit code — see IsGPUCrash
	}
	code := status.ExitStatus()
	return &code
}

var _ stt.Provider = (*Provider)(nil)
