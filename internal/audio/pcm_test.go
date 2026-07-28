// Ported from the Electron suite (test/unit/audioPcm.test.ts) case for case, so a
// regression in the port shows up against the same expectations the original held.
package audio

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestDownsampleEqualRatesReturnsInput(t *testing.T) {
	input := []float32{0.1, 0.2, 0.3}
	out, err := Downsample(input, 16000, 16000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if &out[0] != &input[0] {
		t.Error("expected the input slice itself, not a copy")
	}
}

func TestDownsampleShortensByRatio(t *testing.T) {
	out, err := Downsample(make([]float32, 300), 48000, 16000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 100 {
		t.Errorf("48k->16k of 300 samples: got %d, want 100", len(out))
	}
}

func TestDownsampleInterpolates(t *testing.T) {
	out, err := Downsample([]float32{0, 1, 2, 3}, 2, 1) // ratio 2 -> indices 0, 2
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(out, []float32{0, 2}) {
		t.Errorf("got %v, want [0 2]", out)
	}
}

func TestDownsampleRejectsUpsampling(t *testing.T) {
	_, err := Downsample(make([]float32, 10), 16000, 48000)
	if !errors.Is(err, ErrUpsample) {
		t.Errorf("got %v, want ErrUpsample", err)
	}
}

func TestFloatTo16BitPCMFullScale(t *testing.T) {
	out := FloatTo16BitPCM([]float32{0, 1, -1})
	want := []int16{0, 32767, -32768}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

func TestFloatTo16BitPCMClamps(t *testing.T) {
	out := FloatTo16BitPCM([]float32{2, -2})
	want := []int16{32767, -32768}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

func TestInt16ToLEBytes(t *testing.T) {
	// 1 = 0x0001 -> [01,00]; -1 = 0xFFFF -> [FF,FF]; 256 = 0x0100 -> [00,01]
	got := Int16ToLEBytes([]int16{1, -1, 256})
	want := []byte{0x01, 0x00, 0xff, 0xff, 0x00, 0x01}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got % x, want % x", got, want)
	}
}

func TestInt16ToLEBytesTwoBytesPerSample(t *testing.T) {
	if n := len(Int16ToLEBytes([]int16{0, 0, 0})); n != 6 {
		t.Errorf("got %d bytes, want 6", n)
	}
}

func TestToPCM16LEEndToEnd(t *testing.T) {
	// 4 samples at 2x the target rate -> 2 samples -> 4 bytes.
	got, err := ToPCM16LE([]float32{0, 1, -1, 0}, 32000, 16000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d bytes, want 4", len(got))
	}
	// Downsample takes indices 0 and 2: 0 and -1 -> 0x0000, 0x8000 LE.
	want := []byte{0x00, 0x00, 0x00, 0x80}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got % x, want % x", got, want)
	}
}

func TestToPCM16LEPropagatesUpsampleError(t *testing.T) {
	if _, err := ToPCM16LE(make([]float32, 4), 16000, 48000); !errors.Is(err, ErrUpsample) {
		t.Errorf("got %v, want ErrUpsample", err)
	}
}

func TestLevelSilenceIsZero(t *testing.T) {
	if got := Level(make([]float32, 128)); got != 0 {
		t.Errorf("silence: got %v, want 0", got)
	}
}

func TestLevelEmptyIsZero(t *testing.T) {
	if got := Level(nil); got != 0 {
		t.Errorf("empty: got %v, want 0", got)
	}
}

func TestLevelIsClampedToOne(t *testing.T) {
	loud := make([]float32, 64)
	for i := range loud {
		loud[i] = 1
	}
	if got := Level(loud); got != 1 {
		t.Errorf("full scale: got %v, want 1 (clamped)", got)
	}
}

func TestLevelAppliesGain(t *testing.T) {
	// Constant 0.1 -> RMS 0.1 -> x4 gain -> 0.4. The gain is what makes normal
	// speech move the bars instead of sitting at the bottom.
	quiet := make([]float32, 64)
	for i := range quiet {
		quiet[i] = 0.1
	}
	if got := Level(quiet); math.Abs(got-0.4) > 1e-6 {
		t.Errorf("got %v, want ~0.4", got)
	}
}

func TestLevelPCM16MatchesLevelOverTheSameSignal(t *testing.T) {
	// The overlay bars are driven by LevelPCM16 while some tests and the pre-roll path
	// use Level; the two must agree, or the meter would jump when the source changes.
	samples := []float32{0, 0.5, -0.5, 0.25, -0.25, 1, -1, 0.1}
	pcm := Int16ToLEBytes(FloatTo16BitPCM(samples))
	a, b := Level(samples), LevelPCM16(pcm)
	if math.Abs(a-b) > 1e-3 {
		t.Errorf("Level=%v LevelPCM16=%v — should agree within quantisation error", a, b)
	}
}

func TestLevelPCM16EmptyAndOddLength(t *testing.T) {
	if got := LevelPCM16(nil); got != 0 {
		t.Errorf("empty: got %v, want 0", got)
	}
	// A trailing odd byte is not a sample; it must be ignored, not read past.
	if got := LevelPCM16([]byte{0x00}); got != 0 {
		t.Errorf("single byte: got %v, want 0", got)
	}
}

func TestLevelPCM16FullScaleIsClamped(t *testing.T) {
	loud := make([]int16, 64)
	for i := range loud {
		loud[i] = 32767
	}
	if got := LevelPCM16(Int16ToLEBytes(loud)); got != 1 {
		t.Errorf("full scale: got %v, want 1", got)
	}
}
