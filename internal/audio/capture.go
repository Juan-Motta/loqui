// Microphone capture. Replaces the Electron build's hidden `engine` renderer, which
// owned the only getUserMedia grant and ran a ScriptProcessorNode per provider.
//
// ONE CAPTURE, EVERY PROVIDER. Electron had three separate audio paths: the Azure JS SDK
// opened the device itself, Grok/ElevenLabs went through a ScriptProcessor doing its own
// conversion, and the local helpers opened the device in their own process. Here there
// is a single device, delivering identical frames to whoever is listening.
//
// WHY MINIAUDIO DOES THE RESAMPLING. The device is asked for 16 kHz, 16-bit, mono and
// miniaudio's own converter delivers exactly that whatever the hardware runs at. This is
// the same arrangement Electron had — it constructed `new AudioContext({sampleRate:
// 16000})` and let Chromium resample — and it is deliberately NOT the naive linear
// interpolation in pcm.go: that path was almost never exercised in Electron precisely
// because Chromium honoured the requested rate, so leaning on it here would be trusting
// code that production never really ran.
package audio

import (
	"sync"

	"github.com/gen2brain/malgo"
)

// Format every Loqui provider consumes.
const (
	CaptureSampleRate = 16000
	CaptureChannels   = 1
)

// Frames are handed over as PCM16 little-endian, plus a 0..1 level for the meter.
type Frame struct {
	PCM   []byte
	Level float64
}

// Capture is a running microphone session.
type Capture struct {
	ctx    *malgo.AllocatedContext
	device *malgo.Device

	mu      sync.Mutex
	stopped bool

	frames chan Frame
}

// StartCapture opens the default input device (or deviceID when given) and streams
// frames. Close the returned Capture to stop.
//
// deviceID is the miniaudio device id as a string, matching what ListInputDevices
// reports; an unknown or empty id falls back to the system default rather than failing,
// because a microphone that was unplugged since the setting was saved must not stop the
// user from dictating.
func StartCapture(deviceID string, onLog func(string)) (*Capture, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(msg string) {
		if onLog != nil {
			onLog(msg)
		}
	})
	if err != nil {
		return nil, err
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = CaptureChannels
	cfg.SampleRate = CaptureSampleRate
	if id, ok := lookupDeviceID(ctx, deviceID); ok {
		cfg.Capture.DeviceID = id.Pointer()
	}

	c := &Capture{
		ctx: ctx,
		// Buffered so a slow consumer never stalls the audio thread. ~100 frames is a
		// couple of seconds of speech; past that we drop, because blocking here would
		// glitch the capture itself and a dropped frame costs less than a stutter.
		frames: make(chan Frame, 100),
	}

	device, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_, in []byte, frameCount uint32) {
			if frameCount == 0 || len(in) == 0 {
				return
			}
			// The slice is owned by miniaudio and reused on the next callback, so it
			// MUST be copied before it leaves this function. Passing it on directly
			// produces audio that is subtly corrupted in a way that looks like a bad
			// microphone rather than a bug.
			pcm := make([]byte, len(in))
			copy(pcm, in)
			select {
			case c.frames <- Frame{PCM: pcm, Level: LevelPCM16(pcm)}:
			default: // consumer behind — drop rather than block the audio thread
			}
		},
	})
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return nil, err
	}
	c.device = device

	if err := device.Start(); err != nil {
		device.Uninit()
		_ = ctx.Uninit()
		ctx.Free()
		return nil, err
	}
	return c, nil
}

// Frames is the stream of captured audio. Closed when the capture stops.
func (c *Capture) Frames() <-chan Frame { return c.frames }

// Close stops the device and releases the native context. Safe to call twice.
func (c *Capture) Close() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	device, ctx := c.device, c.ctx
	c.device, c.ctx = nil, nil
	c.mu.Unlock()

	// Uninit stops the device and waits for the audio thread, so no callback can be
	// running by the time the channel is closed.
	if device != nil {
		device.Uninit()
	}
	if ctx != nil {
		_ = ctx.Uninit()
		ctx.Free()
	}
	close(c.frames)
}

// InputDevice is a selectable microphone, for the Ajustes picker.
type InputDevice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// ListInputDevices enumerates capture devices.
func ListInputDevices() ([]InputDevice, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	out := make([]InputDevice, 0, len(infos))
	for _, info := range infos {
		out = append(out, InputDevice{
			ID:      info.ID.String(),
			Name:    info.Name(),
			Default: info.IsDefault != 0,
		})
	}
	return out, nil
}

// lookupDeviceID resolves a stored device id back to a live device. Returns false when
// the id is empty or no longer present, which means "use the default".
func lookupDeviceID(ctx *malgo.AllocatedContext, want string) (malgo.DeviceID, bool) {
	var zero malgo.DeviceID
	if want == "" {
		return zero, false
	}
	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return zero, false
	}
	for _, info := range infos {
		if info.ID.String() == want {
			return info.ID, true
		}
	}
	return zero, false
}
