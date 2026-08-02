// Ported from the Electron suites test/unit/tokenService.test.ts and
// test/unit/azureProbe.test.ts. The HTTP client is injected, so nothing here touches
// the network.
package azure

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeDoer struct {
	calls    int
	status   int
	body     string
	err      error
	lastKey  string
	lastURL  string
	statuses []int // when set, consumed one per call
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.calls++
	f.lastKey = req.Header.Get("Ocp-Apim-Subscription-Key")
	f.lastURL = req.URL.String()
	if f.err != nil {
		return nil, f.err
	}
	status := f.status
	if len(f.statuses) > 0 {
		status = f.statuses[0]
		f.statuses = f.statuses[1:]
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
	}, nil
}

func keyFn(k string) func() (string, error) {
	return func() (string, error) { return k, nil }
}

func TestTokenURL(t *testing.T) {
	got, err := TokenURL("East US 2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const want = "https://eastus2.api.cognitive.microsoft.com/sts/v1.0/issueToken"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTokenFetchesAndSendsTheKey(t *testing.T) {
	doer := &fakeDoer{status: 200, body: "tok-1"}
	svc := NewTokenService(TokenOptions{Region: "eastus", GetKey: keyFn("secret"), Client: doer})

	got, err := svc.Token(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tok-1" {
		t.Errorf("token = %q, want %q", got, "tok-1")
	}
	if doer.lastKey != "secret" {
		t.Errorf("key header = %q, want %q", doer.lastKey, "secret")
	}
}

func TestTokenCachesWithinTTL(t *testing.T) {
	doer := &fakeDoer{status: 200, body: "tok-1"}
	now := time.Unix(0, 0)
	svc := NewTokenService(TokenOptions{
		Region: "eastus", GetKey: keyFn("secret"), Client: doer,
		Now: func() time.Time { return now }, TTL: time.Minute,
	})

	for i := 0; i < 3; i++ {
		if _, err := svc.Token(context.Background(), false); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if doer.calls != 1 {
		t.Errorf("expected 1 HTTP call for 3 cached reads, got %d", doer.calls)
	}
}

func TestTokenRefetchesAfterExpiry(t *testing.T) {
	doer := &fakeDoer{status: 200, body: "tok"}
	now := time.Unix(0, 0)
	svc := NewTokenService(TokenOptions{
		Region: "eastus", GetKey: keyFn("secret"), Client: doer,
		Now: func() time.Time { return now }, TTL: time.Minute,
	})

	if _, err := svc.Token(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := svc.Token(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if doer.calls != 2 {
		t.Errorf("expected a refetch after expiry, got %d calls", doer.calls)
	}
}

// The mid-session refresh depends on this: handing the recognizer the token it already
// holds does nothing about the fact that it is about to expire.
func TestTokenForceBypassesCache(t *testing.T) {
	doer := &fakeDoer{status: 200, body: "tok"}
	svc := NewTokenService(TokenOptions{Region: "eastus", GetKey: keyFn("secret"), Client: doer})

	if _, err := svc.Token(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Token(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if doer.calls != 2 {
		t.Errorf("force should bypass the cache, got %d calls", doer.calls)
	}
}

func TestTokenWithoutAKeyFailsBeforeAnyRequest(t *testing.T) {
	doer := &fakeDoer{status: 200, body: "tok"}
	svc := NewTokenService(TokenOptions{Region: "eastus", GetKey: keyFn("   "), Client: doer})

	_, err := svc.Token(context.Background(), false)
	if !errors.Is(err, ErrNoKey) {
		t.Errorf("got %v, want ErrNoKey", err)
	}
	if doer.calls != 0 {
		t.Errorf("should not have called the service, got %d calls", doer.calls)
	}
}

func TestTokenMapsAuthFailures(t *testing.T) {
	for _, status := range []int{401, 403} {
		doer := &fakeDoer{status: status}
		svc := NewTokenService(TokenOptions{Region: "eastus", GetKey: keyFn("bad"), Client: doer})
		_, err := svc.Token(context.Background(), false)
		if !errors.Is(err, ErrBadCredentials) {
			t.Errorf("status %d: got %v, want ErrBadCredentials", status, err)
		}
	}
}

func TestTokenReportsOtherHTTPErrors(t *testing.T) {
	doer := &fakeDoer{status: 500}
	svc := NewTokenService(TokenOptions{Region: "eastus", GetKey: keyFn("k"), Client: doer})
	_, err := svc.Token(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("got %v, want an error mentioning 500", err)
	}
}

func TestTokenRejectsAnInvalidRegionBeforeRequesting(t *testing.T) {
	doer := &fakeDoer{status: 200, body: "tok"}
	svc := NewTokenService(TokenOptions{Region: "east/us", GetKey: keyFn("k"), Client: doer})
	if _, err := svc.Token(context.Background(), false); err == nil {
		t.Error("expected an error for an invalid region")
	}
	if doer.calls != 0 {
		t.Errorf("should not have called the service, got %d calls", doer.calls)
	}
}

// The five reasons a test can fail are the caller's whole vocabulary for wording it, so they are
// asserted exactly. Getting one wrong is not cosmetic: reporting a 500 as a network failure tells
// someone to check their internet while Azure is the thing that is broken.
func TestConnectionClassifiesEveryOutcome(t *testing.T) {
	cases := []struct {
		name   string
		region string
		key    string
		doer   Doer
		wantOK bool
		want   ConnFailure
	}{
		{"accepted", "eastus", "good", &fakeDoer{status: 200, body: "tok"}, true, ""},
		{"no key", "eastus", "  ", &fakeDoer{status: 200}, false, ConnNoKey},
		{"unusable region", "east/us", "good", &fakeDoer{status: 200}, false, ConnBadRegion},
		{"rejected", "eastus", "bad", &fakeDoer{status: 401}, false, ConnBadCredentials},
		{"forbidden", "eastus", "bad", &fakeDoer{status: 403}, false, ConnBadCredentials},
		{"azure broken", "eastus", "good", &fakeDoer{status: 500}, false, ConnService},
		{"unreachable", "eastus", "good", &errDoer{err: errors.New("dial tcp: no such host")}, false, ConnNetwork},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TestConnection(context.Background(), c.region, c.key, c.doer)
			if got.OK != c.wantOK {
				t.Fatalf("OK = %v, want %v (error %q)", got.OK, c.wantOK, got.Error)
			}
			if got.Kind != c.want {
				t.Errorf("Kind = %q, want %q", got.Kind, c.want)
			}
			if c.wantOK && got.Kind != "" {
				t.Errorf("a success carries Kind %q — nothing failed", got.Kind)
			}
		})
	}
}

// errDoer fails the transport itself: the request never reaches Azure.
type errDoer struct{ err error }

func (d *errDoer) Do(*http.Request) (*http.Response, error) { return nil, d.err }

func TestTestConnectionOK(t *testing.T) {
	got := TestConnection(context.Background(), "eastus", "good", &fakeDoer{status: 200, body: "tok"})
	if !got.OK {
		t.Errorf("got %+v, want OK", got)
	}
}

func TestTestConnectionMissingKey(t *testing.T) {
	got := TestConnection(context.Background(), "eastus", "  ", &fakeDoer{status: 200})
	if got.OK || !strings.Contains(got.Error, "clave") {
		t.Errorf("got %+v, want a missing-key message", got)
	}
}

func TestTestConnectionBadCredentials(t *testing.T) {
	got := TestConnection(context.Background(), "eastus", "bad", &fakeDoer{status: 401})
	if got.OK || !strings.Contains(got.Error, "401/403") {
		t.Errorf("got %+v, want the invalid key/region message", got)
	}
}

func TestTestConnectionInvalidRegion(t *testing.T) {
	got := TestConnection(context.Background(), "east/us", "k", &fakeDoer{status: 200})
	if got.OK || got.Error == "" {
		t.Errorf("got %+v, want a region error", got)
	}
}

func TestTestConnectionNetworkError(t *testing.T) {
	got := TestConnection(context.Background(), "eastus", "k", &fakeDoer{err: errors.New("no route to host")})
	if got.OK || !strings.Contains(got.Error, "no route") {
		t.Errorf("got %+v, want the transport error surfaced", got)
	}
}
