package app

import (
	"os"
	"path/filepath"
	"testing"
)

func putPathFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestHelperPathPrefersContentsHelpers(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "Loqui.app", "Contents", "MacOS", "loqui")
	want := filepath.Join(root, "Loqui.app", "Contents", "Helpers", "globe-listener")
	putPathFile(t, want)
	putPathFile(t, filepath.Join(root, "Loqui.app", "Contents", "Resources", "helpers", "globe-listener"))

	if got := helperPath(executable, t.TempDir(), "globe-listener"); got != want {
		t.Fatalf("helperPath() = %q, want %q", got, want)
	}
}

func TestHelperPathUsesDevelopmentFallback(t *testing.T) {
	working := t.TempDir()
	want := filepath.Join(working, "helpers", "bin", "macos-stt")
	putPathFile(t, want)

	if got := helperPath("", working, "macos-stt"); got != want {
		t.Fatalf("helperPath() = %q, want %q", got, want)
	}
}

func TestHelperPathRejectsLegacyAndMissingFiles(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "Loqui.app", "Contents", "MacOS", "loqui")
	putPathFile(t, filepath.Join(root, "Loqui.app", "Contents", "Resources", "helpers", "whisper-stt"))

	if got := helperPath(executable, t.TempDir(), "whisper-stt"); got != "" {
		t.Fatalf("legacy helper resolved as %q", got)
	}
}

func TestWhisperModelPathUsesBundledResourcesThenDevelopmentThenData(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "Loqui.app", "Contents", "MacOS", "loqui")
	working := t.TempDir()
	data := t.TempDir()
	bundled := filepath.Join(root, "Loqui.app", "Contents", "Resources", "models", "ggml-small.bin")
	development := filepath.Join(working, "helpers", "bin", "ggml-small.bin")
	putPathFile(t, development)
	putPathFile(t, bundled)

	if got := whisperModelPath(executable, working, data); got != bundled {
		t.Fatalf("bundled model = %q, want %q", got, bundled)
	}
	if err := os.Remove(bundled); err != nil {
		t.Fatal(err)
	}
	if got := whisperModelPath(executable, working, data); got != development {
		t.Fatalf("development model = %q, want %q", got, development)
	}
	if err := os.Remove(development); err != nil {
		t.Fatal(err)
	}
	wantData := filepath.Join(data, "models", "ggml-small.bin")
	if got := whisperModelPath(executable, working, data); got != wantData {
		t.Fatalf("data model = %q, want %q", got, wantData)
	}
}
