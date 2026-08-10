# Bound reconnect retries across successful short-lived connections

## Symptom

A provider that connected successfully, emitted `Started`, and then failed retryably could reconnect
forever. Each replacement connection repeated the same sequence, so a metered STT service could keep
billing without producing a transcript.

## Root cause

The controller incremented `reconnectAttempt` on `Canceled`, but reset it on every `Started`. The
`maxReconnects` comparison therefore bounded only failures that happened before a connection opened.
The existing cap test missed the defect because it sent consecutive cancels without the intervening
`Started` emitted by a real successful reconnect.

## Fix

- Reset the counter only at `doStartLocked`, the boundary of a new user dictation.
- Keep reconnect generations inside the same cumulative budget; six retryable failures schedule the
  existing 1, 2, 4, 8, 16, and 30 second delays, and the seventh stops.
- Emit `ReconnectExhausted(maxReconnects)` through the controller IO seam so the app log distinguishes budget
  exhaustion from an immediately terminal failure.
- Ignore a late `Started` after a pending or terminal stop so it cannot repaint the final error as
  listening.
- Classify Grok's discriminator-free in-socket `error` as `ServiceError`; it is now retryable only
  behind the hard controller bound. Handshake auth/config failures remain terminal.

## Tradeoffs

The budget and backoff are cumulative for the whole dictation. Six sparse drops in a very long
dictation therefore inherit the 8/16/30 second waits and eventually require the user to trigger a new
dictation. An ambiguous permanent Grok in-socket error can consume six extra connections and about
61 seconds of backoff before it surfaces terminally. Those costs are bounded and preferred to an
open-ended billing loop.

OpenAI and ElevenLabs remain terminal for generic runtime error events. They preserve structured
post-upgrade codes/event names, but their provider `handle` methods still collapse those signals into
one `BadRequest` bucket. That mapping needs its own provider-specific fix: making the bucket retryable
would also retry known-permanent auth and quota failures.

## Verification

- The lifecycle regression was red before the fix: seven reconnects, seven 1-second delays, no stop,
  no terminal overlay, and no transcript flush.
- The Grok classification test was red as `BadRequest` / config and green as `ServiceError` / bounded
  retry.
- Mutation checks reintroduced the `Started` reset, removed the new-session reset, consumed budget on
  a stale cancel, removed the pending-callback `Desired()` guard, and removed the terminal
  `!Desired()` guard. Every mutation failed its intended regression and was restored.
- A code-review finding added the late-`Started` regression; it failed by repainting the stopped
  session as listening, then passed with the desired-state guard.
- `internal/session`, `internal/stt/grok`, and `internal/app` passed uncached; `internal/session`
  passed under the race detector.
- On 2026-08-07, `./scripts/task.sh check` exited 0 on the final implementation: all Go tests,
  `go vet`, and the frontend type-check passed. The linker emitted the repository's existing macOS
  deployment-target/rpath warnings but no failure.
- A repository sweep confirmed `reconnectAttempt` has only the cap and backoff runtime readers and
  feeds no overlay copy. `ScheduleReconnect` has one production caller in the controller; all IO
  implementers compile against the new exhaustion signal. Historical plans retain the old defect as
  history, while current OpenAI/ElevenLabs comments now state their distinct structured-code mapping
  residual.

## Residual

The reconnect callback reads `Desired()` under the controller mutex, unlocks, and then calls
`StartEngine`. A stop can land in that narrow interval. Holding the mutex across provider IO would
reintroduce the synchronous-callback deadlock documented on `Controller`, so the residual belongs with
the planned shared reconnect/capture lifecycle refactor rather than this accounting fix. If the race
wins, a metered provider can open after the controller stopped owning the session and may remain live
until its own failure/timeout path closes it.
