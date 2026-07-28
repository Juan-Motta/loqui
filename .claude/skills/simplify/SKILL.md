---
name: simplify
description: Post-implementation cleanup pass that reduces complexity without changing behavior — remove dead code, flatten nesting, de-duplicate, clarify names — with the test suite staying green throughout. Use after a change works (tests pass) and before shipping, under Claude Code, Codex, or OpenCode. Not for adding behavior or fixing bugs.
---

# simplify

Make the code simpler without changing what it does. The refactor step is the first thing
skipped under pressure, so this is its own pass: start from green, reduce complexity in small
behavior-preserving steps, end greener. If it changes behavior, it isn't a simplify — it's a
`fix-bug` or `new-feature`.

## 1. Start from green

The suite must pass **before** you touch anything. A simplify pass leans entirely on the tests
to prove behavior didn't change — if they're red (or thin), stop: fix them (or the bug) first.

## 2. Find the complexity

Look for: dead code, deep nesting (early-return / guard-clause opportunities), duplication
(rule of three), unclear names, and over-abstraction that costs more than it saves.
**Chesterton's Fence:** understand *why* a piece of code exists before removing it — the reason
may not be covered by a test.

## 3. Change in small, behavior-preserving steps

One simplification at a time; **re-run the tests after each**. Never fold a behavior change into
a simplify pass — the moment you do, a green suite no longer proves you kept behavior. A
big-bang refactor hides which step broke something.

## 4. Verify behavior is identical

Full suite green, and actually exercise the changed path (`shared/rules/tdd.md` refactor
discipline). The diff should read as *same behavior, less code*: less duplication, shallower
nesting, clearer names, no dead code.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "While I'm here I'll also fix this small bug / add this tweak." | A simplify pass is behavior-preserving. Mixing in a change means the green suite no longer proves you broke nothing — do it as a separate `fix-bug` / `new-feature`. |
| "This code looks pointless — I'll just delete it." | Chesterton's Fence: understand why it's there first. Tests may not cover the reason. Delete only once you know it's truly dead. |
| "I'll do all the simplifications in one commit." | One behavior-preserving step at a time, re-running tests between. A big-bang refactor hides which step regressed. |
| "Tests pass, so the refactor is done." | Also exercise the path — a passing suite proves the tests pass, not that behavior held where tests are thin. |

## Red flags

- You started from a red or thin suite.
- The diff mixes a behavior change into the cleanup.
- You deleted code without understanding why it existed.
- Many simplifications landed in one step with no test run between.

## Verification

Started from green; the suite is green after every step and at the end. The diff is
behavior-preserving (same inputs → same outputs) and complexity is measurably lower (less
duplication, shallower nesting, clearer names, dead code gone). The changed path was exercised,
not just the tests. If behavior changed at all, this was the wrong skill — revert and use
`fix-bug` / `new-feature`.
