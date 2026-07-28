# Workflow State — <feature-name>

> Copy this file to `.workflow/state.md` when a workflow starts. It is the source of truth
> the ship-gate reads. Keep it updated as you progress.

## Active workflow

- **Skill:** new-feature
- **Profile:** standard  <!-- standard (new-feature, fix-bug) | light (quick-fix) — see shared/rules/ship-gates.md -->
- **Feature:** <name>
- **Branch:** <feat/...>
- **Driver:** <claude | codex | opencode>
- **Phase:** <brainstorm | plan | design-review | tdd | code-review | verify | ship>

## Ship-gate checklist (boxes for the Profile above — see shared/rules/ship-gates.md)

<!-- `standard` profile shown below; for the `light` profile (quick-fix) use its shorter list. -->

- [ ] On a feature branch (not `main`)
- [ ] Plan written and design-reviewed by the other engine — `N/A: <reason>` allowed for a simple fix-bug (see ship-gates.md)
- [ ] Tests written (TDD) and passing
- [ ] Code review clean — no open P0/P1/P2
- [ ] E2E verified via verify-e2e (report: docs/e2e/reports/<...>.md)
- [ ] State updated

## Review log

<!-- Standalone workflows: free-form "Design/Code review iteration N — <engine> — findings: …".
     Under /goal (owner=goal): fixed-schema lines that goal-state.sh parses — see
     shared/rules/goal-state.md, e.g.
     - loop=code — round=1 — kind=round — reviewer=codex — result=P1=2 — digest=<sha> — ts=<ISO> -->

- Design review iteration 1 — <engine> — findings: <P0/P1/P2 counts or "clean">
- Code review iteration 1 — <engine> — findings: <...>

## Notes

<decisions, assumptions, what was verified>

## Blockers

<!-- HALT records (/goal, see shared/rules/goal-state.md). Empty when nothing is halted.
     - [ ] BLOCKER — <phase> — <reason> — ts=<ISO>
     HALT is terminal for automation; a human dispositions/resets before a new /goal run. -->

## Attempts

<!-- Durable retry counters (/goal, see shared/rules/goal-state.md). Empty by default.
     - ATTEMPT ship-red — n=<k> — ts=<ISO>   (n>=2 → /goal HALTs) -->
