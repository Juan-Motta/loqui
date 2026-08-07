# Reconnect capture lifecycle

## Context

When a live provider reports a retryable cancellation, the session controller advances to a new
generation and schedules `StartEngine` after backoff. It does not stop the failed generation first.
`Dictation.StartEngine` then replaces the single `provider`, `capture`, and `pumpDone` fields with the
new generation's resources. The previous microphone capture and pump can therefore survive with no
remaining handle through which the app can stop them.

There is a second race in the same ownership boundary. The reconnect timer checks that dictation is
still desired and then calls `StartEngine`. A user stop can land between those operations. The stop
runs before the late start has published any resources, so the late start can open a provider or
microphone after the controller considers the session finished.

## Goals

- Detach the failed provider, request its asynchronous stop, and close its microphone capture and
  audio pump before reconnect backoff begins.
- Make every engine resource belong to one controller generation.
- Prevent an invalidated generation from publishing resources, including resources created while a
  concurrent stop is in progress.
- Make duplicate and stale stops safe: a stale stop may clean an older run but may not affect a newer
  generation.
- Preserve the existing transcript, overlay, backoff, retry-budget, and terminal-error behavior.

## Non-goals

- Buffering or recording speech during reconnect backoff. The microphone is closed during the wait.
- Reusing one capture across providers or generations.
- Changing the six-retry budget or its 1, 2, 4, 8, 16, and 30 second delays.
- Making OpenAI or ElevenLabs runtime failures retryable; their structured error mapping remains a
  separate provider-specific change.
- Changing the public STT provider lifecycle contract.

## Approaches considered

### 1. Generation-owned engine runs — selected

Stop the failed generation before scheduling a retry, and make `Dictation` publish resources only
through a generation-scoped run. A stop invalidates the generation even when its start has not
finished. Resources created after invalidation are closed locally instead of replacing the active
run.

This is the smallest approach that fixes both the existing leak and the stop-versus-start race. It
keeps policy in `session.Controller` and resource ownership in `app.Dictation`.

### 2. Stop before retry without generation ownership

Calling `StopEngine` before `ScheduleReconnect` closes the normal leaked capture, but a timer that has
already passed its desired-state check can still start after a user stop. This treats the common
symptom but leaves the same ownership bug reachable through concurrency.

### 3. Keep capture alive and swap providers

A permanent capture could feed successive providers and buffer frames during backoff. That requires
new buffering, routing, overflow, and privacy semantics, and contradicts the approved behavior that
Loqui does not record audio while it says it is reconnecting.

## Design

### Controller ordering

For a retryable cancellation below the budget, the controller:

1. remembers the failed generation;
2. advances the tracker to the replacement generation;
3. paints the reconnecting overlay;
4. queues `StopEngine(failedGeneration)`;
5. queues the existing reconnect timer, tagged with the replacement generation.

Effects continue to run after the controller mutex is released. Their order is significant: the old
run is detached, its capture is closed, and its provider is asked to stop before the timer is
installed. `Provider.Stop` remains asynchronous by contract; the guarantee is that the app retains no
live ownership and cannot feed more audio while teardown finishes. A `Stopped` event emitted during
that teardown carries the failed generation and is stale after the tracker advance, so it cannot
flush or end the continuing dictation.

The callback's `Desired()` check remains as an inexpensive early filter. It is not the final safety
barrier; `Dictation` generation invalidation makes a start harmless if a stop wins immediately after
the check.

### Dictation ownership

`Dictation` maintains one generation-scoped engine run. The run owns:

- its generation number;
- the provider, once constructed;
- the capture, when the provider consumes host audio;
- the pump cancellation signal;
- idempotent stopped state.

`Dictation` also maintains a monotonic stopped-through generation. `StopEngine(gen)` advances that
boundary even if no run is currently published. `StartEngine(gen)` refuses to begin when `gen` is at
or below the boundary. Consequently, a stop that occurs before a delayed start leaves a tombstone the
late start must honor.

A stop for generation `N` may detach and clean a published run whose generation is at most `N`. It
must not detach a run newer than `N`; this is what makes late stops from superseded providers safe.
The reconnect timer records its target generation under the same rule: stopping `N` cancels a timer
for at most `N`, never one already targeting `N+1`. Idle-guard state belongs to the engine run it
monitors instead of being cleared globally by a stale stop.

### Start transaction

`StartEngine(gen)` first publishes a lightweight run placeholder under the dictation mutex. Provider
construction, `Provider.Start`, microphone opening, and resource closing happen outside that mutex,
because providers may report events synchronously and re-enter the controller and `StopEngine`.

After each resource-producing step, the start path attaches the resource only if its run is still the
current, valid owner. If a concurrent stop invalidated or detached the run, the start path stops the
provider or closes the capture it just created and returns. It never publishes that resource into a
newer run.

The audio pump closes over the run's own capture, provider, and cancellation signal rather than
reading mutable top-level handles. This prevents a replacement generation from changing the objects
used by an older pump.

### Stop transaction

`StopEngine(gen)` performs these operations idempotently:

1. invalidate generations through `gen` under the mutex;
2. detach the matching current run, if any;
3. cancel its pump;
4. close its capture;
5. stop its provider;
6. reset the detached run's metering and idle guard;
7. cancel a reconnect timer only when its target generation is at most `gen`.

No external call runs under the dictation mutex. Detaching ownership before cleanup ensures that a
duplicate stop sees no live handles and cannot close a channel or native capture twice.

On a retry, the controller keeps the reconnecting overlay visible while `StopEngine` resets the audio
level. A new run resets metering when the timer fires. No frames are retained or delivered during the
backoff.

### Error behavior

Provider-construction, provider-start, and microphone-open failures continue to report their existing
`Canceled` and `Stopped` events. If those callbacks synchronously trigger `StopEngine`, the run is
invalidated; the returning start path observes that state and cannot attach later resources.

Retry classification, retry exhaustion, transcript buffering, terminal overlays, and user-facing
error text do not change.

## Test strategy

Tests use provider and capture fakes through narrow app-owned construction seams; they do not open a
real microphone or contact a provider.

- A controller regression proves that a retryable cancel stops the failed generation before it
  schedules the reconnect, without hiding the overlay or delivering the buffered transcript.
- An app lifecycle regression proves that the old capture and pump are closed before the replacement
  generation opens.
- A stop-before-start regression proves that an invalidated generation constructs no provider or
  capture.
- A barrier-controlled start/stop regression proves that resources returned after invalidation are
  closed instead of published.
- A stale-stop regression proves that stopping generation `N` cannot close generation `N+1`.
- A duplicate-stop regression proves teardown is idempotent.
- Existing session regressions continue to prove transcript continuity, overlay state, exact backoff,
  six-retry exhaustion, and stale-event rejection.
- The affected app and session packages run uncached and under Go's race detector, followed by the
  repository's full `./scripts/task.sh check` gate.

The lifecycle is internal and has no sanctioned API, CLI, or UI control that can deterministically
force these interleavings, so E2E verification is expected to be recorded as not applicable with the
automated concurrency regressions as evidence.

## Tradeoffs

The chosen design adds explicit lifecycle state and two small construction seams to a class that
previously stored raw resource handles. That extra state is justified by the concurrency boundary: a
simple stop-before-retry call cannot close the late-start race. The user may lose speech spoken during
backoff, but the overlay already reports reconnecting, no hidden audio is retained, and no abandoned
capture or metered provider survives.
