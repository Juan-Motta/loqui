// The permissions service: what the Permisos tab reads and acts on.
//
// ITS OWN SERVICE rather than part of the settings payload, because permissions change from OUTSIDE
// the app. The user grants Accessibility in System Settings and comes back, and the tab has to be able
// to re-read on demand — a value baked into a bootstrap snapshot would be stale exactly when it
// matters most.
package app

import (
	"github.com/Juan-Motta/loqui-go/internal/permissions"
)

// PermissionsPage is one reading of every grant.
type PermissionsPage struct {
	Rows []PermissionRow `json:"rows"`
	// AllReady is whether every REQUIRED grant is in place. "Unknown" counts as not ready: the app
	// cannot promise dictation will work on a grant it could not read.
	AllReady bool `json:"allReady"`
	// Missing names the required grants that are not in place, for the summary line. Never nil.
	Missing []string `json:"missing"`
}

// PermissionsService is bound to the frontend as Permissions.
type PermissionsService struct {
	// read, request and open are seams: the real ones touch the TCC database and open System
	// Settings, neither of which belongs in a unit test — and TCC state varies per developer.
	read    func() PermissionsState
	request func(id string) bool
	open    func(id string)
}

func NewPermissionsService() *PermissionsService {
	return &PermissionsService{
		read:    livePermissions,
		request: requestPermission,
		open:    openPermissionPane,
	}
}

// ServiceName is what Wails calls this in its logs.
func (s *PermissionsService) ServiceName() string { return "Permissions" }

// List reads every grant now. Bound as Permissions.List().
//
// Called on entering the tab and on "Volver a comprobar", because that is the only way to notice a
// grant the user changed in System Settings while this window stayed open.
func (s *PermissionsService) List() PermissionsPage {
	return s.page()
}

// Request shows the native prompt for a grant that has one, then re-reads. Bound as
// Permissions.Request().
//
// Only the microphone and speech recognition have such an API. For anything else this falls through
// to opening System Settings rather than doing nothing: a button that reports success and changes
// nothing is worse than one that sends you somewhere.
func (s *PermissionsService) Request(id string) PermissionsPage {
	if !s.requester()(id) {
		s.opener()(id)
	}
	return s.page()
}

// Open deep-links to the pane where the user grants a permission, then re-reads. Bound as
// Permissions.Open().
//
// The re-read afterwards is optimistic — the user has not granted anything yet — but it costs nothing
// and covers the case where they grant it fast and the window never lost focus.
func (s *PermissionsService) Open(id string) PermissionsPage {
	s.opener()(id)
	return s.page()
}

func (s *PermissionsService) page() PermissionsPage {
	// Input monitoring is reported as UNKNOWN because there is no API to read it. The grant governs
	// whether the fn listener sees key events at all, so the only evidence would be whether that
	// listener is producing them — and reporting "missing" on no evidence would tell the user to grant
	// something that may already be granted.
	rows := permissionRows(s.reader()(), PermUnknown)
	missing := requiredMissing(rows)
	if missing == nil {
		missing = []string{}
	}
	return PermissionsPage{Rows: rows, AllReady: len(missing) == 0, Missing: missing}
}

func (s *PermissionsService) reader() func() PermissionsState {
	if s.read != nil {
		return s.read
	}
	return livePermissions
}

func (s *PermissionsService) requester() func(string) bool {
	if s.request != nil {
		return s.request
	}
	return requestPermission
}

func (s *PermissionsService) opener() func(string) {
	if s.open != nil {
		return s.open
	}
	return openPermissionPane
}

// requestPermission shows the system prompt for the grants that have one, reporting whether it even
// tried. The answer is discarded: the caller re-reads the real state instead of trusting a return
// value, because the prompt can be dismissed, denied, or never presented at all.
func requestPermission(id string) bool {
	switch id {
	case "microphone":
		permissions.RequestMicrophone()
		return true
	case "speechRecognition":
		permissions.RequestSpeechRecognition()
		return true
	default:
		return false
	}
}

// openPermissionPane reveals the pane where the user grants a permission.
//
// Guiding them there beats describing a path through a settings app Apple rearranges every release.
func openPermissionPane(id string) {
	switch id {
	case "microphone":
		permissions.OpenSettings(permissions.PaneMicrophone)
	case "accessibility":
		permissions.OpenSettings(permissions.PaneAccessibility)
	case "inputMonitoring":
		permissions.OpenSettings(permissions.PaneInputMonitoring)
	case "speechRecognition":
		permissions.OpenSettings(permissions.PaneSpeech)
	}
}
