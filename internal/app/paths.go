package app

import (
	"os"
	"path/filepath"

	"github.com/Juan-Motta/loqui-go/internal/store"
)

// HelperPath locates a compiled native helper.
//
// Where bundled assets live differs between development and a packaged app, and getting it
// wrong is invisible until someone runs the DMG. Packaged, the helpers sit in
// Contents/Resources/helpers — they MUST be outside any archive, because macOS cannot
// execute a binary that isn't a real file on disk. In development they are in helpers/bin,
// which is where the build scripts put them.
//
// Returns "" when the helper isn't there, so the caller can say which build command produces
// it rather than failing with a bare "file not found".
func HelperPath(name string) string {
	if exe, err := os.Executable(); err == nil {
		// bin/loqui.app/Contents/MacOS/loqui -> Contents/Resources/helpers/<name>
		bundled := filepath.Join(filepath.Dir(exe), "..", "Resources", "helpers", name)
		if _, err := os.Stat(bundled); err == nil {
			return bundled
		}
	}
	dev := filepath.Join("helpers", "bin", name)
	if _, err := os.Stat(dev); err == nil {
		return dev
	}
	return ""
}

// WhisperModelBytes is the exact size of ggml-small.bin.
//
// NOW A REFERENCE, not a second copy: the spec — size AND digest — lives in store.WhisperModel, put
// there when the model row was ported. Two copies of a pinned byte count is exactly the kind of
// duplication that drifts silently, and the comment that used to live here promised the digest would
// arrive with that row. It has.
var WhisperModelBytes = store.WhisperModel.Bytes

// WhisperModelPath is where the whisper model lives.
//
// Order matters, and the first entry is the one that was missing: a copy INSIDE the bundle,
// found the same way the helpers are. Checking only the relative dev path made a packaged app
// report "falta el modelo" while the file sat right next to the helper — because an app
// launched from Finder has its working directory at /, so a relative path resolves nowhere.
//
// The 465 MB model is not normally shipped; it is downloaded to the data directory on first
// use. A copy beside the built helper is legitimate in development, so a dev machine never
// waits on a download.
func WhisperModelPath(dataDir string) string {
	if bundled := HelperPath("ggml-small.bin"); bundled != "" {
		return bundled
	}
	return filepath.Join(dataDir, "models", "ggml-small.bin")
}
