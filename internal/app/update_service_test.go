package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeUpdateBackend struct {
	mu          sync.Mutex
	version     string
	release     *UpdateRelease
	checkErr    error
	installErr  error
	restartErr  error
	checks      int
	installs    int
	restarts    int
	installWait chan struct{}
}

func (f *fakeUpdateBackend) CurrentVersion() string { return f.version }

func (f *fakeUpdateBackend) Check(context.Context) (*UpdateRelease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	if f.release == nil {
		return nil, nil
	}
	out := *f.release
	return &out, nil
}

func (f *fakeUpdateBackend) DownloadAndInstall(ctx context.Context) error {
	f.mu.Lock()
	f.installs++
	wait := f.installWait
	err := f.installErr
	f.mu.Unlock()
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func (f *fakeUpdateBackend) Restart(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts++
	return f.restartErr
}

func (f *fakeUpdateBackend) counts() (checks, installs, restarts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checks, f.installs, f.restarts
}

type updateEventRecorder struct {
	mu     sync.Mutex
	names  []string
	values []any
}

func (r *updateEventRecorder) emit(name string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
	r.values = append(r.values, value)
}

func (r *updateEventRecorder) has(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, got := range r.names {
		if got == name {
			return true
		}
	}
	return false
}

func waitForUpdate(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for update service state")
}

func TestUpdateCheckMapsNoUpdateAndAvailableStates(t *testing.T) {
	recorder := &updateEventRecorder{}
	backend := &fakeUpdateBackend{version: "0.2.0"}
	service := NewUpdateService(backend, func() bool { return true }, recorder.emit, nil)

	if result := service.Check(); result.Error != "" {
		t.Fatalf("no-update check returned error %q", result.Error)
	}
	if got := service.Status().State; got != UpdateStateUpToDate {
		t.Fatalf("state after no-update check = %q", got)
	}

	backend.mu.Lock()
	backend.release = &UpdateRelease{Version: "0.3.0", Name: "Loqui 0.3.0", Notes: "notes", Artifact: "Loqui.zip"}
	backend.mu.Unlock()
	if result := service.Check(); result.Error != "" {
		t.Fatalf("available check returned error %q", result.Error)
	}
	status := service.Status()
	if status.State != UpdateStateAvailable || status.AvailableVersion != "0.3.0" || status.Artifact != "Loqui.zip" {
		t.Fatalf("available status = %+v", status)
	}
	if !recorder.has(UpdateEventAvailable) {
		t.Fatal("available check did not emit the update event")
	}

	backend.mu.Lock()
	backend.release = nil
	backend.mu.Unlock()
	if result := service.Check(); result.Error != "" {
		t.Fatalf("follow-up no-update check returned error %q", result.Error)
	}
	status = service.Status()
	if status.State != UpdateStateUpToDate || status.AvailableVersion != "" || status.Artifact != "" {
		t.Fatalf("no-update status retained stale release data = %+v", status)
	}
}

func TestUpdateCheckHidesBackendErrorAndLogsIt(t *testing.T) {
	recorder := &updateEventRecorder{}
	var logged string
	backend := &fakeUpdateBackend{version: "0.2.0", checkErr: errors.New("token=secret should not reach UI")}
	service := NewUpdateService(backend, func() bool { return true }, recorder.emit,
		func(_, message string) { logged = message })

	result := service.Check()
	if result.Error == "" || result.Error == "token=secret should not reach UI" {
		t.Fatalf("check error leaked backend detail: %q", result.Error)
	}
	if logged == "" || logged == result.Error {
		t.Fatalf("backend detail was not sent to the diagnostic logger: %q", logged)
	}
	if service.Status().State != UpdateStateError {
		t.Fatalf("state after failed check = %q", service.Status().State)
	}
}

func TestUpdateInstallAndRestartRequireExplicitState(t *testing.T) {
	backend := &fakeUpdateBackend{version: "0.2.0"}
	service := NewUpdateService(backend, func() bool { return true }, nil, nil)
	if result := service.Install(); result.Error == "" {
		t.Fatal("install succeeded without an available release")
	}
	if result := service.Restart(); result.Error == "" {
		t.Fatal("restart succeeded before a verified install")
	}

	backend.release = &UpdateRelease{Version: "0.3.0", Artifact: "Loqui.zip"}
	if result := service.Check(); result.Error != "" {
		t.Fatalf("check: %s", result.Error)
	}
	if result := service.Install(); result.Error != "" {
		t.Fatalf("install: %s", result.Error)
	}
	if !service.Status().Ready || service.Status().State != UpdateStateReady {
		t.Fatalf("state after install = %+v", service.Status())
	}
	if result := service.Restart(); result.Error != "" {
		t.Fatalf("restart: %s", result.Error)
	}
	_, installs, restarts := backend.counts()
	if installs != 1 || restarts != 1 {
		t.Fatalf("backend install/restart counts = %d/%d", installs, restarts)
	}
}

func TestUpdateSchedulerChecksOnlyWhenEnabledAndNeverInstalls(t *testing.T) {
	recorder := &updateEventRecorder{}
	backend := &fakeUpdateBackend{
		version: "0.2.0",
		release: &UpdateRelease{Version: "0.3.0", Artifact: "Loqui.zip"},
	}
	service := NewUpdateService(backend, func() bool { return true }, recorder.emit, nil)
	service.startAutoChecks(time.Millisecond, time.Hour)
	waitForUpdate(t, func() bool { return service.Status().State == UpdateStateAvailable })
	service.stopAutoChecks()
	checks, installs, _ := backend.counts()
	if checks == 0 {
		t.Fatal("enabled scheduler did not check")
	}
	if installs != 0 {
		t.Fatalf("background scheduler installed an update %d time(s)", installs)
	}
	if !recorder.has(UpdateEventAvailable) {
		t.Fatal("background scheduler did not emit availability")
	}

	disabledBackend := &fakeUpdateBackend{version: "0.2.0", release: backend.release}
	disabled := NewUpdateService(disabledBackend, func() bool { return false }, nil, nil)
	disabled.startAutoChecks(time.Millisecond, time.Hour)
	time.Sleep(20 * time.Millisecond)
	disabled.stopAutoChecks()
	checks, installs, _ = disabledBackend.counts()
	if checks != 0 || installs != 0 {
		t.Fatalf("disabled scheduler made backend calls checks=%d installs=%d", checks, installs)
	}
}

func TestUpdateSchedulerStopCancelsPendingInitialCheck(t *testing.T) {
	backend := &fakeUpdateBackend{version: "0.2.0"}
	service := NewUpdateService(backend, func() bool { return true }, nil, nil)
	service.startAutoChecks(time.Hour, time.Hour)
	service.stopAutoChecks()
	time.Sleep(10 * time.Millisecond)
	checks, _, _ := backend.counts()
	if checks != 0 {
		t.Fatalf("stopped scheduler still checked %d time(s)", checks)
	}
}
