---
name: plan
description: Turn an idea into a reviewed design — clarify intent, compare 2–3 approaches on fixed axes, pick one with rationale, and write a plan the implementation follows. Use before building a non-trivial change under Claude Code, Codex, or OpenCode. Feeds new-feature / fix-bug.
---

# plan

Design before you build. Produces a written plan that `new-feature` / `fix-bug`
implements against. Pairs with `research` (run it first when external tech is involved).

## 1. Clarify intent

Pin down purpose, constraints, and success criteria. Ask the user one question at a time
only when something is genuinely ambiguous; otherwise state your assumptions and proceed.
Do not write implementation code in this phase.

## 2. Compare approaches

List **2–3 genuinely different** approaches and score them on the fixed axes in
`shared/rules/approach-comparison.md` (complexity, blast radius, reversibility, time to
validate, correctness/user risk). Name the default winner and why — prefer the simplest
option that clears the bar.

## 3. Validate the choice (don't self-certify)

Run `review` or `council` on the chosen approach before locking it in. If the cheapest
falsifying experiment is quick, spike it first and let evidence decide. Resolve any
P0/P1/P2 the reviewer raises (`shared/rules/severity.md`).

## 4. Write the plan

Capture: the goal, the chosen approach + the comparison table, the files/units to touch,
edge cases, the tests that will prove it (TDD — `shared/rules/tdd.md`), and acceptance
criteria. Keep units small and single-purpose. Save to `docs/plans/<feature>.md`
(`shared/rules/docs-layout.md`) and reference it from `.workflow/state.md`. Record any
significant architecture decision as an ADR (the `adr` skill) in `docs/adr/`.

## 5. Hand off to implementation

`new-feature` / `fix-bug` build from this plan. A gap here propagates downstream, so a
missing required behavior or acceptance criterion is a P1, not a nit.

## Under `/goal` (owner=goal)

`/goal` invokes `review` directly and owns the plan-review loop and its `.workflow/state.md` logging
(`shared/rules/execution.md`). Under `owner=goal` this skill **produces the plan only** — it does
**NOT** run its own step 3 reviewer dispatch, does not loop to convergence, and does not write
review-log lines. `/goal` runs review, counts rounds, and enforces the breaker.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "I already know the best approach — skip the comparison." | Comparing 2–3 genuinely different options is how you catch the cheaper one you didn't consider, and it gives the reviewer something to check. |
| "One approach is enough to write down." | A plan with a single option and no trade-offs is a decision with no audit trail. Name the alternatives and why they lost. |
| "I'll validate the choice by just building it." | Self-certifying skips the cross-engine check where design flaws are cheapest to fix. Review the choice (or spike it) before locking in. |
| "The acceptance criteria are obvious — leave them out." | Implementation builds from the plan; a missing criterion becomes the wrong feature. Spec-loss is a P1, not a nit. |

## Red flags

- The plan lists one approach, or "alternatives" that aren't genuinely different.
- You picked the most complex option without justifying it over the simplest.
- No test plan or acceptance criteria.
- You locked the choice with no second-engine review and no spike evidence.

## Verification

The plan states the goal, a compared-and-chosen approach (with rationale), the test plan,
and acceptance criteria; the choice was reviewed by another engine. Missing any of these
means it's not ready to implement.
