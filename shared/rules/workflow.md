# Workflow

Use a workflow skill for any non-trivial change. Follow its steps in order.

## Which skill

| Scenario | Skill |
| --- | --- |
| Define what/why before building | `prd` |
| Check current docs / prior art | `research` |
| Design: compare approaches, write a plan | `plan` |
| New feature / behavior change | `new-feature` |
| Bug fix | `fix-bug` |
| Trivial change (<3 files) | `quick-fix` |
| Cross-engine second opinion (plan or diff) | `review` |
| Verify a user-facing change end to end | `verify-e2e` |
| Hard, expensive decision fork | `council` |
| Wrap up + open PR | `finish-branch` |
| Session handoff before closing | `checkpoint` |
| Project map for orientation | `index` |
| Drive a whole feature hands-off (idea → PR-ready) | `goal` |

`goal` is an **orchestrator**: it runs the skills above under `owner=goal` (`execution.md`), pausing
for a human only to approve the PRD and to create the PR (see `goal-autonomy-setup.md` to enable the
unattended permissions). Feature-only in v1 (a bug routes to `fix-bug`). Its autonomy is discipline,
not a runtime gate — see the honesty note in the `goal` skill.

## Phases (shared shape)

1. **Brainstorm** — clarify intent, constraints, success criteria before designing.
2. **Plan** — write the approach; identify files, edge cases, tests.
3. **Design review** — get a second opinion from a *different* engine (any of Claude
   Code / Codex / OpenCode; models in `models.md`) before implementing. Cross-engine
   diversity is the point.
4. **TDD** — red → green → refactor. Write the failing test first.
5. **Code review** — dual review (the other engine + self) against the diff; fix all
   P0/P1/P2 before shipping (see `severity.md`).
6. **Verify** — actually exercise the change, don't just trust tests.
7. **Ship** — only when `.workflow/state.md` gates pass (see `ship-gates.md`).

## Tracking

Keep `.workflow/state.md` current: check boxes as phases complete, record the active
branch and the review iterations. It is the source of truth the ship-gate checklist reads.
