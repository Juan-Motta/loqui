package store

import "testing"

// THE SPEC IS PINNED BY SIZE **AND** DIGEST, and the reason is in the original: the size alone would
// accept a truncated-then-padded file, or a mirror serving something else entirely. The digest is
// what actually says "this is the model we tested against".
func TestTheModelIsPinnedBySizeAndDigest(t *testing.T) {
	if WhisperModel.File != "ggml-small.bin" {
		t.Errorf("file = %q", WhisperModel.File)
	}
	if WhisperModel.Bytes != 487601967 {
		t.Errorf("bytes = %d", WhisperModel.Bytes)
	}
	if len(WhisperModel.SHA256) != 64 {
		t.Errorf("sha256 = %q — a hex digest is 64 characters", WhisperModel.SHA256)
	}
	if WhisperModel.URL == "" {
		t.Error("no download URL")
	}
}

// A RIGHT-SIZED FILE IS NOT PROOF. "unverified" exists so that choosing not to hash — which costs
// seconds at 465 MB — can never be reported as ok: handing whisper a corrupt model fails in a far
// more confusing way than saying so here.
func TestModelVerdict(t *testing.T) {
	const good = "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b"
	for _, c := range []struct {
		name   string
		found  *ModelOnDisk
		reason ModelProblem
		ok     bool
	}{
		{"nothing there", nil, ModelMissing, false},
		{"still downloading", &ModelOnDisk{Bytes: 1000}, ModelIncomplete, false},
		{"bigger than the real one", &ModelOnDisk{Bytes: 487601968}, ModelCorrupt, false},
		{"right size, not hashed", &ModelOnDisk{Bytes: 487601967}, ModelUnverified, false},
		{"right size, wrong digest", &ModelOnDisk{Bytes: 487601967, SHA256: "deadbeef"}, ModelCorrupt, false},
		{"right size, right digest", &ModelOnDisk{Bytes: 487601967, SHA256: good}, "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := VerdictFor(c.found)
			if v.OK != c.ok {
				t.Errorf("OK = %v, want %v", v.OK, c.ok)
			}
			if v.Problem != c.reason {
				t.Errorf("problem = %q, want %q", v.Problem, c.reason)
			}
		})
	}
}

// "Not exact — legible", which is the whole point of a progress line.
func TestFormatBytes(t *testing.T) {
	for _, c := range []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{-1, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		// Big numbers do not need decimals; small ones read wrong without them.
		{150 * 1024, "150 KB"},
		{487601967, "465 MB"},
	} {
		if got := FormatBytes(c.in); got != c.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProgressPercent(t *testing.T) {
	for _, c := range []struct {
		got, total int64
		want       int
	}{
		{0, 100, 0},
		{50, 100, 50},
		{100, 100, 100},
		// Clamped: a server that reports a smaller total than it sends must not show 140%.
		{140, 100, 100},
		{-5, 100, 0},
		// No total, no percentage — and NOT a division by zero.
		{10, 0, 0},
	} {
		if got := ProgressPercent(c.got, c.total); got != c.want {
			t.Errorf("ProgressPercent(%d, %d) = %d, want %d", c.got, c.total, got, c.want)
		}
	}
}

// Resuming is the difference between a failed 465 MB download costing nothing and costing
// everything.
func TestRangeHeader(t *testing.T) {
	if got := RangeHeader(0); got != "" {
		t.Errorf("RangeHeader(0) = %q, want empty — there is nothing to resume from", got)
	}
	if got := RangeHeader(-1); got != "" {
		t.Errorf("RangeHeader(-1) = %q, want empty", got)
	}
	if got := RangeHeader(1000); got != "bytes=1000-" {
		t.Errorf("RangeHeader(1000) = %q", got)
	}
}

func TestETASeconds(t *testing.T) {
	// Unknown rate means NO estimate, not zero: "0 s remaining" on a download that has not started
	// is a lie the user can see.
	if _, ok := ETASeconds(0, 1000, 0); ok {
		t.Error("an unknown rate must not produce an estimate")
	}
	if _, ok := ETASeconds(0, 0, 100); ok {
		t.Error("an unknown total must not produce an estimate")
	}
	if got, ok := ETASeconds(400, 1000, 100); !ok || got != 6 {
		t.Errorf("ETASeconds = %d (ok=%v), want 6", got, ok)
	}
}
