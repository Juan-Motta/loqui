// Policy for long-session robustness: classify a cancellation, decide whether
// reconnecting can possibly help, compute backoff, and detect an idle session. Ported
// from the Electron build's src/shared/sessionPolicy.ts.
package session

import (
	"math"
	"regexp"
	"strings"
	"time"
)

// CancelClass is why a session was cancelled, reduced to what the retry decision needs.
type CancelClass string

const (
	// ClassAuth: the credential is wrong. Retrying cannot fix it.
	ClassAuth CancelClass = "auth"
	// ClassConfig: something only the user can fix — no key, no region, a helper that
	// isn't built, the whisper model not downloaded.
	ClassConfig CancelClass = "config"
	// ClassNetwork: transient. Backoff and retry is the right response.
	ClassNetwork CancelClass = "network"
	// ClassOther: unknown. Treated as retryable, but capped by the attempt limit.
	ClassOther CancelClass = "other"
)

// errorCodeClass maps a provider's STRUCTURED cancellation code to a class. This is the
// authoritative signal; the text heuristic below is only a fallback for cancels that
// carry no code.
var errorCodeClass = map[string]CancelClass{
	"AuthenticationFailure": ClassAuth,
	"Forbidden":             ClassAuth,
	"BadRequest":            ClassConfig, // malformed request — retrying won't help

	// NotConfigured is ours, not Azure's: no API key, no region, a native helper that
	// isn't compiled, the whisper model missing. Retrying can never resolve any of them.
	//
	// It exists because of a real bug. classifyText below reads the error MESSAGE, and
	// once the app was translated the same failure classified differently per language:
	// the Spanish text matched "configura" and stopped, the English text fell through to
	// "other" and reconnected forever. A structured code makes the decision independent
	// of the user's language. Never reintroduce prose-matching as the primary signal.
	"NotConfigured": ClassConfig,

	"ConnectionFailure":  ClassNetwork,
	"ServiceTimeout":     ClassNetwork,
	"ServiceUnavailable": ClassNetwork,
	"TooManyRequests":    ClassNetwork, // throttled: backoff is exactly right

	"ServiceError": ClassOther,
	"RuntimeError": ClassOther,
	"NoError":      ClassOther,
}

var (
	authStatusRe = regexp.MustCompile(`(^|[^0-9])(401|403)([^0-9]|$)`)
	authWordRe   = regexp.MustCompile(`forbidden|unauthor|authentication`)
	configRe     = regexp.MustCompile(`configura|no configurado|not ready|not configured|missing (region|key|clave)`)
	networkRe    = regexp.MustCompile(`1006|connection|network|timeout|enotfound|socket|disconnect`)
)

// classifyText is the FALLBACK for cancels with no structured code. Kept because
// providers and thrown errors do not always supply one, but never consulted when a code
// is present — see the NotConfigured note above for what happens when prose drives
// decisions.
func classifyText(text string) CancelClass {
	s := strings.ToLower(text)
	switch {
	case authStatusRe.MatchString(s) || authWordRe.MatchString(s):
		return ClassAuth
	case configRe.MatchString(s):
		return ClassConfig
	case networkRe.MatchString(s):
		return ClassNetwork
	default:
		return ClassOther
	}
}

// Cancel is a cancellation to classify.
type Cancel struct {
	// ErrorCode is the structured reason. Preferred, and sufficient on its own.
	ErrorCode string
	// Error is the human-readable message. May be localised, so it is only a fallback.
	Error string
}

// ClassifyCancel decides what kind of failure this was.
func ClassifyCancel(c Cancel) CancelClass {
	if c.ErrorCode != "" {
		if class, ok := errorCodeClass[c.ErrorCode]; ok {
			return class
		}
		// An unknown code (a newer SDK, another provider) is not a reason to give up
		// on the structured path — fall through to the text, as the original did.
	}
	return classifyText(c.Error)
}

// ShouldReconnect reports whether retrying could possibly succeed.
func ShouldReconnect(class CancelClass) bool {
	return class != ClassAuth && class != ClassConfig
}

// BackoffOptions bounds exponential backoff.
type BackoffOptions struct {
	Base time.Duration
	Max  time.Duration
}

// Backoff returns the delay before attempt n (0-based), doubling and capped.
func Backoff(attempt int, opts BackoffOptions) time.Duration {
	base, max := opts.Base, opts.Max
	if base <= 0 {
		base = time.Second
	}
	if max <= 0 {
		max = 30 * time.Second
	}
	if attempt < 0 {
		attempt = 0
	}
	// Guard the shift: a large attempt count would overflow into a negative duration
	// and turn the backoff into "retry immediately, for ever".
	if attempt > 30 {
		return max
	}
	d := time.Duration(math.Min(float64(base)*math.Pow(2, float64(attempt)), float64(max)))
	return d
}

// IsIdleExpired reports whether nothing has happened for longer than the limit. It caps
// billing on a session left open in silence — a cloud recognizer streaming an empty room
// costs money and produces nothing.
func IsIdleExpired(lastActivity, now time.Time, limit time.Duration) bool {
	return now.Sub(lastActivity) > limit
}
