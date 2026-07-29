package app

import (
	"slices"
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/permissions"
)

func testPermissions(state PermissionsState) *PermissionsService {
	return &PermissionsService{
		read:    func() PermissionsState { return state },
		request: func(string) bool { return true },
		open:    func(string) {},
	}
}

func rowsByID(page PermissionsPage) map[string]PermissionRow {
	out := map[string]PermissionRow{}
	for _, r := range page.Rows {
		out[r.ID] = r
	}
	return out
}

// A grant macOS will not let the app read is UNVERIFIED, not missing.
//
// This is the failure the Electron build hit: defaulting to "granted" made a denied microphone look
// fine while every dictation died. Defaulting the other way is not much better — it tells the user to
// grant something that may already be granted. The third state is the only honest answer.
func TestAnUnreadableGrantIsUnverifiedNotMissing(t *testing.T) {
	svc := testPermissions(PermissionsState{
		Microphone:        permissions.Unknown,
		SpeechRecognition: permissions.Granted,
		Accessibility:     true,
	})

	rows := rowsByID(svc.List())
	if got := rows["microphone"].State; got != PermUnknown {
		t.Errorf("microphone = %q, want %q", got, PermUnknown)
	}
	if got := rows["microphone"].Label; got != "Sin verificar" {
		t.Errorf("label = %q", got)
	}
}

// Denied and not-yet-asked both read as MISSING: from the user's side both mean "the app cannot do
// this yet", and the row's button is what distinguishes them.
func TestDeniedAndNotDeterminedBothReadAsMissing(t *testing.T) {
	for _, s := range []permissions.Status{permissions.Denied, permissions.NotDetermined} {
		svc := testPermissions(PermissionsState{Microphone: s})
		if got := rowsByID(svc.List())["microphone"].State; got != PermMissing {
			t.Errorf("microphone with %q = %q, want %q", s, got, PermMissing)
		}
	}
}

// A REQUIRED missing grant says "Requerido"; a recommended one says "Recomendado". The distinction is
// the point: one blocks dictation and the other does not, and shouting equally about both teaches the
// user to ignore the tab.
func TestRequiredAndRecommendedAreLabelledDifferently(t *testing.T) {
	svc := testPermissions(PermissionsState{
		Microphone:        permissions.Denied,
		SpeechRecognition: permissions.Denied,
		Accessibility:     false,
	})
	rows := rowsByID(svc.List())

	if got := rows["microphone"].Label; got != "Requerido" {
		t.Errorf("microphone label = %q, want Requerido", got)
	}
	if !rows["microphone"].Required {
		t.Error("microphone is not marked required")
	}
	if got := rows["speechRecognition"].Label; got != "Recomendado" {
		t.Errorf("speechRecognition label = %q, want Recomendado", got)
	}
	if rows["speechRecognition"].Required {
		t.Error("speechRecognition is marked required; only the Apple engine needs it")
	}
}

// The native prompt is offered ONLY where it exists and is still needed. Accessibility has no such
// API, so its button must deep-link instead of promising a prompt that never appears.
func TestOnlyPromptableGrantsOfferTheNativePrompt(t *testing.T) {
	svc := testPermissions(PermissionsState{
		Microphone:        permissions.Denied,
		SpeechRecognition: permissions.NotDetermined,
		Accessibility:     false,
	})
	rows := rowsByID(svc.List())

	if got := rows["microphone"].Action; got != PermRequest {
		t.Errorf("microphone action = %q, want %q", got, PermRequest)
	}
	if got := rows["speechRecognition"].Action; got != PermRequest {
		t.Errorf("speechRecognition action = %q, want %q", got, PermRequest)
	}
	if got := rows["accessibility"].Action; got != PermOpen {
		t.Errorf("accessibility action = %q, want %q — there is no prompt for it", got, PermOpen)
	}
	if got := rows["inputMonitoring"].Action; got != PermOpen {
		t.Errorf("inputMonitoring action = %q, want %q", got, PermOpen)
	}
}

// An ALREADY-GRANTED permission does not offer the prompt either: there is nothing to ask for, and the
// button should take the user to where they could revoke it.
func TestAGrantedPermissionDoesNotOfferThePrompt(t *testing.T) {
	svc := testPermissions(PermissionsState{Microphone: permissions.Granted, Accessibility: true})
	rows := rowsByID(svc.List())

	if got := rows["microphone"].Action; got != PermOpen {
		t.Errorf("granted microphone action = %q, want %q", got, PermOpen)
	}
	if got := rows["microphone"].Label; got != "Concedido" {
		t.Errorf("label = %q", got)
	}
}

// Input monitoring has NO API to read, so it is always unverified. Reporting "missing" on no evidence
// would send the user to grant something that may already be granted.
func TestInputMonitoringIsAlwaysUnverified(t *testing.T) {
	svc := testPermissions(PermissionsState{Microphone: permissions.Granted, Accessibility: true})
	if got := rowsByID(svc.List())["inputMonitoring"].State; got != PermUnknown {
		t.Errorf("inputMonitoring = %q, want %q", got, PermUnknown)
	}
}

// AllReady is what the summary line reads from, and an UNREADABLE required grant must not count as
// ready: the app cannot promise dictation works on something it could not check.
func TestAllReadyRequiresEveryRequiredGrant(t *testing.T) {
	ready := testPermissions(PermissionsState{Microphone: permissions.Granted, Accessibility: true})
	page := ready.List()
	if !page.AllReady {
		t.Errorf("all granted but AllReady=false, missing=%v", page.Missing)
	}
	if len(page.Missing) != 0 {
		t.Errorf("missing = %v, want none", page.Missing)
	}

	unreadable := testPermissions(PermissionsState{Microphone: permissions.Unknown, Accessibility: true})
	page = unreadable.List()
	if page.AllReady {
		t.Error("an unreadable required grant was counted as ready")
	}
	if !slices.Contains(page.Missing, "Micrófono") {
		t.Errorf("missing = %v, want it to name the microphone", page.Missing)
	}
}

// Missing must never marshal to null: the page would have to guard for it, like every other list in
// this payload.
func TestMissingIsNeverNil(t *testing.T) {
	svc := testPermissions(PermissionsState{Microphone: permissions.Granted, Accessibility: true})
	if svc.List().Missing == nil {
		t.Error("Missing is nil, want an empty slice")
	}
}

// Requesting a grant with no prompt FALLS BACK to opening System Settings. A button that reports
// success and changes nothing is worse than one that sends the user somewhere.
func TestRequestingAnUnpromptableGrantOpensSettings(t *testing.T) {
	var opened []string
	svc := testPermissions(PermissionsState{Accessibility: false})
	svc.request = func(string) bool { return false } // no prompt available
	svc.open = func(id string) { opened = append(opened, id) }

	svc.Request("accessibility")

	if !slices.Contains(opened, "accessibility") {
		t.Errorf("opened = %v, want it to have opened the accessibility pane", opened)
	}
}

// Every action RE-READS, because the point of pressing it is that the state may now be different.
func TestActingReReadsTheState(t *testing.T) {
	reads := 0
	svc := testPermissions(PermissionsState{Microphone: permissions.Granted, Accessibility: true})
	svc.read = func() PermissionsState {
		reads++
		return PermissionsState{Microphone: permissions.Granted, Accessibility: true}
	}

	svc.List()
	svc.Request("microphone")
	svc.Open("accessibility")

	if reads != 3 {
		t.Errorf("the state was read %d times, want one per call", reads)
	}
}
