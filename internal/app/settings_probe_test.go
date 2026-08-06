package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/store"
)

// fakeDoer stands in for the network. It counts calls, so a test can assert that a rejected probe
// never left the machine — "cero HTTP" is half of what most of these cases are about.
type fakeDoer struct {
	mu     sync.Mutex
	calls  int
	status int
	body   string
	// deadline records how much of the context budget was left when the request arrived. It is what
	// proves the HTTP budget starts AFTER the preflight rather than being eaten by it.
	deadline time.Duration
	// block makes the request wait for the context, for the timeout case.
	block bool
	// err makes the transport itself fail, for the cases where Azure is never reached.
	err error
	// sentKey is the credential that actually went out, byte for byte. Counting Keychain reads says
	// which SOURCE was consulted; only this says which VALUE was used — and a probe that approves a
	// key different from the one about to be stored is worse than one that never ran.
	sentKey string
}

func (d *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	d.calls++
	d.sentKey = req.Header.Get("Ocp-Apim-Subscription-Key")
	if dl, ok := req.Context().Deadline(); ok {
		d.deadline = time.Until(dl)
	}
	block := d.block
	status, body := d.status, d.body
	failure := d.err
	d.mu.Unlock()

	if block {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	if failure != nil {
		return nil, failure
	}
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func (d *fakeDoer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func (d *fakeDoer) key() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sentKey
}

// secretSpy wraps the key read so a test can assert it was never consulted. A probe that rejects on
// the region must not pay the Keychain's three seconds to find that out.
type secretSpy struct {
	mu    sync.Mutex
	calls int
	fn    func(store.KeySlot) (string, error)
}

func (s *secretSpy) read(slot store.KeySlot) (string, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.fn(slot)
}

func (s *secretSpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// probeService is testService plus the two seams the probe needs: the network and the key read.
func probeService(t *testing.T, st *store.Store) (*SettingsService, *testVault, *fakeDoer, *secretSpy) {
	t.Helper()
	svc, vault := testService(t, st)
	doer := &fakeDoer{}
	spy := &secretSpy{fn: svc.getSecret}
	svc.probeClient = doer
	svc.getSecret = spy.read
	return svc, vault, doer, spy
}

// The slot has to be one this app can actually probe, and that is NOT the same question as whether
// its credential is usable.
//
// store.IsAvailableKeySlot is true for grok, so using it as the gate would send a Grok key to
// Azure's token endpoint. The allowlist is what stops that, and this is the case that would catch
// it going back.
func TestProbingASlotWithoutAProbeNeverLeavesTheMachine(t *testing.T) {
	for _, slot := range []string{"grok", "openai", "elevenlabs", "azure-openai"} {
		t.Run(slot, func(t *testing.T) {
			st := store.NewAt(t.TempDir())
			svc, _, doer, spy := probeService(t, st)

			res := svc.TestConnection(slot, "eastus", "una-clave")

			if res.OK {
				t.Errorf("OK = true for %s, which has no connection test", slot)
			}
			if res.Error == "" {
				t.Error("a slot with no probe must say so")
			}
			if doer.count() != 0 {
				t.Errorf("HTTP calls = %d, want 0", doer.count())
			}
			if spy.count() != 0 {
				t.Errorf("Keychain reads = %d, want 0", spy.count())
			}
		})
	}
}

func TestProbingAnUnknownSlotIsRejected(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _, doer, _ := probeService(t, st)

	res := svc.TestConnection("no-such-slot", "eastus", "una-clave")

	if res.OK || res.Error == "" {
		t.Fatalf("an unknown slot was accepted: %+v", res)
	}
	if doer.count() != 0 {
		t.Errorf("HTTP calls = %d, want 0", doer.count())
	}
}

// "No key" and "your keys could not be read" send the user to two different places, so the probe
// must not collapse them — the same distinction KeyStatusFor exists for.
func TestProbeTellsAnEmptySlotApartFromUnreadableCredentials(t *testing.T) {
	cases := []struct {
		name     string
		readErr  error
		wantsKey bool // the message must point at the key field
		says     string
		// notSays keeps the three messages from collapsing into one. Without it, mapping every
		// failure onto the timeout wording would pass: each case would still "mention" its word.
		notSays string
	}{
		{"empty slot", store.ErrNoSecret, true, "falta la clave", "archivo de claves"},
		{"credentials unreadable", store.ErrSecretsUnreadable, false, "archivo de claves", "falta la clave"},
		{"read broke some other way", errors.New("disk on fire"), false, "no se pudo leer la clave guardada", "archivo de claves"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := store.NewAt(t.TempDir())
			if err := st.UpdateSettings(func(cfg *store.Settings) error {
				cfg.Region = "eastus2"
				return nil
			}); err != nil {
				t.Fatalf("arranging the region: %v", err)
			}
			svc, _, doer, _ := probeService(t, st)
			svc.getSecret = func(store.KeySlot) (string, error) { return "", c.readErr }

			res := svc.TestConnection("azure-speech", "", "")

			if res.OK {
				t.Fatal("a probe with no usable key reported success")
			}
			if !strings.Contains(res.Error, c.says) {
				t.Errorf("error = %q, want it to mention %q", res.Error, c.says)
			}
			if strings.Contains(res.Error, c.notSays) {
				t.Errorf("error = %q — that is another case's wording, so the three collapsed into one",
					res.Error)
			}
			if doer.count() != 0 {
				t.Errorf("HTTP calls = %d, want 0 — nothing to authenticate with", doer.count())
			}
			// Only the empty slot is the user's to fix by typing, so only that one marks the field.
			if got := res.Field == "key"; got != c.wantsKey {
				t.Errorf("Field = %q, wantsKey = %v", res.Field, c.wantsKey)
			}
		})
	}
}

// The stored region is not validated on the way in — LoadSettings takes any string that is valid
// JSON — so the probe is where a bad one has to be caught.
func TestProbeRejectsAnUnusableRegionWithoutTouchingTheKeychain(t *testing.T) {
	cases := []struct{ name, stored string }{
		{"no region anywhere", ""},
		{"stored region is not a region", "east/us"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := store.NewAt(t.TempDir())
			if err := st.UpdateSettings(func(cfg *store.Settings) error {
				cfg.Region = c.stored
				return nil
			}); err != nil {
				t.Fatalf("arranging the region: %v", err)
			}
			svc, vault, doer, spy := probeService(t, st)
			// A key IS stored, and the field is left empty on purpose: that is the only arrangement
			// where the Keychain read WOULD happen, so it is the only one that can prove the region
			// is checked first. With a typed key this test passes whatever the order is — which is
			// what it used to do.
			vault.set(store.SlotAzureSpeech, "la-guardada")

			res := svc.TestConnection("azure-speech", "", "")

			if res.OK || res.Error == "" {
				t.Fatalf("an unusable region was accepted: %+v", res)
			}
			if res.Field != "region" {
				t.Errorf("Field = %q, want region — that is the input the user has to fix", res.Field)
			}
			if doer.count() != 0 {
				t.Errorf("HTTP calls = %d, want 0", doer.count())
			}
			// A region that cannot work does not justify RESOLVING the credential. (The payload this
			// returns still reads the Keychain, as every payload does — that is a different cost,
			// and claiming otherwise would be a claim this assertion does not make.)
			if spy.count() != 0 {
				t.Errorf("probe key resolutions = %d, want 0", spy.count())
			}
		})
	}
}

func TestProbeReportsWhatAzureAnswered(t *testing.T) {
	cases := []struct {
		name   string
		status int
		wantOK bool
	}{
		{"credentials accepted", http.StatusOK, true},
		{"credentials rejected", http.StatusUnauthorized, false},
		{"service broken", http.StatusInternalServerError, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := store.NewAt(t.TempDir())
			svc, _, doer, _ := probeService(t, st)
			doer.status = c.status
			doer.body = "un-token"

			res := svc.TestConnection("azure-speech", "eastus2", "una-clave")

			if res.OK != c.wantOK {
				t.Fatalf("OK = %v, want %v (error: %q)", res.OK, c.wantOK, res.Error)
			}
			if c.wantOK && res.Message == "" {
				t.Error("a successful probe must say so — an empty message is what this whole fix is about")
			}
			if !c.wantOK && res.Error == "" {
				t.Error("a failed probe must say why")
			}
			if doer.count() != 1 {
				t.Errorf("HTTP calls = %d, want 1", doer.count())
			}
		})
	}
}

// Typing a key is how you check one BEFORE saving it; leaving the field empty checks the one that is
// actually in use. Both are real, and which key was used has to be the one the user meant.
func TestProbeUsesTheTypedKeyAndFallsBackToTheStoredOne(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault, doer, spy := probeService(t, st)
	vault.set(store.SlotAzureSpeech, "la-guardada")
	doer.body = "un-token"

	if res := svc.TestConnection("azure-speech", "eastus2", "la-escrita"); !res.OK {
		t.Fatalf("typed key: %+v", res)
	}
	if spy.count() != 0 {
		t.Errorf("Keychain reads = %d — a typed key must not be looked up", spy.count())
	}
	if got := doer.key(); got != "la-escrita" {
		t.Errorf("sent %q, want la-escrita — counting reads alone would pass on any constant", got)
	}

	if res := svc.TestConnection("azure-speech", "eastus2", ""); !res.OK {
		t.Fatalf("stored key: %+v", res)
	}
	if spy.count() != 1 {
		t.Errorf("Keychain reads = %d, want 1 — the empty field must read the stored key", spy.count())
	}
	if got := doer.key(); got != "la-guardada" {
		t.Errorf("sent %q, want la-guardada", got)
	}
}

// What is TESTED has to be what would be STORED. A pasted credential usually arrives with a newline
// or a space, and trimming on one path and not the other hands out a green tick for a key that then
// fails at dictation — the exact false confidence this button exists to remove.
func TestTheProbeSendsTheSameBytesTheSaveWouldStore(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault, doer, _ := probeService(t, st)
	doer.body = "un-token"

	if res := svc.TestConnection("azure-speech", "eastus2", "  la-escrita\n"); !res.OK {
		t.Fatalf("probe failed: %+v", res)
	}
	probed := doer.key()

	if res := svc.SaveConnection("azure-speech", "eastus2", "  la-escrita\n"); res.Error != "" {
		t.Fatalf("save failed: %s", res.Error)
	}
	stored, _ := vault.get(store.SlotAzureSpeech)

	if probed != stored {
		t.Errorf("probed %q but stored %q — the tick was given to a different credential", probed, stored)
	}
}

// The environment override is what dictation would use, so the probe has to agree with it. If they
// disagreed, a green probe would be describing a credential the microphone never sees.
func TestProbeHonoursTheEnvironmentOverrideExactlyAsDictationDoes(t *testing.T) {
	cases := []struct {
		name       string
		env        string
		wantsVault bool
		saysEnv    bool
	}{
		{"a real override wins over the Keychain", "la-del-entorno", false, false},
		{"an empty variable is not an override", "", true, false},
		{"a blank variable is an override, and a broken one", "   ", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := store.NewAt(t.TempDir())
			svc, vault, doer, spy := probeService(t, st)
			vault.set(store.SlotAzureSpeech, "la-guardada")
			doer.body = "un-token"
			t.Setenv("LOQUI_AZURE_KEY", c.env)

			res := svc.TestConnection("azure-speech", "eastus2", "")

			if c.saysEnv {
				// A blank override is the one case where the key is unusable AND the reason is
				// invisible: the slot reads as configured and the Keychain holds a good key that
				// nothing will read. Saying "no key" here sends the user to fix the wrong thing.
				if !strings.Contains(res.Error, "LOQUI_AZURE_KEY") {
					t.Errorf("error = %q, want it to name the variable that is in force", res.Error)
				}
				if doer.count() != 0 {
					t.Errorf("HTTP calls = %d, want 0", doer.count())
				}
				return
			}
			if !res.OK {
				t.Fatalf("probe failed: %+v", res)
			}
			if got := spy.count() > 0; got != c.wantsVault {
				t.Errorf("read the Keychain = %v, want %v", got, c.wantsVault)
			}
		})
	}
}

// The HTTP budget must start after the preflight, not be eaten by it: a slow Keychain would
// otherwise leave the network with whatever seconds were left over.
func TestTheNetworkBudgetStartsAfterTheKeyIsResolved(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault, doer, _ := probeService(t, st)
	vault.set(store.SlotAzureSpeech, "la-guardada")
	doer.body = "un-token"
	svc.getSecret = func(store.KeySlot) (string, error) {
		time.Sleep(120 * time.Millisecond)
		return "la-guardada", nil
	}

	if res := svc.TestConnection("azure-speech", "eastus2", ""); !res.OK {
		t.Fatalf("probe failed: %+v", res)
	}

	doer.mu.Lock()
	left := doer.deadline
	doer.mu.Unlock()
	if left < probeHTTPTimeout-50*time.Millisecond {
		t.Errorf("the request arrived with %v of budget, want ~%v — the Keychain ate into it",
			left, probeHTTPTimeout)
	}
}

func TestAProbeThatNeverAnswersGivesUpAndSaysSo(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _, doer, _ := probeService(t, st)
	doer.block = true
	svc.probeTimeout = 40 * time.Millisecond

	start := time.Now()
	res := svc.TestConnection("azure-speech", "eastus2", "una-clave")
	elapsed := time.Since(start)

	if res.OK {
		t.Fatal("a probe that never answered reported success")
	}
	if res.Error == "" {
		t.Error("giving up has to be reported, not swallowed")
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v — the deadline did not bound the call", elapsed)
	}
}

// Not reaching Azure at all is its own answer. Go's transport text is about sockets and is in
// English; showing it as the verdict of a button labelled "Probar conexión" tells the user nothing
// they can act on, and looks like the app broke rather than the network.
func TestANetworkFailureIsWordedForAPersonAndDetailedInTheLog(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _, doer, _ := probeService(t, st)
	var logged strings.Builder
	svc.log = func(tag, msg string) { logged.WriteString(tag + " " + msg + "\n") }
	doer.err = errors.New("dial tcp: lookup eastus2.api.cognitive.microsoft.com: no such host")

	res := svc.TestConnection("azure-speech", "eastus2", "una-clave")

	if res.OK {
		t.Fatal("a probe that never reached Azure reported success")
	}
	if strings.Contains(res.Error, "dial tcp") || strings.Contains(res.Error, "no such host") {
		t.Errorf("error = %q — that is Go's transport text, not a sentence for a user", res.Error)
	}
	if strings.Contains(strings.ToLower(res.Error), "inválida") {
		t.Errorf("error = %q — an unreachable service is not a rejected credential", res.Error)
	}
	// The detail is not thrown away: it is what a bug report needs.
	if !strings.Contains(logged.String(), "no such host") {
		t.Errorf("the technical detail was lost; log = %q", logged.String())
	}
}

// The payload never carries the secret, and neither does the probe's result. This is the test that
// notices if a future error message starts interpolating the key it was given.
func TestAProbeResultNeverCarriesTheSecret(t *testing.T) {
	const secret = "clave-secretisima-0123456789"
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError} {
		st := store.NewAt(t.TempDir())
		svc, _, doer, _ := probeService(t, st)
		doer.status = status

		var logged strings.Builder
		svc.log = func(tag, msg string) { logged.WriteString(tag + " " + msg + "\n") }

		res := svc.TestConnection("azure-speech", "eastus2", secret)

		// The WHOLE result, serialised. Checking named fields one by one only covers the fields
		// someone remembered; a future message interpolated into any other field would walk past it.
		encoded, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("status %d: the result leaked the key: %s", status, encoded)
		}
		// And the log, which is the other thing that leaves this process: it is attached to bug
		// reports, so a credential in it travels further than one on screen.
		if strings.Contains(logged.String(), secret) {
			t.Fatalf("status %d: the log leaked the key: %s", status, logged.String())
		}
	}
}

// The probe returns a payload even though it writes nothing, and this is the case that makes it
// load-bearing rather than decorative: a Keychain write that timed out and landed afterwards leaves
// the page painted from a snapshot that says there is no key. The probe is the next thing that
// reads the Keychain, so it is the next chance to tell the truth.
func TestTheProbeCarriesAFreshPayloadSoALateWriteStopsBeingInvisible(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault, doer, _ := probeService(t, st)
	doer.body = "un-token"

	// The page's picture: nothing stored. This is what a timed-out save leaves behind.
	before := svc.Load()
	if statusOf(before, "azure-speech") != store.KeyAbsent {
		t.Fatalf("arranged state is wrong: %v", statusOf(before, "azure-speech"))
	}

	// The abandoned write lands.
	vault.set(store.SlotAzureSpeech, "la-que-llegó-tarde")

	res := svc.TestConnection("azure-speech", "eastus2", "")

	if !res.OK {
		t.Fatalf("probe failed: %+v", res)
	}
	if got := statusOf(res.Payload, "azure-speech"); got != store.KeyPresent {
		t.Errorf("the probe's payload still says %v — the card would stay stale", got)
	}
}

func statusOf(p SettingsPayload, slot string) store.KeyStatus {
	for _, k := range p.Keys {
		if k.Slot == slot {
			return k.Status
		}
	}
	return ""
}

// A probe is a read. Nothing it does may change what is stored — the user is checking a credential,
// not committing to it.
func TestAProbeWritesNothing(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault, doer, _ := probeService(t, st)
	doer.body = "un-token"
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = "eastus2"
		return nil
	}); err != nil {
		t.Fatalf("arranging: %v", err)
	}

	if res := svc.TestConnection("azure-speech", "westeurope", "una-clave"); !res.OK {
		t.Fatalf("probe failed: %+v", res)
	}

	if got := st.LoadSettings().Region; got != "eastus2" {
		t.Errorf("region = %q — the probe wrote the region it was asked to test", got)
	}
	if vault.size() != 0 {
		t.Errorf("the probe stored %d secrets", vault.size())
	}
}

// A cancelled context must not be reported as a rejected credential: "your key is wrong" and "we
// gave up asking" send the user to completely different places.
func TestProbeDistinguishesGivingUpFromBeingRejected(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _, doer, _ := probeService(t, st)
	doer.block = true
	svc.probeTimeout = 40 * time.Millisecond

	res := svc.TestConnection("azure-speech", "eastus2", "una-clave")

	if strings.Contains(strings.ToLower(res.Error), "inválida") {
		t.Errorf("error = %q — a timeout is not a bad credential", res.Error)
	}
	_ = context.Canceled
}
