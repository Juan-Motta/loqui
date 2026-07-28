// The line protocol the native STT helpers speak. Ported from the Electron build's
// src/shared/sttHelperProtocol.ts and src/shared/helperExit.ts.
//
// Both local engines — Apple SpeechAnalyzer (helpers/macos-stt.swift) and whisper.cpp
// (helpers/whisper-stt.cpp) — print one JSON object per line on stdout. That is the whole
// interface, which is why they survived the port from Electron untouched: a line protocol
// does not care what language the host is written in.
package helper

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// ParseLine turns one line of helper output into an event, or false when the line is not
// one to act on.
//
// Unknown types are DROPPED rather than surfaced. The helpers emit "info" lines for
// diagnostics — which locale they settled on, whether they are downloading a language
// model — and stray stderr can end up interleaved on some pipes. Treating either as an
// event would put noise into the transcript.
func ParseLine(line string) (stt.Event, bool) {
	s := strings.TrimSpace(line)
	if s == "" {
		return stt.Event{}, false
	}

	var raw struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Language string `json:"language"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return stt.Event{}, false // non-JSON noise
	}

	switch stt.EventType(raw.Type) {
	case stt.Started, stt.Partial, stt.Final, stt.Canceled, stt.Stopped:
	default:
		return stt.Event{}, false // includes "info"
	}

	return stt.Event{
		Type:     stt.EventType(raw.Type),
		Text:     raw.Text,
		Language: raw.Language,
		Error:    raw.Error,
	}, true
}

// ntstatusFatalBase is where Windows fatal exception codes start: 0xC0000005 access
// violation, 0xC000001D illegal instruction, 0xC0000409 fast-fail (an abort, or an uncaught
// C++ exception — which is what vulkan.hpp throws when a driver refuses).
const ntstatusFatalBase = 0xc0000000

// ExitFacts is what is known about a helper that has ended.
type ExitFacts struct {
	// Code is the process exit code. Nil means terminated by a signal (POSIX).
	Code *int
	// SawFinal is whether this run delivered any transcript at all.
	SawFinal bool
	// GPUEnabled is whether this run was asked to use the GPU.
	GPUEnabled bool
}

// IsGPUCrash reports a fatal exit that produced nothing, while the GPU was in use.
//
// WHY THIS EXISTS: whisper's GPU backend is only as good as the driver behind it. An AMD
// iGPU under an old OEM driver took the helper down on the first transcription — no
// message, no assert, just a process that vanished. The app cannot fix someone's driver,
// but it must stop handing dictation to a backend that is killing it.
//
// Deliberately narrow. The helper's OWN error exits (1 = no microphone, 2 = model failed to
// load) are ordinary small codes and must never be blamed on the GPU; nor may a run that
// already delivered text, since whatever killed it came after the work. Signals are
// excluded too: on macOS that is indistinguishable from the SIGKILL this app itself sends
// when a helper overstays its welcome, and blaming the GPU for our own kill would
// permanently disable a backend that works.
func IsGPUCrash(facts ExitFacts) bool {
	if !facts.GPUEnabled || facts.SawFinal || facts.Code == nil {
		return false
	}
	return *facts.Code >= ntstatusFatalBase
}

// FormatExitCode renders an exit code for a log line. Hex for an NTSTATUS, because
// `3221226505` says nothing and `0xc0000409` says "fatal exception".
func FormatExitCode(code *int) string {
	if code == nil {
		return "signal"
	}
	if *code >= ntstatusFatalBase {
		return fmt.Sprintf("0x%x", *code)
	}
	return fmt.Sprintf("%d", *code)
}
