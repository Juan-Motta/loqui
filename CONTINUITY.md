# Continuity — session handoff

- **Focus:** reconnect lifecycle fix complete and ready for local integration into `main`. Failed
  providers/captures close before backoff, buffered frames stop at detach, late starts are rejected,
  and reconnect/idle timers are generation-gated.

- **NEXT STEP:** choose and configure a stable signing identity (fixed self-signed identity or
  Developer ID). Ad-hoc rebuilds still revoke Accessibility and Input Monitoring permissions.

- **Active workflow:** `.workflow/state.md` (`fix-bug`, phase `ship`). Design:
  `docs/superpowers/specs/2026-08-07-reconnect-capture-lifecycle-design.md`. Durable diagnosis:
  `docs/solutions/reconnect-capture-lifecycle.md`. The external reviewers returned no valid verdict,
  so the recorded delayed self-review waiver found and fixed one P1 plus one P2, then converged clean.
  Seven mutation checks detect their intended regressions. The affected tests, race detector,
  `./scripts/task.sh check`, and all workflow gates exited 0 on 2026-08-07. The owner selected local
  merge; do not push or create a PR unless requested separately.

- **Blocker:** the owner must choose fixed self-signed identity or Developer ID before the signing
  work begins.

- **Provider residual:** OpenAI and ElevenLabs preserve structured post-upgrade error codes but their
  runtime providers still collapse them into one terminal bucket. Map those codes before making any
  subset retryable.

- **Updated:** 2026-08-07
