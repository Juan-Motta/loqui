# Docs Layout

Where each artifact lives. The workflow skills write to these paths so knowledge is
findable and portable across all three engines.

| Path | Holds | Written by |
| --- | --- | --- |
| `docs/prds/<feature>.md` | Product requirements (problem, users, goals, criteria) | `prd` |
| `docs/plans/<feature>.md` | Design plan (approach, files, tests, acceptance) | `plan`, `new-feature` |
| `docs/research/<YYYY-MM-DD>-<topic>.md` | Sourced research brief | `research` |
| `docs/solutions/<slug>.md` | Solved-bug knowledge base (symptom→cause→fix) | `fix-bug` |
| `docs/adr/<NNN>-<slug>.md` | Architecture decision records | `adr` (from `plan` / `council`) |
| `docs/e2e/reports/` | verify-e2e evidence reports (committed; the ship-gate binds to these) | `verify-e2e` |
| `docs/e2e/use-cases/` | Graduated user-journey use cases (committed regression suite) | `verify-e2e` / `new-feature` / `fix-bug` |
| `docs/CHANGELOG.md` | Human-readable history of notable changes | `finish-branch` / ship |
| `docs/index.md` | High-level project map (structure, entry points, conventions) | `index` |

## Rules

- Create the file under the right folder; the folder already exists (`.gitkeep`).
- One artifact per file; name it after the feature/topic so it's greppable.
- These are the **portable memory** of the project — see `shared/rules/memory.md`.
- Keep `docs/CHANGELOG.md` newest-first, one line or short block per shipped change.
