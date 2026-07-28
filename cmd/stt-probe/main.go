// stt-probe — run one real dictation from the command line: open the microphone, feed a
// provider, print every event.
//
// This is the successor to the throwaway spike that proved Azure works from Go
// (docs/research/2026-07-27-azure-speech-go-macos.md). It lives in the repo because the
// question it answers keeps coming back and is not answerable from a unit test: does the
// whole capture -> convert -> service -> transcript chain work on THIS machine, with
// THESE credentials, against a real microphone?
//
// It is deliberately not part of the app: no windows, no tray, no session controller.
// When a dictation misbehaves, this is how you find out which half is at fault.
//
// Run it through the wrapper, NOT with a bare `go run`: this binary links the Azure Speech
// SDK, so without the cgo flags in scripts/go.sh the build fails with
// "'speechapi_c_error.h' file not found" — even for -mic-only, which never touches Azure.
//
//	./scripts/go.sh run ./cmd/stt-probe -mic-only
//	SPEECH_KEY=... SPEECH_REGION=eastus ./scripts/go.sh run ./cmd/stt-probe -seconds 20
//	wails3 task probe:mic          # the same thing, shorter
//
// Without SPEECH_KEY it still exercises everything up to authentication, which is enough
// to tell a broken microphone from a broken credential.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/audio"
	"github.com/Juan-Motta/loqui-go/internal/stt"
	"github.com/Juan-Motta/loqui-go/internal/stt/azure"
)

func main() {
	seconds := flag.Int("seconds", 15, "how long to listen")
	langs := flag.String("langs", "es-CO,en-US", "comma-separated LID candidates (one locale per language)")
	device := flag.String("device", "", "input device id (see -list)")
	list := flag.Bool("list", false, "list input devices and exit")
	micOnly := flag.Bool("mic-only", false, "capture and report levels without contacting any service")
	flag.Parse()

	if *list {
		devices, err := audio.ListInputDevices()
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot list devices:", err)
			os.Exit(1)
		}
		for _, d := range devices {
			mark := " "
			if d.Default {
				mark = "*"
			}
			fmt.Printf("%s %-40s %s\n", mark, d.Name, d.ID)
		}
		return
	}

	// Half the diagnosis is "is the microphone even producing audio?", and answering it
	// without a credential in hand is the whole point of this mode: a silent capture and
	// a rejected key look identical from the outside otherwise.
	if *micOnly {
		micOnlyRun(*device, *seconds)
		return
	}

	region := os.Getenv("SPEECH_REGION")
	if region == "" {
		region = "eastus"
	}
	key := os.Getenv("SPEECH_KEY")
	if key == "" {
		fmt.Println("note: SPEECH_KEY is empty — expect AuthenticationFailure. The capture")
		fmt.Println("      half is still exercised, so a mic problem will show up first.")
	}

	tokens := azure.NewTokenService(azure.TokenOptions{
		Region: region,
		GetKey: func() (string, error) { return key, nil },
	})
	provider := azure.New(azure.Config{
		Region:     region,
		Candidates: strings.Split(*langs, ","),
		Tokens:     tokens,
	})

	done := make(chan struct{})
	var closeOnce bool
	finish := func() {
		if !closeOnce {
			closeOnce = true
			close(done)
		}
	}

	start := time.Now()
	sink := func(e stt.Event) {
		at := time.Since(start).Truncate(time.Millisecond)
		switch e.Type {
		case stt.Partial:
			fmt.Printf("[%8s] partial  %q\n", at, e.Text)
		case stt.Final:
			fmt.Printf("[%8s] FINAL    %q  (language=%s)\n", at, e.Text, e.Language)
		case stt.Canceled:
			fmt.Printf("[%8s] canceled code=%s %s\n", at, e.ErrorCode, e.Error)
		case stt.Stopped:
			fmt.Printf("[%8s] stopped\n", at)
			finish()
		default:
			fmt.Printf("[%8s] %s\n", at, e.Type)
		}
	}

	fmt.Printf("region=%s candidates=%s\n", region, *langs)
	if err := provider.Start(1, sink); err != nil {
		fmt.Fprintln(os.Stderr, "start failed:", err)
		// Not an early exit: the events already printed say more than this error does.
	}

	cap, err := audio.StartCapture(*device, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open the microphone:", err)
		fmt.Fprintln(os.Stderr, "on macOS this usually means the mic permission was denied")
		provider.Stop()
		os.Exit(1)
	}
	fmt.Printf("listening for %ds at %d Hz — speak now\n", *seconds, audio.CaptureSampleRate)

	// Feed the provider and report the level once a second, so silence is visibly
	// silence rather than an unexplained absence of transcript.
	go func() {
		var peak float64
		var bytes int
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case f, ok := <-cap.Frames():
				if !ok {
					return
				}
				bytes += len(f.PCM)
				if f.Level > peak {
					peak = f.Level
				}
				provider.PushAudio(f.PCM)
			case <-tick.C:
				fmt.Printf("           ... %d KB pushed, peak level %.2f\n", bytes/1024, peak)
				peak = 0
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	select {
	case <-time.After(time.Duration(*seconds) * time.Second):
	case <-sig:
		fmt.Println("\ninterrupted")
	case <-done: // a fatal cancel arrived before the clock ran out
	}

	cap.Close()
	provider.Stop()

	// Give the service its chance to flush the last segment — the same reason the real
	// session waits for Stopped instead of assuming teardown is instant.
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		fmt.Println("gave up waiting for the final flush")
	}
}

// micOnlyRun opens the device and reports what it hears, contacting nothing.
func micOnlyRun(device string, seconds int) {
	cap, err := audio.StartCapture(device, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open the microphone:", err)
		fmt.Fprintln(os.Stderr, "on macOS this usually means the mic permission was denied")
		os.Exit(1)
	}
	defer cap.Close()

	fmt.Printf("mic-only: %ds at %d Hz mono — say something\n", seconds, audio.CaptureSampleRate)
	deadline := time.After(time.Duration(seconds) * time.Second)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	var bytes int
	var peak float64
	var frames int
	for {
		select {
		case f, ok := <-cap.Frames():
			if !ok {
				return
			}
			frames++
			bytes += len(f.PCM)
			if f.Level > peak {
				peak = f.Level
			}
		case <-tick.C:
			// A bar, because a number does not make it obvious that the level is
			// tracking your voice.
			bar := strings.Repeat("#", int(peak*30))
			fmt.Printf("  %5d KB  %4d frames  peak %.3f |%-30s|\n", bytes/1024, frames, peak, bar)
			peak, frames = 0, 0
		case <-deadline:
			fmt.Printf("done: %d KB captured (%.1f s of audio at %d Hz)\n",
				bytes/1024, float64(bytes)/2/float64(audio.CaptureSampleRate), audio.CaptureSampleRate)
			return
		}
	}
}
