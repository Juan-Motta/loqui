package app

import (
	"context"
	"sync"
	"time"
)

// Update states are intentionally app-owned strings. The frontend does not need to know which
// updater implementation is underneath, and keeping the Wails state out of the binding makes the
// UI contract testable with a fake backend.
const (
	UpdateStateUnavailable = "unavailable"
	UpdateStateIdle        = "idle"
	UpdateStateChecking    = "checking"
	UpdateStateUpToDate    = "up-to-date"
	UpdateStateAvailable   = "available"
	UpdateStateInstalling  = "installing"
	UpdateStateReady       = "ready"
	UpdateStateRestarting  = "restarting"
	UpdateStateError       = "error"
)

const (
	UpdateEventChecking   = "updates:checking"
	UpdateEventUpToDate   = "updates:up-to-date"
	UpdateEventAvailable  = "updates:available"
	UpdateEventInstalling = "updates:installing"
	UpdateEventReady      = "updates:ready"
	UpdateEventError      = "updates:error"
)

const (
	updateErrUnavailable = "Las actualizaciones no están disponibles en esta compilación"
	updateErrBusy        = "Ya hay una actualización en curso"
	updateErrCheck       = "No se pudo buscar actualizaciones"
	updateErrInstall     = "No se pudo instalar la actualización"
	updateErrRestart     = "No se pudo reiniciar Loqui"
)

// UpdateRelease is the provider-neutral information the UI needs before asking the user to
// download an update. It deliberately excludes URLs, credentials, and provider metadata.
type UpdateRelease struct {
	Version  string `json:"version"`
	Name     string `json:"name"`
	Notes    string `json:"notes"`
	Artifact string `json:"artifact"`
}

// UpdateStatus is the complete update state the About view can render from one snapshot.
type UpdateStatus struct {
	State            string `json:"state"`
	CurrentVersion   string `json:"currentVersion"`
	AvailableVersion string `json:"availableVersion"`
	Name             string `json:"name"`
	Notes            string `json:"notes"`
	Artifact         string `json:"artifact"`
	Error            string `json:"error"`
	Ready            bool   `json:"ready"`
}

// UpdateResult carries a fresh status even when an operation fails, matching the Settings service
// contract: the webview must repaint from authoritative state rather than optimistic local state.
type UpdateResult struct {
	Status UpdateStatus `json:"status"`
	Error  string       `json:"error"`
}

// UpdateBackend is the narrow seam around Wails' updater. The service never imports a provider or
// a window toolkit, so all state transitions can be exercised without a macOS GUI or network.
type UpdateBackend interface {
	CurrentVersion() string
	Check(context.Context) (*UpdateRelease, error)
	DownloadAndInstall(context.Context) error
	Restart(context.Context) error
}

// UpdateService owns user-visible update state and the opt-out background check scheduler.
type UpdateService struct {
	mu         sync.Mutex
	backend    UpdateBackend
	autoChecks func() bool
	emit       func(string, any)
	log        func(string, string)
	status     UpdateStatus
	release    *UpdateRelease

	autoCancel context.CancelFunc
	autoDone   chan struct{}
}

func NewUpdateService(backend UpdateBackend, autoChecks func() bool, emit func(string, any), logf func(string, string)) *UpdateService {
	if autoChecks == nil {
		autoChecks = func() bool { return false }
	}
	service := &UpdateService{
		backend:    backend,
		autoChecks: autoChecks,
		emit:       emit,
		log:        logf,
		status: UpdateStatus{
			State: UpdateStateUnavailable,
		},
	}
	if backend != nil {
		service.status.State = UpdateStateIdle
		service.status.CurrentVersion = backend.CurrentVersion()
	}
	return service
}

// configureBackend attaches the platform updater after the Wails application exists. It is kept
// private so Wails does not expose an interface-valued setup method to the webview.
func (s *UpdateService) configureBackend(backend UpdateBackend) {
	s.mu.Lock()
	s.backend = backend
	if backend == nil {
		s.status.State = UpdateStateUnavailable
		s.status.CurrentVersion = ""
	} else {
		s.status.State = UpdateStateIdle
		s.status.CurrentVersion = backend.CurrentVersion()
		s.status.Error = ""
	}
	s.mu.Unlock()
}

// ConfigureUpdateBackend attaches the platform updater after application.New creates it. This is a
// package function rather than a service method so the generated frontend bindings expose only the
// user-facing Check/Install/Restart/Status operations.
func ConfigureUpdateBackend(service *UpdateService, backend UpdateBackend) {
	if service != nil {
		service.configureBackend(backend)
	}
}

// ServiceName is the stable diagnostic name; the generated binding follows UpdateService.
func (s *UpdateService) ServiceName() string { return "Updates" }

func (s *UpdateService) Status() UpdateStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *UpdateService) Check() UpdateResult {
	return s.check(context.Background(), false)
}

func (s *UpdateService) check(ctx context.Context, background bool) UpdateResult {
	s.mu.Lock()
	backend := s.backend
	if backend == nil {
		result := s.failLocked(updateErrUnavailable, UpdateStateUnavailable)
		s.mu.Unlock()
		return result
	}
	if s.status.State == UpdateStateChecking || s.status.State == UpdateStateInstalling || s.status.State == UpdateStateRestarting {
		result := s.failLocked(updateErrBusy, s.status.State)
		s.mu.Unlock()
		return result
	}
	s.status.State = UpdateStateChecking
	s.status.Error = ""
	s.status.Ready = false
	s.clearReleaseLocked()
	status := s.status
	s.mu.Unlock()
	s.emitEvent(UpdateEventChecking, status)

	release, err := backend.Check(ctx)
	if err != nil {
		s.recordLog("UPDATE-CHECK", err.Error())
		s.mu.Lock()
		result := s.failLocked(updateErrCheck, UpdateStateError)
		s.mu.Unlock()
		if !background {
			s.emitEvent(UpdateEventError, result.Status)
		}
		return result
	}
	if release == nil {
		s.mu.Lock()
		result := s.successLocked(UpdateStateUpToDate)
		s.mu.Unlock()
		s.emitEvent(UpdateEventUpToDate, result.Status)
		return result
	}
	if release.Version == "" || release.Artifact == "" {
		s.recordLog("UPDATE-CHECK", "provider returned an incomplete release")
		s.mu.Lock()
		result := s.failLocked(updateErrCheck, UpdateStateError)
		s.mu.Unlock()
		if !background {
			s.emitEvent(UpdateEventError, result.Status)
		}
		return result
	}

	s.mu.Lock()
	copyRelease := *release
	s.release = &copyRelease
	s.status.State = UpdateStateAvailable
	s.status.AvailableVersion = release.Version
	s.status.Name = release.Name
	s.status.Notes = release.Notes
	s.status.Artifact = release.Artifact
	s.status.Error = ""
	s.status.Ready = false
	result := UpdateResult{Status: s.status}
	s.mu.Unlock()
	s.emitEvent(UpdateEventAvailable, result.Status)
	return result
}

func (s *UpdateService) Install() UpdateResult {
	s.mu.Lock()
	backend := s.backend
	if backend == nil {
		result := s.failLocked(updateErrUnavailable, UpdateStateUnavailable)
		s.mu.Unlock()
		return result
	}
	if s.status.State == UpdateStateInstalling || s.status.State == UpdateStateRestarting {
		result := s.failLocked(updateErrBusy, s.status.State)
		s.mu.Unlock()
		return result
	}
	if s.release == nil || s.status.State != UpdateStateAvailable {
		result := s.failLocked("No hay una actualización preparada para instalar", s.status.State)
		s.mu.Unlock()
		return result
	}
	s.status.State = UpdateStateInstalling
	s.status.Error = ""
	status := s.status
	s.mu.Unlock()
	s.emitEvent(UpdateEventInstalling, status)

	if err := backend.DownloadAndInstall(context.Background()); err != nil {
		s.recordLog("UPDATE-INSTALL", err.Error())
		s.mu.Lock()
		result := s.failLocked(updateErrInstall, UpdateStateError)
		s.mu.Unlock()
		s.emitEvent(UpdateEventError, result.Status)
		return result
	}
	s.mu.Lock()
	s.status.State = UpdateStateReady
	s.status.Ready = true
	s.status.Error = ""
	result := UpdateResult{Status: s.status}
	s.mu.Unlock()
	s.emitEvent(UpdateEventReady, result.Status)
	return result
}

func (s *UpdateService) Restart() UpdateResult {
	s.mu.Lock()
	backend := s.backend
	if backend == nil {
		result := s.failLocked(updateErrUnavailable, UpdateStateUnavailable)
		s.mu.Unlock()
		return result
	}
	if !s.status.Ready || s.status.State != UpdateStateReady {
		result := s.failLocked("La actualización todavía no está lista para reiniciar", s.status.State)
		s.mu.Unlock()
		return result
	}
	s.status.State = UpdateStateRestarting
	s.status.Error = ""
	s.mu.Unlock()

	if err := backend.Restart(context.Background()); err != nil {
		s.recordLog("UPDATE-RESTART", err.Error())
		s.mu.Lock()
		result := s.failLocked(updateErrRestart, UpdateStateError)
		s.mu.Unlock()
		s.emitEvent(UpdateEventError, result.Status)
		return result
	}
	return UpdateResult{Status: s.Status()}
}

// startAutoChecks performs one delayed check and then checks at interval. It deliberately calls
// check(), never Install(): the user's confirmation is the boundary between discovery and mutation.
func (s *UpdateService) startAutoChecks(initialDelay, interval time.Duration) {
	if initialDelay <= 0 {
		initialDelay = 30 * time.Second
	}
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	s.stopAutoChecks()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.mu.Lock()
	s.autoCancel = cancel
	s.autoDone = done
	s.mu.Unlock()
	go func() {
		defer close(done)
		timer := time.NewTimer(initialDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.runBackgroundCheck(ctx)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runBackgroundCheck(ctx)
			}
		}
	}()
}

func (s *UpdateService) runBackgroundCheck(ctx context.Context) {
	if !s.autoChecks() {
		return
	}
	_ = s.check(ctx, true)
}

func (s *UpdateService) stopAutoChecks() {
	s.mu.Lock()
	cancel := s.autoCancel
	done := s.autoDone
	s.autoCancel = nil
	s.autoDone = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// StartUpdateChecks starts the app-owned background scheduler without exposing scheduler controls
// as webview bindings.
func StartUpdateChecks(service *UpdateService, initialDelay, interval time.Duration) {
	if service != nil {
		service.startAutoChecks(initialDelay, interval)
	}
}

// StopUpdateChecks stops the app-owned background scheduler.
func StopUpdateChecks(service *UpdateService) {
	if service != nil {
		service.stopAutoChecks()
	}
}

func (s *UpdateService) successLocked(state string) UpdateResult {
	s.status.State = state
	s.status.Error = ""
	s.status.Ready = false
	return UpdateResult{Status: s.status}
}

func (s *UpdateService) clearReleaseLocked() {
	s.release = nil
	s.status.AvailableVersion = ""
	s.status.Name = ""
	s.status.Notes = ""
	s.status.Artifact = ""
}

func (s *UpdateService) failLocked(message, state string) UpdateResult {
	s.status.State = state
	s.status.Error = message
	s.status.Ready = false
	return UpdateResult{Status: s.status, Error: message}
}

func (s *UpdateService) emitEvent(name string, value any) {
	if s.emit != nil {
		s.emit(name, value)
	}
}

func (s *UpdateService) recordLog(tag, message string) {
	if s.log != nil {
		s.log(tag, message)
	}
}
