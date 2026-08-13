package app

import (
	"context"
	"regexp"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

var updateZIPName = regexp.MustCompile(`^Loqui-[0-9]+\.[0-9]+\.[0-9]+-macos-arm64\.zip$`)

// UpdateAssetMatcher prevents the GitHub provider from choosing the manual DMG when both release
// formats are present. It is intentionally strict about platform, architecture, and extension.
func UpdateAssetMatcher(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	if req.Platform != "darwin" || req.Arch != "arm64" {
		return -1
	}
	for i, asset := range assets {
		if updateZIPName.MatchString(asset.Name) && !strings.Contains(asset.Name, "/") {
			return i
		}
	}
	return -1
}

// NewGitHubUpdateProvider creates the public-repository provider used by packaged builds.
func NewGitHubUpdateProvider() (updater.Provider, error) {
	return github.New(github.Config{
		Repository:    "Juan-Motta/loqui",
		ChecksumAsset: "SHA256SUMS",
		AssetMatcher:  UpdateAssetMatcher,
	})
}

// ConfigureWailsUpdater initializes the framework updater with headless mode. Loqui owns its
// localized About UI, so the built-in English window is deliberately not used.
func ConfigureWailsUpdater(u *updater.Updater, currentVersion string) error {
	if u == nil || currentVersion == "" {
		return nil
	}
	provider, err := NewGitHubUpdateProvider()
	if err != nil {
		return err
	}
	return u.Init(updater.Config{
		CurrentVersion: currentVersion,
		Providers:      []updater.Provider{provider},
		Window:         updater.WindowNone,
	})
}

// wailsUpdateClient is exactly the subset of *updater.Updater used by the adapter. Keeping this
// interface small makes service tests independent of Wails' host process and window machinery.
type wailsUpdateClient interface {
	Check(context.Context) (*updater.Release, error)
	DownloadAndInstall(context.Context) error
	Restart(context.Context) error
}

type wailsUpdateBackend struct {
	client  wailsUpdateClient
	version string
}

func newWailsUpdateBackend(client wailsUpdateClient, version string) *wailsUpdateBackend {
	return &wailsUpdateBackend{client: client, version: version}
}

// NewWailsUpdateBackend adapts the framework updater for the app-owned service.
func NewWailsUpdateBackend(client interface {
	Check(context.Context) (*updater.Release, error)
	DownloadAndInstall(context.Context) error
	Restart(context.Context) error
}, version string) UpdateBackend {
	return newWailsUpdateBackend(client, version)
}

func (b *wailsUpdateBackend) CurrentVersion() string { return b.version }

func (b *wailsUpdateBackend) Check(ctx context.Context) (*UpdateRelease, error) {
	release, err := b.client.Check(ctx)
	if err != nil || release == nil {
		return nil, err
	}
	return &UpdateRelease{
		Version:  release.Version,
		Name:     release.Name,
		Notes:    release.Notes,
		Artifact: release.Artifact.Filename,
	}, nil
}

func (b *wailsUpdateBackend) DownloadAndInstall(ctx context.Context) error {
	return b.client.DownloadAndInstall(ctx)
}

func (b *wailsUpdateBackend) Restart(ctx context.Context) error {
	return b.client.Restart(ctx)
}
