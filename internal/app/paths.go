package app

import (
	"os"
	"path/filepath"

	"github.com/Juan-Motta/loqui-go/internal/store"
)

// HelperPath locates a compiled native helper.
//
// Where bundled assets live differs between development and a packaged app, and getting it
// wrong is invisible until someone runs the DMG. Packaged helper executables sit in
// Contents/Helpers. In development they are in helpers/bin, which is where the build scripts put
// them.
//
// Returns "" when the helper isn't there, so the caller can say which build command produces
// it rather than failing with a bare "file not found".
func existingPath(path string) string {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

func helperPath(executablePath, workingDir, name string) string {
	if executablePath != "" {
		bundled := filepath.Clean(filepath.Join(filepath.Dir(executablePath), "..", "Helpers", name))
		if found := existingPath(bundled); found != "" {
			return found
		}
	}
	return existingPath(filepath.Join(workingDir, "helpers", "bin", name))
}

func HelperPath(name string) string {
	executablePath, _ := os.Executable()
	workingDir, _ := os.Getwd()
	return helperPath(executablePath, workingDir, name)
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
// Order matters: a packaged copy lives under Contents/Resources/models, a development copy lives
// under helpers/bin, and the normal first-use download belongs under the app data directory.
//
// The 465 MB model is not normally shipped; it is downloaded to the data directory on first
// use. A copy beside the built helper is legitimate in development, so a dev machine never
// waits on a download.
func whisperModelPath(executablePath, workingDir, dataDir string) string {
	if executablePath != "" {
		bundled := filepath.Clean(filepath.Join(filepath.Dir(executablePath), "..", "Resources", "models", "ggml-small.bin"))
		if found := existingPath(bundled); found != "" {
			return found
		}
	}
	if found := existingPath(filepath.Join(workingDir, "helpers", "bin", "ggml-small.bin")); found != "" {
		return found
	}
	return filepath.Join(dataDir, "models", "ggml-small.bin")
}

func WhisperModelPath(dataDir string) string {
	executablePath, _ := os.Executable()
	workingDir, _ := os.Getwd()
	return whisperModelPath(executablePath, workingDir, dataDir)
}
