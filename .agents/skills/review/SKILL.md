---
name: review
description: Get a cross-engine second opinion on a plan or a code diff — run the other engine as reviewer, collect severity-tagged findings (P0–P3), and report. Use during the design-review and code-review phases under Claude Code, Codex, or OpenCode.
---

# review

A utility skill for the review phases of other workflows. The point is **model
diversity**: the reviewer must be a *different* engine than the one driving.

## 1. Identify the target

- **Plan review** — a written plan/spec before implementation.
- **Code review** — the current diff (uncommitted, or vs `main`).

State which, and the exact scope (file paths, base ref).

## 2. Pick the reviewer (must differ from the driver)

- Driver Claude Code → reviewer Codex (`codex exec ...`) or OpenCode (`opencode run ...`).
- Driver Codex → reviewer Claude (`claude -p ...`) or OpenCode.
- Driver OpenCode → reviewer Claude or Codex.

Use the reviewer model + effort from `shared/rules/models.md`. Give the reviewer the
target + this instruction: report findings tagged by severity (`shared/rules/severity.md`)
with location and a concrete fix.

**Invoke the reviewer read-only.** It judges the diff/plan; it must not change it. Use
read-only permissions (Codex `--sandbox read-only`; Claude/OpenCode: no write/edit tools)
and hand it the plan/diff as text. Afterward, confirm the working-tree diff is unchanged.

**Single-engine fallback.** If no second engine is available, do a delayed self-review (or
use a human reviewer) and log a waiver in `.workflow/state.md` — see the fallback in
`shared/rules/ship-gates.md`. Cross-engine is preferred; the waiver keeps the degradation
explicit, not silent.

## 3. Collect findings

Gather the reviewer's output as P0/P1/P2/P3 items. If output is missing or unparseable,
treat it as a failed review — re-run, do not fabricate a verdict.

## 4. Act and record

- Resolve all P0/P1/P2 (P3 optional). Re-run the reviewer until a pass is clean.
- Record each iteration and its result in `.workflow/state.md` (which engine, findings).

## Under `/goal` (owner=goal)

Under `owner=goal`, perform **exactly one** read-only reviewer pass and return the severity-tagged
findings. Do **NOT** do step 4's "resolve, re-run, record each iteration" — `/goal` owns the loop:
it decides iterations, writes the review-log lines, and enforces the breaker
(`shared/rules/execution.md`). No looping, no state writes from this skill.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "Same-engine review is fine." | The whole point is a *different* model's blind spots. A same-engine pass is an echo, not diversity — use the other engine or log a waiver. |
| "The reviewer output was empty — I'll just say it passed." | Missing or unparseable output is a *failed* review; re-run it. Never fabricate a verdict. |
| "P2s aren't worth fixing." | The loop exits only on no P0/P1/P2. P3s are optional; P2s block. |
| "I'll let the reviewer edit the fix while it's at it." | The reviewer is read-only — it judges, it doesn't touch the diff. Confirm the working tree is unchanged afterward. |

## Red flags

- Reviewer engine == driver engine, with no waiver logged.
- A "clean" verdict with no reviewer output behind it.
- The loop has run many passes without converging — escalate, don't grind.
- The working-tree diff changed during the review.

## Verification

The loop exits only when a single pass from the reviewer yields no P0/P1/P2. State that
explicitly before returning to the calling workflow.
