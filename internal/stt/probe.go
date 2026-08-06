package stt

// ProbeResult is what "test this credential" answers. It lives here, in the contract package, because
// four providers produce one and internal/app consumes it — and because a plain struct adds no
// dependency, so this package still imports nothing.
//
// THREE OUTCOMES, NOT NINE. An earlier design mapped every documented failure of every service into a
// category (Quota, RateLimited, AccountAction…). That collapsed under measurement: OpenAI returns 429
// for both rate limits and exhausted credit, ElevenLabs' decoded events carried no name to key off, and
// Azure groups 401 and 403 into one credential verdict — and not one of those categories could be
// provoked without a paid account, so eight of the nine were untestable. What a user needs is whether
// the key works; when it does not, the provider's own short code says more than a bucket we guessed.
type ProbeResult struct {
	// OK is true only when a readiness message arrived and was recognised. Never inferred from a clean
	// close: ElevenLabs closes with code 1000 AFTER refusing a credential, so silence and tidy
	// shutdowns are failures. See docs/research/2026-08-06-where-realtime-stt-auth-fails.md.
	OK bool
	// Kind says which of the three answers this is.
	Kind ProbeKind
	// Message is for the user, already in Spanish, and never contains the credential. For a rejected
	// key it is OUR wording rather than the provider's: the services were measured echoing key
	// material back, masked in the middle with the tail intact.
	Message string
	// Detail is technical text for the LOG and never for the user: Go's transport error, the socket
	// that refused, the DNS failure. It is English, it is about sockets rather than about anything a
	// person can act on, and — like everything the server writes — it must be treated as possibly
	// carrying whatever the caller sent. Callers log it; nothing paints it.
	Detail string
	// Code is the provider's own machine-readable string when it gave one — "invalid_api_key",
	// "insufficient_quota", "server_error", a close code. Short, non-prose, no key material, and
	// exactly what a user would search for. Empty when the provider offered nothing.
	Code string
}

// ProbeKind is the answer to "is this credential usable".
type ProbeKind int

const (
	// ProbeNoKey means there was nothing to test. The network is never touched.
	ProbeNoKey ProbeKind = iota
	// ProbeOK means the service accepted the credential and opened a session.
	ProbeOK
	// ProbeKeyRejected means the service refused the credential — the one verdict that tells the user
	// exactly what to fix.
	ProbeKeyRejected
	// ProbeFailed is everything else: the service was unreachable, it answered something unexpected, it
	// closed early, or it never answered. Code carries whatever it did say.
	ProbeFailed
)

func (k ProbeKind) String() string {
	switch k {
	case ProbeNoKey:
		return "no-key"
	case ProbeOK:
		return "ok"
	case ProbeKeyRejected:
		return "key-rejected"
	default:
		return "failed"
	}
}
