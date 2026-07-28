package audio

import "errors"

// ErrUpsample is returned by Downsample when asked to raise the sample rate. See the
// note there for why this is an error rather than a best effort.
var ErrUpsample = errors.New("audio: outputRate must be <= inputRate")
