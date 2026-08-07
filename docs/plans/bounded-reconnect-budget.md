# Bounded reconnect budget

## Goal

Guarantee that one user dictation can schedule at most `maxReconnects` retries, even when every
replacement provider connection emits `Started` before failing. Once the budget is exhausted, stop
the session, show the terminal error, and preserve any transcript already recognized. With that hard
bound in place, classify xAI/Grok's structurally ambiguous in-socket `error` event as retryable
`ServiceError` instead of the current misleading non-retryable `BadRequest`.

## Verified root cause and scheduling audit

`handleCancelLocked` first compares `reconnectAttempt >= maxReconnects`; only when the budget remains
does it increment and schedule. Thus attempts 1–6 are scheduled and the seventh retryable cancel
stops without incrementing. The defect is that the `stt.Started` branch resets the counter after every
successful WebSocket handshake. The existing cap test sends consecutive `Canceled` events without
intervening `Started` events, so it never exercises the real reconnect lifecycle. A regression test
that runs complete `Started -> Canceled` cycles currently schedules seven retries, never stops, and
leaves the session desired.

The repository audit establishes the chokepoint behind the hard-bound claim:

- `Controller.handleCancelLocked` contains the only production call to `ScheduleReconnect`.
- `internal/app.Dictation.ScheduleReconnect` only implements that callback with one replaceable
  `time.AfterFunc`; it does not decide or recursively schedule retries.
- `reconnectAttempt` has exactly three writers: reset at `doStartLocked`, the defective reset at
  `stt.Started`, and increment at the `handleCancelLocked` chokepoint.
- `doStartLocked` is reached only from `applyLocked(CommandStart)`. The state machine emits
  `CommandStart` only for a trigger press from idle; reconnect uses `Tracker.Bump` and calls
  `StartEngine` directly, so it cannot reset through `doStartLocked`.
- The complete production `StartEngine` call graph has two controller sites: `doStartLocked` for a
  new user session (must reset), and the scheduled callback inside `handleCancelLocked` for a
  continuation (must not reset). `internal/app.Dictation.StartEngine` is the IO implementation, not
  a third decision path. Device/config changes do not call it directly.
- Every provider failure reaches the controller as a generation-tagged `stt.Canceled`; the
  controller rejects stale generations before entering `handleCancelLocked`.
- `internal/app.Dictation.StopEngine` calls `clearTimers`, which stops the armed reconnect timer.
  If the timer callback has already won the race, the callback closure owned by
  `Controller.handleCancelLocked` acquires `c.mu`, reads `tracker.Desired()` while serialized, releases
  the mutex, and only then calls `IO.StartEngine` if still desired. A stopped session therefore cannot
  reopen through a stale timer and the read has no data race. This existing guard belongs to
  `internal/session`, not the app timer adapter, and is tested by invoking the controller fake IO's
  captured callback through that locked path.
- `handleCancelLocked` begins with `if !tracker.Desired() { return }`. Exhaustion calls
  `doStopLocked`, which makes `Desired()` false; therefore a duplicate `Canceled` for the still-current
  terminal generation cannot re-enter stop, exhaustion signalling, or transcript flush.

## Constraints and success criteria

- The budget is a hard billing bound for one user-initiated dictation.
- A new user-initiated dictation gets a fresh full budget.
- A single user dictation may schedule at most `maxReconnects` reconnects in total, regardless of
  intervening `Started` events or failures before `Started`.
- Stale-generation events consume no budget.
- Giving up keeps the existing stop, overlay-error, and exactly-once transcript-flush semantics.
- Structured authentication and configuration failures from the handshake remain non-retryable.
- Grok in-socket `error` events become bounded-retry `ServiceError`. Their schema contains only
  provider prose and no structured discriminator, so transient and permanent cases cannot be split
  safely without reintroducing locale/provider-prose-driven retry policy. The conscious trade is up
  to six retries and 61 seconds of backoff plus handshake time for an ambiguous permanent in-socket
  error, including in-socket auth/quota prose, never an unbounded loop. Only HTTP handshake
  auth/config failures have structured terminal classifications.
- No provider/network API changes and no WebSocket lifecycle refactor are in scope.

## Approach comparison

| Approach | Complexity | Blast radius | Reversibility | Time to validate | Correctness/user risk |
| --- | --- | --- | --- | --- | --- |
| **A. Session-scoped cumulative budget**: reset only in `doStartLocked`, never on provider `Started` | Low: remove one reset | Low: controller plus Grok classification | High if the counter fix and Grok classification are reverted together | Low: deterministic controller tests | Low: hard cost bound; later isolated drops also inherit longer cumulative backoff, and after six drops a long session stops and needs a manual retrigger |
| **B. Recognition-based reset plus a separate absolute per-session ceiling** | Med: two counters and cross-provider “healthy” semantics | Med: controller policy and more event cases | Med | Med: two interacting limits | Med-low: retains a hard ceiling, but noisy/empty activity complicates the soft reset and UX |
| **C. Reset after recognition activity with no absolute ceiling** | Med: define cross-provider “healthy” activity | Med: provider event semantics differ | Med | Med: provider-specific cases | High: noisy/empty activity can reopen the unlimited billing loop |
| **D. Reset after a stable-time threshold** | High: timer ownership, cancellation, and concurrency | High: controller/IO timer contract changes | Low | High: timing and race tests | High: weakens the hard bound and can still retry forever over time |

## Chosen approach

Choose **A, a cumulative budget per user dictation**. It is the simplest option that preserves the
documented cap as a hard upper bound rather than a heuristic. `doStartLocked` is the verified user
session boundary and already resets the counter there; `Tracker.Bump` keeps reconnects inside that
session. The hybrid B can be revisited only if real long-dictation dropout reports justify its extra
state; the initial safety fix should not add a second retry policy.

Keep `maxReconnects == 6`: this is the existing product policy and yields bounded backoffs of
1, 2, 4, 8, 16, and 30 seconds before stopping. Changing the limit while fixing its accounting would
confound the regression. Grok's newly retryable ambiguous error consumes the same budget rather than
creating another one.

The delay index remains the same session-cumulative counter as the safety budget. That means even
widely separated drops wait 8, 16, then 30 seconds on attempts 4–6. This is accepted for the first
safety fix: decoupling fast-recovery backoff from the absolute billing ceiling requires the second
counter in approach B and is deferred until observed long-session behavior justifies it.

The counter fix and Grok classification ship as one atomic change. The repository handoff explicitly
couples them, and `ServiceError` is safe only once the hard bound exists. Separate commits would permit
shipping or reverting into the known unbounded combination; logical separation remains visible in
the tests and diff.

## Implementation units

1. In `internal/session/controller_test.go`, retain the failing regression that performs
   `maxReconnects + 1` complete short-lived connection cycles. Recognize text on at least two
   generations including the terminal generation, and assert exactly `maxReconnects` schedules, one
   stop, `Desired() == false`, one joined transcript delivery with no loss or duplication, and exactly
   one error-tier overlay whose state is last. Assert one `ReconnectExhausted(maxReconnects)` IO
   signal so exhaustion is diagnosable independently of the provider's error prose. Pin reconnect
   delays to `1s, 2s, 4s, 8s, 16s, 30s` and the joined transcript to generation order with the terminal
   generation's text last. Then send a duplicate retryable `Canceled` for the exhausted current
   generation and assert no second stop, signal, overlay error, or delivery.
2. Strengthen the pre-`Started` cap test to assert exactly `maxReconnects` schedules followed by a
   stop. Add focused controller tests proving a stale cancel consumes no unit and a fresh user
   dictation receives the full budget after exhaustion. Add a pending-callback test that stops the
   session, invokes the captured controller reconnect callback, and asserts no new engine start and
   no exhaustion signal. Extend existing auth/config-cancel and normal user-stop tests to assert zero
   exhaustion signals. These are characterization tests; mutation checks must demonstrate that
   moving/removing the user-session reset, incrementing before the generation gate, removing the
   callback's locked `Desired()` check, or removing the terminal `!tracker.Desired()` guard makes them
   fail.
3. In `internal/session/controller.go`, remove the reconnect counter reset from the `stt.Started`
   branch. Keep the sole reset at the new-session boundary in `doStartLocked`; do not add a second
   scheduling path. Preserve the existing compare-before-increment shape: at the top of the retryable
   path, `if reconnectAttempt >= maxReconnects`, queue `ReconnectExhausted(reconnectAttempt)`, stop,
   and return; otherwise increment, then schedule with backoff index `reconnectAttempt - 1`. Do not
   increment on the terminal cancel; the signal parameter means successful schedules consumed and
   equals `maxReconnects`. Do not signal auth/config or user termination. Preserve the existing
   controller-mutex acquisition around the callback's `Desired()` recheck. Mechanically find and
   update every `session.IO` implementer (app `Dictation`, controller fakes/reentrant adapters, and
   the Grok controller fake).
   Keep the exhaustion count parameter for diagnostic clarity and future configurability even though
   it currently always equals the constant at the terminal branch.
4. In `internal/stt/grok/errors_test.go`, change the in-socket server-error expectation to assert
   both the exact `ServiceError` code and `ShouldReconnect == true`. Run it red against the current
   `BadRequest` mapping. Keep the existing handshake tests proving bad credentials and generic bad
   requests remain non-retryable.
5. In `internal/stt/grok/errors.go`, map `serverErrorCode` to `ServiceError` and replace the obsolete
   safety comment with the bounded-retry invariant. Do not classify provider prose.
6. In `internal/app/dictation.go`, implement `ReconnectExhausted` as a `RECONNECT` log that states the
   exhausted attempt count. The controller stays pure while the app exposes why the session stopped.
7. Run the targeted session and Grok tests, the neighboring provider/session packages, and the full
   `./scripts/task.sh check` gate. Record the result in `docs/solutions/bounded-reconnect-budget.md`.
   Add a concise entry to `docs/CHANGELOG.md` at ship time. A new controller logging surface is out of
   scope: it emits the typed IO signal and the app owns the log. Sweep code/docs for `maxReconnects`,
   `reconnectAttempt`, and reconnect-policy prose so no text describes the old per-connection reset.
   Confirm its only runtime readers remain the cap comparison and backoff calculation; none feeds
   user-facing overlay copy. Record the pre-existing narrow TOCTOU window between unlocking after
   the callback's `Desired()` read and calling `StartEngine`—closing it by holding the controller lock
   across reentrant IO is unsafe and belongs with the separate reconnect/capture lifecycle refactor.

## Edge cases and test matrix

- Complete `Started -> Canceled` cycles cannot reset the budget.
- Reconnect attempts that fail before `Started` consume one unit each and stop at the same cap.
- The `(maxReconnects + 1)`th retryable cancel stops without scheduling another callback.
- A stale cancel from a superseded generation remains ignored and consumes no budget.
- A non-retryable cancel stops immediately without scheduling a retry.
- A user/non-retryable stop cancels an armed app timer; even if its callback is already running, the
  callback's `Desired()` gate prevents a new engine start.
- Auth/config and user termination emit no `ReconnectExhausted` signal.
- Starting a genuinely new dictation after exhaustion restores all `maxReconnects` retries.
- A duplicate retryable `Canceled` for the exhausted current generation is ignored by the terminal
  `!Desired()` guard and cannot repeat stop, exhaustion signal, overlay error, or transcript flush.
- Text recognized on multiple generations before terminal exhaustion is joined and flushed exactly
  once; the final overlay state is the provider error.
- Grok HTTP auth/config failures remain terminal, while its discriminator-free in-socket `error`
  event is retryable only through the bounded controller.

## Acceptance criteria

- The complete-lifecycle regression is red before production changes and green afterward.
- The Grok classification test is red before the constant change and green afterward.
- The controller has one reconnect-scheduling chokepoint (`handleCancelLocked`) and one budget reset
  at the user-session boundary (`doStartLocked`).
- Seven short-lived retryable failures schedule exactly six reconnects, then stop with
  `Desired() == false`, emit exactly one `ReconnectExhausted(6)` signal, leave exactly one
  error-tier overlay as the last overlay state, and flush the cross-generation transcript exactly
  once.
- Seven pre-`Started` retryable failures enforce the same bound.
- A stale cancel consumes no budget, and a new user action after exhaustion gets six further retry
  opportunities before stopping.
- Invoking an already-captured reconnect callback after stop does not call `StartEngine`.
- Non-retryable and user-initiated termination emit zero `ReconnectExhausted` signals.
- A duplicate terminal-generation cancel after exhaustion produces no additional side effect.
- `serverErrorCode == "ServiceError"` and its session classification is retryable; existing bad-key
  and bad-request handshake tests remain non-retryable.
- `ShouldReconnect` remains owned by `internal/session`; the Grok-package integration assertion is
  intentionally colocated with the private `serverErrorCode` so it proves the provider's exact code
  crosses that policy boundary as retryable without creating an import cycle.
- Existing session, Grok provider, policy, transcript, and generation-gating tests remain green.
- Mutation checks prove the new-session reset and stale-generation ordering tests are effective.
- Intermediate overlay behavior during bounded Grok retries remains the existing reconnecting state;
  the changelog notes that an ambiguous permanent in-socket error may take about 61 seconds plus
  handshakes to surface terminally.
- Cross-engine design and code reviews have no open P0/P1/P2 findings.
- The full project gate exits zero.
