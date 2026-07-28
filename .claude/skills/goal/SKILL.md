---
name: goal
description: Drive one feature objective autonomously from idea to a PR-ready branch — prd → plan → TDD → cross-engine review → verify → ship — pausing for a human only to approve the PRD and to create the PR. Use when you want the whole workflow run hands-off/unattended under Claude Code, Codex, or OpenCode; everything between the two gates the agent decides itself.
---

# goal

Orchestrate one **feature** objective end to end with only two planned human pauses (approve the
PRD; create the PR). Between them it runs autonomously: ambiguous forks go to `council`, not the
human; non-convergence and unrecoverable failure HALT for a human. It composes the existing skills
under `owner=goal` (`shared/rules/execution.md`) — it never re-implements them — advancing the state
machine in `.workflow/state.md`. All line/state schemas are in `shared/rules/goal-state.md`.

**Honesty:** this autonomy is discipline plus a terminal Attested checkpoint (`check-gates` at
ship), not a runtime hard gate. Intermediate progress is Advisory. On all three engines the only
bad-faith-resistant gate is the fully-activated Fase 1 CI template. `/goal` cannot block a bad ship
locally — it only automates the discipline.

## 0. Classify → clean slot → initialize → preflight

1. **Classify.** If the objective is a **bug**, STOP and route the human to `/fix-bug` — v1 `/goal`
   is feature-only.
2. **Clean slot** (`shared/rules/execution.md`): if `.workflow/state.md` has a non-`done`
   `## /goal loop` OR any other unfinished workflow, STOP and ask the human to disposition it. A
   `halted` loop is **never** auto-resumed (see §4).
3. **Initialize FIRST** (so a preflight failure has a loop to mark halted): REPLACE `## Active
   workflow` + `## /goal loop` (`goal`, `status=active`, `phase=preflight`, `base_sha`=merge-base,
   `reentries=0`), reset the standard `## Ship-gate checklist` to unchecked, and clear goal-owned
   `## Review log` / `## Blockers` / `## Attempts` (schemas: `shared/rules/goal-state.md`).
4. **Capability preflight** — prove on the driver engine:
   - a non-driver reviewer engine is available (`shared/rules/models.md`); **persist the REVIEWERS
     manifest** (`shared/rules/goal-state.md`);
   - spawning the reviewer/council CLI **and** the post-GATE-2 `git push` / `gh pr create` are
     prompt-free — if not, HALT and point the human to `shared/rules/goal-autonomy-setup.md`;
   - reviewer/council output goes to stdout or under `.workflow/`;
   - **(Claude subagent-driven)** `.claude/agents/codeforge-implementer.md` exists and contains
     `commit_policy` — else HALT (stale agent; re-run codeforge setup);
   - the digest is stable across one test-suite run (`goal-digest.sh` before/after; if it moves,
     HALT naming the unignored generated paths).
   Any failure → write a `## Blockers` line, `status=halted`, and stop. Never degrade to per-round
   prompting or driver self-review.

## 1. PRD → GATE 1 (human, explicit)

Run `prd`. Set `status=awaiting-gate1`, ask the human to approve **explicitly** (`AskUserQuestion`
on Claude; a plain prompt elsewhere). On approval write `gate1` (`shared/rules/goal-state.md`) and
`status=active`. Do not enter the middle without a `gate1` record. Tick the "Plan written and
design-reviewed" box only after the plan-review loop certifies (below).

## 2. Autonomous middle (no human pauses; council for forks)

Advance `phase`, ticking each ship-gate box as its phase completes:

- **research** (only if external tech).
- **plan-review loop** — run `plan` for the plan doc, then orchestrate `review` over it (reviewer ≠
  driver). `/goal` owns the loop + logging: append a `## Review log` line per round
  (`goal-state.md`); certification = the REVIEWERS set clean at one digest; HALT if the `kind=round`
  count reaches `N` (4) — `sh shared/scripts/goal-state.sh round-count plan`.
- **tdd** — `commit_policy=defer` (implementers stage only, never commit; write the `step` cursor).
- **code-review loop** — review rounds (reviewer + self-pass) → first clean pass → `simplify`
  **once** (record the SIMPLIFY marker) → one re-cert pass → certify. Breaker: HALT at `round-count
  code` == `N`. Global cap: HALT if `reentries >= 3` or code rounds `>= 3·N`.
- **verify** — `verify-e2e` (or honest `N/A`); write the report path into the E2E box. **Any
  post-cert change to a digest-covered path** → `reentries++`, re-enter code-review (simplify does
  not re-run).
- **Ambiguity** → `council`; **non-convergence / unrecoverable / unavoidable ask-tier** → HALT.

## 3. Ship → GATE 2 (human, explicit) — commit BEFORE the gate

With every ship-gate box already ticked from its phase, and the E2E report bound:

1. `base=$(sh shared/scripts/goal-state.sh field base_sha)`; confirm
   `sh shared/scripts/goal-digest.sh "$base" .` == the certification digest, AND
   `sh shared/scripts/check-gates.sh` is green. (PowerShell: `pwsh shared/scripts/goal-digest.ps1 "$base" .`.)
2. Write the CHANGELOG (digest-neutral).
3. **Single commit** (stage the digest-covered set ∪ CHANGELOG + the E2E report).
4. Assert `sh shared/scripts/goal-digest.sh "$base" . --from-head` == the certification digest AND
   the report + CHANGELOG are in HEAD — mismatch → HALT (never GATE 2 over an unproven commit).
5. `status=awaiting-gate2` → ask the human to approve the PR **explicitly** → write the GATE 2
   authorization line (`goal-state.md`; action/head/branch/remote/ts — **no nonce**) → re-verify the
   committed head → push → open the PR → `status=done`.

**Recovery:** on a ship-side `check-gates` red, `sh shared/scripts/goal-state.sh ship-red-bump`;
`n>=2` → HALT. A red-path fix that touches a digest-covered path → re-enter code-review
(`reentries++`). GATE 2 **declined / changes requested** → soft-reset the unpushed commit, return to
`<owning-phase>` with `status=active`. Committed-head **drift** after authorization → invalidate the
GATE 2 line and revalidate (re-check committed digest + gates) before a fresh GATE 2.

## 4. State + resume

`## /goal loop` is the source of truth (`shared/rules/goal-state.md`). On a mid-session compaction,
re-orient from the summary + a Read of `## /goal loop` and continue from `phase`/`step` — never
restart a completed phase, never enter the middle without a valid `gate1`. **`status=halted` is
terminal for automation**: `/goal` does not resume it; a human takes over interactively and must
disposition/reset the run before a new `/goal` invocation. This is best-effort in-session
continuity, not durable cross-session resume (a v2 follow-up).

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "It's basically a feature — let /goal handle a bug too." | v1 is feature-only: a bug has no PRD gate and a different phase map. Route to `/fix-bug`. |
| "Preflight is ceremony — start and approve prompts as they come." | An autonomous loop can't answer a native prompt; it stalls. If reviewer/push aren't prompt-free, HALT and point to `goal-autonomy-setup.md`. |
| "The review looked fine; skip counting rounds." | The breaker is the only bound on a non-converging loop. Count `kind=round` (goal-state.sh) and HALT at N. |
| "Commit per task so progress is saved." | Under `/goal`, implementers use `commit_policy=defer`; a mid-loop commit breaks ship-gates. `/goal` makes the single commit at ship. |
| "Push after the gates — the native prompt is the approval." | GATE 2 is an explicit driver-issued approval; the native prompt is bypassable. Commit first, then explicit approval, then push. |
| "It halted; I resolved the blocker, so resume it." | `halted` is terminal for automation. A human dispositions/resets and starts fresh — `/goal` never self-releases a halt. |

## Red flags

- A bug objective driven through the feature machine.
- Preflight run before state init, or the middle entered without a `gate1` record.
- Review-log lines that `goal-state.sh round-count` can't parse (breaker silently no-ops).
- An implementer committing during `tdd`.
- GATE 2 authorized before the single commit, or a push with no explicit human approval.
- `goal-digest.sh` invoked without `<base_sha>`, or `--from-head` passed as the base.

## Verification

- [ ] Objective classified; a bug was routed to `/fix-bug`.
- [ ] State initialized before preflight; preflight passed or HALTed honestly (pointing to
      `goal-autonomy-setup.md` on the permissions case).
- [ ] Exactly two explicit human gates (PRD approval; PR creation); everything else autonomous or a
      council call.
- [ ] Review loops bounded via `goal-state.sh round-count` (certification or HALT at N/MAX);
      `simplify` ran at most once.
- [ ] Single commit BEFORE GATE 2; `goal-digest.sh --from-head` == certification digest.
- [ ] `## /goal loop` reflects the real phase; no completed phase restarted; a halt was left for a
      human, not self-resumed.
