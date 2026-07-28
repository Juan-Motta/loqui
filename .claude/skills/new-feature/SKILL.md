---
name: new-feature
description: Full feature workflow — research, plan, cross-engine design review, TDD, code review, verify, and ship behind the ship-gate. Use when starting or implementing any new feature or behavior change — build it end to end with tests and review — under Claude Code, Codex, or OpenCode.
---

# new-feature

Drive a feature from idea to shipped change with cross-engine review discipline. Works
identically under Claude Code, Codex, and OpenCode.

## 0. Set up tracking

- Confirm you are **not on `main`** — create a branch (`feat/<name>`).
- Copy `shared/state.template.md` to `.workflow/state.md`; set **Profile: standard**, the feature name, and branch.

## 1. Research (when external tech is involved)

If the feature touches an unfamiliar or external library/API/protocol, run the `research`
skill first and write a sourced brief (`shared/rules/research.md`). Skip only for changes
fully contained in code you already understand.

## 2. Plan

Clarify intent, then compare 2–3 approaches and pick one — use the `plan` skill and the
fixed axes in `shared/rules/approach-comparison.md`. Capture the goal, chosen approach,
files/units to touch, edge cases, the test plan, and acceptance criteria. Keep units
small. Do not write implementation code yet.

## 3. Design review (cross-engine)

Validate the plan with the `review` skill (a *different* engine reviews) — or `council`
for a hard fork. Reviewer/advisor models come from `shared/rules/models.md`. Collect
findings by severity (`shared/rules/severity.md`); resolve P0/P1/P2; record the iteration
in `.workflow/state.md`.

## 4. TDD

Red → green → refactor (`shared/rules/tdd.md`). Write the failing test first, make it pass
minimally, then refactor. Never write implementation before a failing test exists. Execute
the implementation per the configured mode in `shared/rules/execution.md` (inline, or
subagent-driven on Claude Code).

## 5. Code review (cross-engine)

Review the diff with the `review` skill (the other engine) + a self-pass. Fix all
P0/P1/P2. Repeat until a single pass is clean. Record iterations in `.workflow/state.md`.
Then run `simplify` for a behavior-preserving cleanup pass while the suite is green.

## 6. Verify

Run the `verify-e2e` skill: design/execute API, CLI, and (for UI-facing changes) UI
user-journey use cases, and let it write the evidence report the ship-gate checks. For
purely internal changes (migration, refactor, tooling), record
`E2E verified — N/A: <reason>` in `.workflow/state.md`.

## 7. Ship

Only when every required box in `.workflow/state.md` is checked (`shared/rules/ship-gates.md`).
Commit, then push / open PR — approve the native prompt **only if the gates are green**.
Record the change in `docs/CHANGELOG.md` and save any reusable learning
(`shared/rules/memory.md`). Or run `finish-branch` to do the wrap-up.

## Under `/goal` (owner=goal)

`/goal` drives the phase sequence itself; it does not run `new-feature` as a standalone unit. If a
phase's guidance here is used under `owner=goal`, the following belong to `/goal`, not this skill:
state init (§0), design-review and code-review **loop control** (§3/§5), `simplify`, verify (§6),
ship (§7), review-log lines, and phase transitions. Implementers run with `commit_policy=defer`
(stage-only; `/goal` commits once at ship). This skill contributes only the requested phase's work.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "I understand this — skip the plan." | Even familiar work benefits from comparing approaches and a written plan the reviewer can check. A plan gap that builds the wrong thing is a P1 (`shared/rules/severity.md`). |
| "I'll write the tests after the code." | TDD is red → green → refactor. Implementation before a failing test isn't TDD and silently skips cases. |
| "The reviewer is just another AI — its findings don't count." | The whole point is a second, differently-trained model catching what you miss. Resolve P0/P1/P2 before shipping. |
| "I'll do the design review after I've built it." | Reviewing after implementation only sees code consistent with an *unreviewed* plan. Review the plan first, where the cheap fixes are. |
| "Tests pass, no need to actually run it." | Exercise the real change — tests alone miss integration, wiring, and UX defects. |

## Red flags

- No plan file, or a plan with a single approach and no comparison.
- Implementation code exists before any failing test.
- You reviewed with the same engine that wrote the code (no cross-engine pass).
- You're about to ship on green tests without exercising the change end-to-end.

## Verification

- [ ] Plan written with a compared, chosen approach + acceptance criteria.
- [ ] Design reviewed cross-engine; P0/P1/P2 resolved.
- [ ] TDD followed — a failing test preceded each piece of implementation.
- [ ] Code review clean on a single pass (cross-engine + self-pass).
- [ ] Change exercised for real; outcome observed and noted.
- [ ] Every required ship-gate box checked; `docs/CHANGELOG.md` updated.
