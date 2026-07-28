// Short-lived Azure auth tokens and the "probar conexión" check. Ported from the
// Electron build's src/main/tokenService.ts and src/main/azureProbe.ts, which were two
// files only because one lived behind an IPC handler and the other behind a different
// one; they hit the same endpoint.
//
// WHY A TOKEN AT ALL, when the recognizer accepts a subscription key directly: the key
// is the long-lived credential for the whole Azure resource. Exchanging it for a
// 10-minute token means the value handed to the recognizer is worthless if it leaks,
// and the key itself never leaves this process.
package azure

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/settings"
)

// A token is valid for 10 minutes; refresh at 9 so a request in flight when the clock
// runs out is not the thing that discovers it.
const defaultTTL = 9 * time.Minute

// Doer is the HTTP surface used here, injected so the tests never touch the network.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// TokenURL is the regional STS endpoint that exchanges a subscription key for a token.
func TokenURL(region string) (string, error) {
	id, err := settings.NormalizeRegion(region)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://%s.api.cognitive.microsoft.com/sts/v1.0/issueToken", id), nil
}

// TokenService caches an authorization token for one region.
//
// The key is read through a function rather than stored: it lives in the keychain, and
// keeping a copy in this struct for the lifetime of the process is exactly the kind of
// long-lived plaintext secret the design avoids.
type TokenService struct {
	region string
	getKey func() (string, error)
	client Doer
	now    func() time.Time
	ttl    time.Duration

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// TokenOptions configures a TokenService. Only Region and GetKey are required.
type TokenOptions struct {
	Region string
	GetKey func() (string, error)
	Client Doer
	Now    func() time.Time
	TTL    time.Duration
}

func NewTokenService(opts TokenOptions) *TokenService {
	svc := &TokenService{
		region: opts.Region,
		getKey: opts.GetKey,
		client: opts.Client,
		now:    opts.Now,
		ttl:    opts.TTL,
	}
	if svc.client == nil {
		svc.client = &http.Client{Timeout: 10 * time.Second}
	}
	if svc.now == nil {
		svc.now = time.Now
	}
	if svc.ttl == 0 {
		svc.ttl = defaultTTL
	}
	return svc
}

// Token returns a cached token, or fetches a fresh one. Pass force to bypass the cache,
// which is what the mid-session refresh needs: handing the recognizer the SAME token it
// already has does nothing about the fact that it is about to expire.
func (s *TokenService) Token(ctx context.Context, force bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !force && s.token != "" && s.now().Before(s.expiresAt) {
		return s.token, nil
	}

	key, err := s.getKey()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(key) == "" {
		return "", ErrNoKey
	}

	url, err := TokenURL(s.region)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", key)
	req.Header.Set("Content-Length", "0")

	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("%w (%d)", ErrBadCredentials, res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return "", fmt.Errorf("issueToken HTTP %d", res.StatusCode)
	}

	body, err := readAll(res)
	if err != nil {
		return "", err
	}
	s.token = body
	s.expiresAt = s.now().Add(s.ttl)
	return s.token, nil
}

// ConnResult is the outcome of the settings screen's "probar conexión" button.
type ConnResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// TestConnection exchanges a key for a token: HTTP 200 means the credentials and the
// region are both right. It returns the failure as a message instead of an error
// because every outcome here is a normal answer to the user's question, not a fault.
func TestConnection(ctx context.Context, region, key string, client Doer) ConnResult {
	if strings.TrimSpace(key) == "" {
		return ConnResult{Error: "Falta la clave (subscription key)"}
	}
	svc := NewTokenService(TokenOptions{
		Region: region,
		GetKey: func() (string, error) { return key, nil },
		Client: client,
	})
	if _, err := svc.Token(ctx, true); err != nil {
		switch {
		case errorIs(err, ErrBadCredentials):
			return ConnResult{Error: "Clave o región inválida (401/403)"}
		default:
			return ConnResult{Error: err.Error()}
		}
	}
	return ConnResult{OK: true}
}
