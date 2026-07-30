package openai

import (
	"encoding/binary"
	"math"
	"testing"
)

func pcmOf(samples ...int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
	return out
}

func samplesOf(pcm []byte) []int16 {
	out := make([]int16, len(pcm)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}
	return out
}

// The ratio this provider actually needs: 16 kHz in, 24 kHz out, so 3 samples out for every 2 in.
// Getting the LENGTH wrong is the silent failure — the service plays the audio at the wrong speed.
func TestResampleStretchesSixteenToTwentyFour(t *testing.T) {
	in := pcmOf(0, 100, 200, 300, 400, 500, 600, 700)
	out := Resample(in, 16000, 24000)
	wantSamples := len(samplesOf(in)) * 3 / 2
	if got := len(samplesOf(out)); got != wantSamples {
		t.Fatalf("%d muestras -> %d, quería %d (24/16 = 1.5×)", len(samplesOf(in)), got, wantSamples)
	}
}

// The same rate must be a no-op, and cheap: resampling 16 to 16 would only add rounding error.
func TestResampleIsAPassThroughAtTheSameRate(t *testing.T) {
	in := pcmOf(1, -1, 32767, -32768)
	out := Resample(in, 16000, 16000)
	if string(out) != string(in) {
		t.Errorf("16k->16k cambió los datos: %v -> %v", samplesOf(in), samplesOf(out))
	}
}

// Downwards too, so the function is not silently one-directional like the original's.
func TestResampleAlsoGoesDown(t *testing.T) {
	in := pcmOf(0, 100, 200, 300, 400, 500)
	out := Resample(in, 24000, 16000)
	if got, want := len(samplesOf(out)), 4; got != want {
		t.Errorf("6 muestras a 24k->16k dan %d, quería %d", got, want)
	}
}

// A ramp must come out a ramp: interpolation that reversed or repeated samples would still produce the
// right LENGTH, which is why length alone is not enough of a test.
func TestResamplePreservesTheShapeOfARamp(t *testing.T) {
	var in []int16
	for i := 0; i < 32; i++ {
		in = append(in, int16(i*1000))
	}
	out := samplesOf(Resample(pcmOf(in...), 16000, 24000))
	for i := 1; i < len(out); i++ {
		if out[i] < out[i-1] {
			t.Fatalf("la rampa dejó de ser monótona en %d: %v", i, out[:i+1])
		}
	}
	// And it spans the same range, rather than compressing towards zero.
	if out[0] != in[0] {
		t.Errorf("la primera muestra = %d, quería %d", out[0], in[0])
	}
	if last := out[len(out)-1]; last < in[len(in)-2] {
		t.Errorf("la última muestra = %d, se quedó por debajo del final de la rampa (%d)", last, in[len(in)-1])
	}
}

// A sine must survive as a sine of the SAME frequency in Hz. This is the test that would catch an
// off-by-one in the ratio: the length can be right while the pitch is wrong.
func TestResampleKeepsThePitchOfASine(t *testing.T) {
	const (
		inRate  = 16000
		outRate = 24000
		freq    = 500.0
		samples = 1600 // 0.1 s
	)
	in := make([]int16, samples)
	for i := range in {
		in[i] = int16(20000 * math.Sin(2*math.Pi*freq*float64(i)/float64(inRate)))
	}
	out := samplesOf(Resample(pcmOf(in...), inRate, outRate))

	// Count zero crossings: for a clean sine that is 2 per period, so the frequency in Hz is
	// crossings / 2 / duration. Duration is the same 0.1 s, so the count must be the same too.
	crossings := func(s []int16) int {
		n := 0
		for i := 1; i < len(s); i++ {
			if (s[i-1] < 0) != (s[i] < 0) {
				n++
			}
		}
		return n
	}
	inC, outC := crossings(in), crossings(out)
	if diff := inC - outC; diff > 2 || diff < -2 {
		t.Errorf("cruces por cero: %d -> %d. La duración no cambia, así que un conteo distinto significa que el tono cambió", inC, outC)
	}
}

// No output sample may fall outside the range of the input.
//
// This replaced a test that asserted full-scale input stays full-scale, which was simply WRONG:
// interpolating across the step from +32767 to -32768 must produce intermediate values, and it flagged
// the correct answer as a defect. Writing it also showed that the clamp in Resample is unreachable by
// this algorithm — linear interpolation is a convex combination, so it never leaves the interval its
// endpoints span. What IS worth pinning is exactly that: an interpolation that extrapolated past the
// endpoints, or that wrapped, would break this and nothing else in the suite would notice.
func TestResampleNeverLeavesTheRangeOfTheInput(t *testing.T) {
	in := []int16{32767, 32767, -32768, -32768, 0, 12345, -9999, 32767}
	lo, hi := in[0], in[0]
	for _, s := range in {
		if s < lo {
			lo = s
		}
		if s > hi {
			hi = s
		}
	}
	for _, rates := range [][2]int{{16000, 24000}, {24000, 16000}, {16000, 48000}} {
		for i, s := range samplesOf(Resample(pcmOf(in...), rates[0], rates[1])) {
			if s < lo || s > hi {
				t.Errorf("%d->%d muestra %d = %d, fuera de [%d, %d]", rates[0], rates[1], i, s, lo, hi)
			}
		}
	}
}

// Degenerate input must not panic and must not invent audio.
func TestResampleHandlesEmptyAndOddInput(t *testing.T) {
	if got := Resample(nil, 16000, 24000); len(got) != 0 {
		t.Errorf("nil -> %v", got)
	}
	if got := Resample([]byte{1}, 16000, 24000); len(got) != 1 {
		// A lone byte is not a sample; it comes back untouched rather than being padded into one.
		t.Errorf("un byte suelto -> %v", got)
	}
	// An odd trailing byte is dropped, not turned into half a sample.
	out := Resample(pcmOf(100, 200), 16000, 24000)
	odd := Resample(append(pcmOf(100, 200), 0x7f), 16000, 24000)
	if len(odd) != len(out) {
		t.Errorf("el byte impar cambió la salida: %d vs %d bytes", len(odd), len(out))
	}
	if got := Resample(pcmOf(1, 2), 0, 24000); len(got) != 4 {
		t.Errorf("una tasa de entrada inválida debería devolver la entrada intacta, dio %v", got)
	}
}

// Interpolation must actually INTERPOLATE, with the arithmetic pinned to exact values.
//
// A zero-order hold (repeat the nearest sample) passes every other test in this file: it keeps the
// length, stays monotonic on a ramp and spans the same range. It is audibly worse — a stair instead of
// a line — and a mutation that dropped the interpolation survived until this test existed.
func TestResampleActuallyInterpolatesBetweenSamples(t *testing.T) {
	// 16 -> 24 kHz reads at 2/3 of a sample per step: positions 0, 0.667, 1.333, 2, ...
	in := pcmOf(0, 300, 600, 900)
	out := samplesOf(Resample(in, 16000, 24000))
	want := []int16{0, 200, 400, 600, 800, 900} // 0.667*300, 1.333*300 ... last clamps at the end
	if len(out) != len(want) {
		t.Fatalf("salida = %v, quería %d muestras", out, len(want))
	}
	for i := range want {
		if diff := int(out[i]) - int(want[i]); diff > 1 || diff < -1 {
			t.Errorf("muestra %d = %d, quería ~%d (una repetición daría %d)", i, out[i], want[i], in[i*2/3])
		}
	}
}

// The pitch test above counts crossings over the WHOLE buffer, and that is not enough: with the ratio
// inverted the resampler consumes the input 1.5× too fast, flatlines for the rest, and ends with the
// same total. So the crossings must also be spread evenly — half of them in each half of the output.
func TestResampleSpreadsTheSignalOverTheWholeOutput(t *testing.T) {
	const inRate, outRate, freq, samples = 16000, 24000, 500.0, 1600
	in := make([]int16, samples)
	for i := range in {
		in[i] = int16(20000 * math.Sin(2*math.Pi*freq*float64(i)/float64(inRate)))
	}
	out := samplesOf(Resample(pcmOf(in...), inRate, outRate))

	crossings := func(s []int16) int {
		n := 0
		for i := 1; i < len(s); i++ {
			if (s[i-1] < 0) != (s[i] < 0) {
				n++
			}
		}
		return n
	}
	half := len(out) / 2
	first, second := crossings(out[:half]), crossings(out[half:])
	if second == 0 {
		t.Fatalf("la segunda mitad de la salida no tiene señal (%d cruces en la primera): el audio se consumió demasiado rápido y el resto es una línea plana", first)
	}
	// Within a crossing or two of each other for a constant tone.
	if diff := first - second; diff > 2 || diff < -2 {
		t.Errorf("cruces desequilibrados: %d en la primera mitad, %d en la segunda", first, second)
	}
}
