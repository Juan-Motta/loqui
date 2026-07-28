package app

import (
	"os"
	"path/filepath"
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

// WhisperModelPath is where the whisper model lives.
//
// The 465 MB model is NOT shipped in the app — it is downloaded to the data directory on
// first use. A copy sitting next to the built helper is legitimate in development (the build
// script fetches one) and is preferred, so a dev machine never waits on a download.
func WhisperModelPath(dataDir string) string {
	dev := filepath.Join("helpers", "bin", "ggml-small.bin")
	if _, err := os.Stat(dev); err == nil {
		return dev
	}
	return filepath.Join(dataDir, "models", "ggml-small.bin")
}
