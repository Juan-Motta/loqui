---
name: fix-bug
description: Bug-fix workflow with systematic debugging — reproduce with a failing test, isolate root cause, fix minimally, cross-review, verify, and ship behind the ship-gate. Use when correcting incorrect behavior under Claude Code, Codex, or OpenCode.
---

# fix-bug

Fix a defect with evidence, not guesses. Works identically under all three engines.

## 0. Set up tracking

- Confirm you are **not on `main`** — create a branch (`fix/<name>`).
- Copy `shared/state.template.md` to `.workflow/state.md`; set skill = `fix-bug`, **Profile:
  standard**, feature/bug name, branch, and driver.

## 1. Reproduce

Reproduce the bug deterministically. Capture the exact input, expected vs actual, and the
smallest failing case. Do not propose a fix until you can trigger it on demand.

## 2. Root cause (systematic debugging)

Form a hypothesis, add logging/assertions to test it, and confirm the actual cause before
touching the fix. Read the relevant code and cite `file:line`. Distinguish what you
verified from what you infer.

## 3. Failing test first (TDD)

Write a test that fails **because of the bug** (red) — see `shared/rules/tdd.md`. This
proves the repro and prevents regression. For a high-impact surface, also design-review
the fix approach with the `review` skill (a different engine) before implementing.

## 4. Fix minimally

Make the smallest change that turns the test green. Refactor only if it clarifies. Fix
the real cause, not the symptom. Execute per `shared/rules/execution.md` (inline, or
subagent-driven on Claude Code).

## 5. Code review (cross-engine)

Review the diff with the **other** engine (`review` skill; models per
`shared/rules/models.md`) + a self-pass; resolve all P0/P1/P2 (`shared/rules/severity.md`).
Record iterations in `.workflow/state.md`.

## 6. Verify

Exercise the original repro and confirm it's gone; check you didn't break neighbors.
Note what you observed. Then run the `verify-e2e` skill to confirm the fix through the
user-facing interface (API/CLI/UI). Internal-only fixes record
`E2E verified — N/A: <reason>`. Record the fix (symptom → root cause → fix → how
verified) in `docs/solutions/<slug>.md` (`shared/rules/memory.md`).

## 7. Ship

Only when every required box in `.workflow/state.md` is checked
(`shared/rules/ship-gates.md`). Commit, then push / open PR — approve the native prompt
only if the gates are green.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "The cause is obvious — I'll just fix it." | A fix without a reproduced failing test is a guess. Reproduce first; read the code and cite `file:line`. |
| "A failing test is overkill for this bug." | The failing test proves the repro and blocks regression — it *is* the fix's evidence. Red before green. |
| "It's late / the lead said skip the regression test." | Time and authority pressure don't change that an unproven fix regresses. The failing test is non-negotiable. |
| "The suite passes, so it's verified." | Re-exercise the *original* repro and check neighbors. Green tests ≠ the user's bug is gone. |
| "This patch makes the symptom go away." | Fix the root cause, not the symptom, or it comes back. |

## Red flags

- You're writing the fix before you can trigger the bug on demand.
- No test fails *because of the bug*.
- You changed code you haven't read (no `file:line` cited).
- You're patching the symptom, not the cause.
- You skipped the cross-engine review to "save time."

## Verification

- [ ] Bug reproduced deterministically before any fix.
- [ ] A test failed because of the bug (red) and passes after (green).
- [ ] Root cause identified and cited (`file:line`) — not the symptom.
- [ ] Cross-engine review clean; all P0/P1/P2 resolved.
- [ ] Original repro re-exercised and gone; neighbors intact.
- [ ] Fix recorded (symptom → root cause → fix → how verified) in `docs/solutions/`.
- [ ] Every required ship-gate box checked.
