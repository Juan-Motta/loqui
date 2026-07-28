---
name: finish-branch
description: Wrap up a completed branch — confirm the ship-gate profile is green, do a final verify, record durable docs, then commit, push, and open the PR. Use to close out work under Claude Code, Codex, or OpenCode after the feature/fix workflow is done.
---

# finish-branch

Close out a branch cleanly. This does not replace the feature/fix workflow — it's the
final step after one.

## 1. Confirm the gates

Run the deterministic checker — don't just eyeball the file:

```sh
sh shared/scripts/check-gates.sh        # pwsh shared/scripts/check-gates.ps1 on Windows
```

It reads `.workflow/state.md`, confirms **every box for the active profile** is checked (or
N/A), and exits non-zero listing any that aren't (`shared/rules/ship-gates.md`). If it fails,
go back and finish those boxes — do not ship.

This is **Tier B**: it verifies the *record*, not the work. A checked box is an
*attestation* (you claimed it), not independent proof — so also spot-confirm the underlying
work is real (tests actually ran, the change was actually exercised). See the
Verified / Attested / Advisory distinction in `shared/rules/ship-gates.md`.

`check-gates` now also enforces the `E2E verified` marker — if it's checked (not `N/A:`),
a fresh `VERDICT: PASS` report must exist under `docs/e2e/reports/`. finish-branch relies on
the report produced during the workflow; it does not re-run the journeys.

## 2. Final verify

Run the test suite and exercise the change once more end-to-end. Confirm the working tree
is otherwise clean and the branch is up to date.

## 3. Record durable docs — BEFORE shipping

Do this **before** the ship commit so the documentation ships *with* the change (not left
uncommitted, and never as a second unreviewed commit):

- Add a newest-first entry to `docs/CHANGELOG.md` (`shared/rules/docs-layout.md`).
- Save any reusable learning per `shared/rules/memory.md` (solved bugs → `docs/solutions/`,
  decisions → `docs/adr/`).

## 4. Commit

Commit all intended changes **including the docs from step 3** with a clear message.
Nothing uncommitted, no stray files.

## 5. Push + open PR

Push the branch and open the PR. Both hit the **native prompt** (a commit-confirmation, not
a gate — `shared/rules/ship-gates.md`) — approve only because you just confirmed the gates
in step 1. Write a PR description stating what changed, why, and how it was verified.

## 6. Update transient state

Only now — after shipping — record the PR link / merge outcome in `.workflow/state.md` so
the workflow state reflects reality. (This is the one thing that legitimately comes after
the ship commit.)

## Under `/goal` (owner=goal)

Under `owner=goal`, `/goal` owns the ship — this skill does **NOT** independently run steps 4–6.
`/goal` performs them in its §8 order: **single commit FIRST**, then it proves the committed-tree
digest equals the certification digest, then **GATE 2** authorizes the already-committed head, then
push → PR. Step 3 (durable docs) may add **only** the CHANGELOG at ship time (it is digest-neutral);
any ADR/solution doc must be written **before** final certification, because a post-certification
change to a digest-covered path forces code-review re-entry (`goal` skill §6.5).

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "The gates are basically green — ship it." | "Basically" isn't checked. Any open required box means go back and finish it; the native prompt is not the gate. |
| "I'll add the changelog / ADR in a follow-up commit." | Docs ship *with* the change, in the same commit. A second unreviewed commit is exactly how they get lost. |
| "The push prompt appeared, so I'm cleared to ship." | The prompt is a commit-confirmation that reads no gate state (`shared/rules/ship-gates.md`). It proves nothing — you are the gate. |
| "I'll record the PR outcome later." | Update `.workflow/state.md` with the PR link / merge outcome now, so the workflow reflects reality. |

## Red flags

- An open box in `.workflow/state.md` and you're pushing anyway.
- The CHANGELOG / ADR is uncommitted or slated for "later."
- You approved the push prompt without re-confirming the gates in step 1.
- Stray or unintended files in the working tree at commit time.

## Verification

- [ ] Every required box for the active profile is checked in `.workflow/state.md`.
- [ ] Full suite run and the change exercised end-to-end once more.
- [ ] CHANGELOG entry (+ ADR / solution where applicable) committed *with* the change.
- [ ] Working tree clean — nothing stray, nothing uncommitted.
- [ ] PR opened stating what changed, why, and how it was verified; outcome recorded in state.
