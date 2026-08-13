package app

import (
	"context"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func TestUpdateAssetMatcherSelectsOnlyTheAppleSiliconZip(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "Loqui-0.3.0-macos-arm64.dmg"},
		{Name: "Loqui-0.3.0-macos-arm64.dmg.sha256"},
		{Name: "SHA256SUMS"},
		{Name: "Loqui-0.3.0-macos-x86_64.zip"},
		{Name: "Loqui-0.3.0-macos-arm64.zip"},
	}
	got := UpdateAssetMatcher(updater.CheckRequest{Platform: "darwin", Arch: "arm64"}, assets)
	if got != 4 {
		t.Fatalf("matcher selected index %d, want Apple Silicon ZIP at index 4", got)
	}
}

func TestUpdateAssetMatcherRejectsWrongPlatformArchitectureAndFormat(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "Loqui-0.3.0-macos-arm64.dmg"},
		{Name: "Loqui-0.3.0-macos-arm64.zip"},
	}
	for _, request := range []updater.CheckRequest{
		{Platform: "windows", Arch: "arm64"},
		{Platform: "darwin", Arch: "amd64"},
	} {
		if got := UpdateAssetMatcher(request, assets); got != -1 {
			t.Errorf("matcher for %+v selected index %d, want -1", request, got)
		}
	}
	if got := UpdateAssetMatcher(updater.CheckRequest{Platform: "darwin", Arch: "arm64"}, assets[:1]); got != -1 {
		t.Fatalf("matcher selected DMG index %d", got)
	}
}

func TestWailsUpdateBackendMapsReleaseAndDelegatesOperations(t *testing.T) {
	provider := &fakeWailsProvider{
		release: &updater.Release{
			Version: "0.3.0",
			Name:    "Loqui 0.3.0",
			Notes:   "notes",
			Artifact: updater.Artifact{
				Filename: "Loqui-0.3.0-macos-arm64.zip",
			},
		},
	}
	backend := newWailsUpdateBackend(provider, "0.2.0")
	release, err := backend.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if release.Version != "0.3.0" || release.Artifact != "Loqui-0.3.0-macos-arm64.zip" {
		t.Fatalf("mapped release = %+v", release)
	}
	if backend.CurrentVersion() != "0.2.0" {
		t.Fatalf("CurrentVersion = %q", backend.CurrentVersion())
	}
	if err := backend.DownloadAndInstall(context.Background()); err != nil {
		t.Fatalf("DownloadAndInstall: %v", err)
	}
	if err := backend.Restart(context.Background()); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if provider.downloads != 1 || provider.restarts != 1 {
		t.Fatalf("provider calls downloads=%d restarts=%d", provider.downloads, provider.restarts)
	}
}

type fakeWailsProvider struct {
	release   *updater.Release
	downloads int
	restarts  int
}

func (p *fakeWailsProvider) CurrentVersion() string { return "0.2.0" }

func (p *fakeWailsProvider) Check(context.Context) (*updater.Release, error) {
	return p.release, nil
}

func (p *fakeWailsProvider) DownloadAndInstall(context.Context) error {
	p.downloads++
	return nil
}

func (p *fakeWailsProvider) Restart(context.Context) error {
	p.restarts++
	return nil
}
