package azure

import (
	"errors"
	"io"
	"net/http"
	"strings"
)

var (
	// ErrNoKey means nothing is stored in the keychain slot yet. This is a
	// configuration problem, never a transient one, so it must not be retried.
	ErrNoKey = errors.New("no stored subscription key — configure it in Ajustes")
	// ErrBadCredentials is a 401/403 from Azure: the key or the region is wrong.
	// Also not retryable — reconnecting with the same wrong key just bills nothing
	// forever.
	ErrBadCredentials = errors.New("issueToken rejected credentials (invalid key/region)")
)

func errorIs(err, target error) bool { return errors.Is(err, target) }

func readAll(res *http.Response) (string, error) {
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
