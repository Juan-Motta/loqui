// The whisper.cpp model: what it is, and the pure helpers around fetching it.
//
// WHY IT IS DOWNLOADED AND NOT SHIPPED, ported verbatim from the original's reasoning because it is
// still the right trade: the model is 465 MB. Bundling it would take the DMG from ~97 MB to ~590 MB,
// and every update would re-download the whole thing. The cloud engines and Apple's on-device engine
// need no model at all, so only whisper users pay the cost, and they pay it once.
//
// EVERYTHING HERE IS PURE — no filesystem, no network. That is what makes the rules testable without
// a 465 MB fixture or a server, and it is the same split the original made (its modelStore.ts did the
// I/O). The downloader lives in internal/app, where the progress events and the HTTP client belong.
package store

import (
	"fmt"
	"strconv"
)

// ModelSpec is the one description of the model this app is built against.
type ModelSpec struct {
	File   string
	URL    string
	Bytes  int64
	SHA256 string
	Label  string
}

// WhisperModel is PINNED BY SIZE AND DIGEST, and both are load-bearing.
//
// Size alone would accept a truncated-then-padded file, or a mirror serving something else of the
// right length. The digest is what actually says "this is the model we tested against" — and getting
// that wrong does not fail at download time, it fails as garbage transcription that looks like a bug
// in this app.
var WhisperModel = ModelSpec{
	File:   "ggml-small.bin",
	URL:    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin",
	Bytes:  487601967,
	SHA256: "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b",
	Label:  "Whisper small (multilingüe)",
}

// ModelProblem is why the model on disk cannot be used. Empty when it can.
type ModelProblem string

const (
	// ModelMissing: nothing at that path. The download has never run.
	ModelMissing ModelProblem = "missing"
	// ModelIncomplete: smaller than it should be — an interrupted download, which is RESUMABLE.
	// Distinct from corrupt for exactly that reason: one offers "Resume", the other does not.
	ModelIncomplete ModelProblem = "incomplete"
	// ModelCorrupt: the wrong size, or the right size with the wrong digest. Not resumable.
	ModelCorrupt ModelProblem = "corrupt"
	// ModelUnverified: the right size, but nobody hashed it.
	//
	// A SEPARATE STATE RATHER THAN "ok", deliberately. Hashing 465 MB takes seconds, so a caller is
	// allowed to skip it — but a right-sized file is not proof, and handing whisper a corrupt model
	// fails in a far more confusing way than saying so here.
	ModelUnverified ModelProblem = "unverified"
)

// ModelVerdict is whether what is on disk can be used.
type ModelVerdict struct {
	OK      bool         `json:"ok"`
	Problem ModelProblem `json:"problem"`
}

// ModelOnDisk is what a caller found. SHA256 empty means "I did not hash it".
type ModelOnDisk struct {
	Bytes  int64
	SHA256 string
}

// VerdictFor judges what was found. A nil pointer means the file is not there at all — which is a
// different answer from "there and empty", and the UI offers a different button for each.
func VerdictFor(found *ModelOnDisk) ModelVerdict {
	switch {
	case found == nil:
		return ModelVerdict{Problem: ModelMissing}
	case found.Bytes < WhisperModel.Bytes:
		return ModelVerdict{Problem: ModelIncomplete}
	case found.Bytes > WhisperModel.Bytes:
		return ModelVerdict{Problem: ModelCorrupt}
	case found.SHA256 == "":
		return ModelVerdict{Problem: ModelUnverified}
	case found.SHA256 == WhisperModel.SHA256:
		return ModelVerdict{OK: true}
	default:
		return ModelVerdict{Problem: ModelCorrupt}
	}
}

var byteUnits = []string{"B", "KB", "MB", "GB"}

// FormatBytes is a human size for a progress line. NOT EXACT — legible, which is the whole point:
// nobody reads 487601967 while watching a bar move.
func FormatBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	value := float64(n)
	unit := 0
	for value >= 1024 && unit < len(byteUnits)-1 {
		value /= 1024
		unit++
	}
	switch {
	case unit == 0:
		return strconv.FormatInt(int64(value+0.5), 10) + " B"
	// Big numbers do not need decimals; small ones read wrong without them.
	case value >= 100:
		return fmt.Sprintf("%.0f %s", value, byteUnits[unit])
	default:
		return fmt.Sprintf("%.1f %s", value, byteUnits[unit])
	}
}

// ProgressPercent is clamped to 0..100. A server that reports a smaller total than it sends must not
// drive a bar to 140%.
func ProgressPercent(received, total int64) int {
	if total <= 0 {
		return 0
	}
	pct := int(float64(received)/float64(total)*100 + 0.5)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// RangeHeader is the HTTP Range for resuming, or "" when starting from scratch.
//
// Resuming is the difference between an interrupted 465 MB download costing nothing and costing
// everything, which is why "incomplete" is its own verdict.
func RangeHeader(existingBytes int64) string {
	if existingBytes <= 0 {
		return ""
	}
	return "bytes=" + strconv.FormatInt(existingBytes, 10) + "-"
}

// ETASeconds is the estimate, and the bool is whether there IS one.
//
// An unknown rate returns false rather than zero: "0 s remaining" on a download that has not started
// moving is a lie the user can see, and it makes the whole line untrustworthy.
func ETASeconds(received, total, bytesPerSecond int64) (int, bool) {
	if total <= 0 || bytesPerSecond <= 0 {
		return 0, false
	}
	remaining := total - received
	if remaining < 0 {
		remaining = 0
	}
	return int(float64(remaining)/float64(bytesPerSecond) + 0.5), true
}
