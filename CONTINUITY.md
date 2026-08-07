# Continuity — session handoff

- **Focus:** the Loqui Electron/TypeScript port to Go + Wails v3 is finished. The bounded reconnect
  budget fix is complete on `fix/bounded-reconnect-budget`: one dictation schedules at most six
  reconnects across successful short-lived connections, Grok in-socket errors use bounded
  `ServiceError`, and a late terminal `Started` cannot repaint the error overlay.

- **NEXT STEP:** finish integrating `fix/bounded-reconnect-budget` according to the owner's selected
  branch option. After it lands, start the next bug red in `internal/app`: a retryable reconnect calls
  `Dictation.StartEngine` without stopping the previous capture, so the old capture/pump can survive
  while the new one overwrites `d.capture` and `d.pumpDone`.

- **Active workflow:** `.workflow/state.md` (`fix-bug`, phase `ship`). The plan is
  `docs/plans/bounded-reconnect-budget.md`; the durable diagnosis is
  `docs/solutions/bounded-reconnect-budget.md`. Design review converged in five rounds; code review in
  three. `./scripts/task.sh check` exited 0 on 2026-08-07.

- **Blockers — owner decisions, not code:** choose a fixed self-signed identity vs Developer ID;
  confirm Apple SpeechAnalyzer transcription with a voice/file input; remove the orphan
  `com.jualopezmo.loquigo` / `azure-speech` Keychain item.

- **Handoff notes:** OpenAI and ElevenLabs preserve structured post-upgrade error codes but their
  runtime providers still collapse them into one terminal bucket; map those codes before making any
  subset retryable. The reconnect callback also has a documented narrow TOCTOU between its locked
  `Desired()` read and `StartEngine`; solve it with the shared reconnect/capture lifecycle refactor,
  not by holding the controller mutex across reentrant provider IO.

- **Updated:** 2026-08-07
