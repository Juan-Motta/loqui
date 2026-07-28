// Pure DSP for the capture path. Converts the float32 frames a microphone produces
// into the 16 kHz, 16-bit, mono, little-endian PCM every Loqui provider consumes —
// Azure's push stream, the Grok/ElevenLabs WebSockets and the native helpers all want
// the same bytes.
//
// Ported from the Electron build's src/shared/audioPcm.ts, including its tests. It was
// the riskiest part of that capture pipeline and it is more load-bearing here: in the
// port this is the ONLY audio path, because Go now captures for every provider instead
// of three renderer-side pipelines each doing their own conversion.
package audio

import "math"

// Downsample resamples by linear interpolation. Loqui only ever downsamples (mics run
// at 44.1/48 kHz, the target is 16 kHz); equal rates pass the input straight through.
//
// Upsampling is rejected rather than approximated: asking for it means the caller got
// the device rate wrong, and quietly inventing samples would turn that mistake into a
// transcription-quality bug nobody could trace back here.
func Downsample(input []float32, inputRate, outputRate int) ([]float32, error) {
	if outputRate == inputRate {
		return input, nil
	}
	if outputRate > inputRate {
		return nil, ErrUpsample
	}
	ratio := float64(inputRate) / float64(outputRate)
	outLen := int(float64(len(input)) / ratio)
	out := make([]float32, outLen)
	for i := 0; i < outLen; i++ {
		pos := float64(i) * ratio
		idx := int(pos)
		frac := float32(pos - float64(idx))
		a := input[idx]
		b := a
		if idx+1 < len(input) {
			b = input[idx+1]
		}
		out[i] = a + (b-a)*frac
	}
	return out, nil
}

// FloatTo16BitPCM maps float32 samples in [-1, 1] to int16, clamping out-of-range
// input. Full scale is asymmetric — -32768 negative, +32767 positive — because signed
// 16-bit is: using 32768 for both would wrap the loudest positive peaks to negative,
// which is heard as a click on exactly the syllables the user shouted.
func FloatTo16BitPCM(input []float32) []int16 {
	out := make([]int16, len(input))
	for i, v := range input {
		s := float64(v)
		s = math.Max(-1, math.Min(1, s))
		if s < 0 {
			out[i] = int16(s * 0x8000)
		} else {
			out[i] = int16(s * 0x7fff)
		}
	}
	return out
}

// Int16ToLEBytes serialises samples as little-endian 16-bit, the wire format of every
// provider in Loqui.
func Int16ToLEBytes(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		u := uint16(s)
		out[i*2] = byte(u)
		out[i*2+1] = byte(u >> 8)
	}
	return out
}

// ToPCM16LE is the whole conversion in one call: the shape every capture callback
// actually needs.
func ToPCM16LE(input []float32, inputRate, outputRate int) ([]byte, error) {
	ds, err := Downsample(input, inputRate, outputRate)
	if err != nil {
		return nil, err
	}
	return Int16ToLEBytes(FloatTo16BitPCM(ds)), nil
}

// Level reports a 0..1 loudness for the overlay's bars and the Home waveform: RMS with
// the same gain the Electron meter applied (×4), so speech reaches a lively range
// instead of hovering near zero.
//
// It replaces an AnalyserNode in a hidden renderer window. Computing it here means the
// meter reacts to the actual microphone for EVERY provider, including the local helpers
// that capture in their own process — which is why Electron needed that window at all.
func Level(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, v := range samples {
		sum += float64(v) * float64(v)
	}
	rms := math.Sqrt(sum / float64(len(samples)))
	return math.Min(1, rms*4)
}

// LevelPCM16 is Level over already-converted little-endian PCM16 — the form the capture
// callback receives, since miniaudio does the conversion for us (see capture.go).
// An odd trailing byte is ignored rather than treated as a sample.
func LevelPCM16(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		s := float64(int16(uint16(pcm[i*2])|uint16(pcm[i*2+1])<<8)) / 32768
		sum += s * s
	}
	rms := math.Sqrt(sum / float64(n))
	return math.Min(1, rms*4)
}
