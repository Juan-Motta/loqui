// What this MACHINE turned out to be capable of — as opposed to what the user configured,
// which lives in settings.json. Ported from the Electron build's src/main/deviceState.ts.
//
// Kept apart on purpose: a user who copies their settings to another computer must not carry
// "the GPU is broken" with them, and nothing here is a preference anyone chose.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// deviceState is the per-machine record. Today it holds one fact: whether the whisper
// helper's GPU backend killed the process.
type deviceState struct {
	WhisperGPU string `json:"whisperGpu"` // "unknown" | "broken"
	// Reason is kept for the log and for a future "re-test the GPU" button.
	Reason string `json:"whisperGpuReason,omitempty"`
	At     string `json:"whisperGpuAt,omitempty"`
}

func (s *Store) devicePath() string { return filepath.Join(s.dir, "device.json") }

func (s *Store) readDevice() deviceState {
	raw, err := os.ReadFile(s.devicePath())
	if err != nil {
		return deviceState{WhisperGPU: "unknown"}
	}
	var st deviceState
	if err := json.Unmarshal(raw, &st); err != nil {
		return deviceState{WhisperGPU: "unknown"}
	}
	// Anything unrecognised reads as "unknown": a corrupt or hand-edited file must not
	// permanently disable the GPU, it must just be re-tested.
	if st.WhisperGPU != "broken" {
		st.WhisperGPU = "unknown"
	}
	return st
}

// WhisperGPUAllowed reports whether the whisper helper may use the GPU on this machine.
func (s *Store) WhisperGPUAllowed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readDevice().WhisperGPU != "broken"
}

// MarkWhisperGPUBroken records that the GPU backend took the helper down. Idempotent.
//
// A write failure is deliberately ignored: the fallback still applies for this run, it just
// won't be remembered. Losing one more dictation beats failing to start.
func (s *Store) MarkWhisperGPUBroken(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(deviceState{
		WhisperGPU: "broken",
		Reason:     reason,
		At:         time.Now().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.devicePath(), data, 0o600)
}

// ClearWhisperGPUVerdict lets the GPU be tried again — e.g. after a driver update.
func (s *Store) ClearWhisperGPUVerdict() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, _ := json.MarshalIndent(deviceState{WhisperGPU: "unknown"}, "", "  ")
	_ = os.WriteFile(s.devicePath(), data, 0o600)
}
