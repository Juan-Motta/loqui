package openai

import "encoding/binary"

// Rate conversion for this provider's audio.
//
// WHY THIS EXISTS AND WHY IT IS NOT A PORT. The app captures 16 kHz mono PCM16 — one pipeline shared by
// every provider (internal/audio) — and a realtime session is declared at 24 kHz. The Electron build
// never had this problem: its audio came from a browser AudioContext running at the device rate (often
// 48 kHz) and it only ever went DOWN, so shared/audioPcm.ts's downsample explicitly refuses
// outputRate > inputRate. Going 16 → 24 is the opposite direction and had to be written.
//
// IT MATTERS BECAUSE THE FAILURE IS SILENT. Sending 16 kHz samples to a session declared at 24 kHz is
// not rejected: the service plays them 1.5× too fast and transcribes a chirpy, hurried voice, badly.
// An error would have been kinder.
//
// LINEAR INTERPOLATION, same as the original's, and deliberately not something fancier. A proper
// band-limited resampler would be better for music; for speech headed to a transcriber the difference
// is inaudible against the codec and the model, and a hand-rolled FIR is a much better place for a bug.

// Resample converts little-endian mono PCM16 from one sample rate to another.
//
// Operates on BYTES because that is what the capture pipeline hands over and what the wire takes;
// converting to []int16 and back at every boundary would be two extra copies per audio chunk.
//
// An odd trailing byte is dropped: a half sample cannot be interpolated, and carrying it into the next
// chunk would mean holding state here — this function stays pure so it can be tested exhaustively.
func Resample(pcm []byte, fromRate, toRate int) []byte {
	if fromRate <= 0 || toRate <= 0 || fromRate == toRate || len(pcm) < 2 {
		return pcm
	}
	in := len(pcm) / 2 // whole samples only
	if in == 0 {
		return nil
	}
	out := in * toRate / fromRate
	if out <= 0 {
		return nil
	}

	sample := func(i int) float64 {
		if i < 0 {
			i = 0
		}
		if i >= in {
			i = in - 1
		}
		return float64(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
	}

	res := make([]byte, out*2)
	ratio := float64(fromRate) / float64(toRate)
	for i := 0; i < out; i++ {
		pos := float64(i) * ratio
		idx := int(pos)
		frac := pos - float64(idx)
		a, b := sample(idx), sample(idx+1)
		v := a + (b-a)*frac
		// Clamp before the cast. DEFENSIVE ONLY, and worth saying so: linear interpolation is a convex
		// combination of two in-range samples, so it cannot leave the int16 range — writing the test
		// for this is what made that clear. It stays because the cast would WRAP rather than saturate
		// if the interpolation ever changed, turning a loud sample into an equally loud one of the
		// opposite sign, which is an audible click rather than a rounding error.
		if v > 32767 {
			v = 32767
		}
		if v < -32768 {
			v = -32768
		}
		binary.LittleEndian.PutUint16(res[i*2:], uint16(int16(v)))
	}
	return res
}
