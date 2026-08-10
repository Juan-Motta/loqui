# Own reconnect resources by generation

## Symptom

A retryable provider failure scheduled a replacement without first closing the failed provider and
microphone. `Dictation.StartEngine` then overwrote the app's only provider, capture, and pump handles,
so the superseded pipeline could survive without an owner. Separately, a user stop could land after a
reconnect callback read `Desired()` but before that callback called `StartEngine`, allowing a stopped
generation to open resources late.

## Root cause

Controller generations gated provider events, but app resources and timers had no generation
ownership. `Dictation` stored one mutable set of top-level handles, reconnect timers carried no target
generation, and the idle guard could stop whatever generation happened to be current when it fired.

## Fix

- The controller queues `StopEngine(failedGeneration)` before it schedules the replacement backoff.
- `Dictation` owns one generation-scoped `engineRun` containing the provider, capture, audio pump,
  meter peak, and idle guard.
- A monotonic `stoppedThrough` tombstone rejects a start or resource attachment that arrives after its
  generation was stopped.
- Provider and capture publication are transactional: late resources are closed instead of becoming
  detached handles.
- The pump re-checks run ownership before every provider push, so capture-buffered frames cannot drain
  after detach; activity updates are generation-scoped for the same reason.
- Reconnect waits carry their target generation and identity. Stop, replacement, shutdown, and even a
  callback that fires after timer cancellation all re-check ownership before running.
- Idle guards call `StopByGuard(generation)`, and the controller makes the generation check atomically
  with its stop decision.

## Tradeoffs

No audio is recorded or buffered during reconnect backoff: the old microphone is closed before the
wait and a replacement opens only when the retry starts. Provider `Stop` remains asynchronous by
contract, but ownership is transferred and the stop is requested before the timer is installed. The
extra run/timer state makes lifecycle rules explicit while leaving retry delays, the six-retry budget,
transcript accumulation, provider error policy, and public `stt.Provider` contract unchanged.

## Verification

- The controller regression failed before the fix with
  `retry effects = [schedule], want [stop:1 schedule]`, then passed with stop-before-schedule ordering.
- Provider, capture, timer, and idle regressions were written before their production seams and failed
  on the missing generation ownership.
- Focused suites, all `internal/app` tests, and the combined `internal/session` + `internal/app` race
  suite passed after the implementation.
- Mutation checks removed each critical guard in turn: retry stop ordering, stopped-generation start
  rejection, capture attachment validity, stale-stop generation gating, and reconnect wait
  identity/tombstone validation, plus the per-frame pump and activity gates. Every mutation failed its
  intended regression and returned to GREEN after restoration.
- Cross-review and the final repository gate are recorded in `.workflow/state.md` on the exact tree
  that ships.
- On 2026-08-07, `./scripts/go.sh test ./internal/session ./internal/app ./internal/stt/... -count=1`,
  `./scripts/go.sh test -race ./internal/session ./internal/app -count=1`, and
  `./scripts/task.sh check` all exited 0. The full gate emitted only the repository's existing macOS
  deployment-target and duplicate-rpath linker warnings.
