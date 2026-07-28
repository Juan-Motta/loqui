# Workflow discipline for Claude Code, Codex + OpenCode

> This file is the always-on instruction set. `AGENTS.md` is a generated copy of it, so
> **Claude Code** (reads `CLAUDE.md`), **Codex** (reads `AGENTS.md`), and **OpenCode**
> (reads `AGENTS.md`, falls back to `CLAUDE.md`) all load the exact same discipline — no
> symlinks, no drift. Both are generated from the codeforge source; to change the discipline,
> edit the source there and re-run the codeforge installer against this project (don't
> hand-edit `CLAUDE.md`/`AGENTS.md` here — a re-install overwrites them).

## What this is

A lightweight, **skills-and-config-only** workflow system. It's **discipline, not a hard
gate**: you follow the instructions (advisory), plus each engine shows a **best-effort
native prompt** on outward actions (push / PR) that the human confirms — it reads no gate
state and is bypassable. It runs identically under Claude Code, Codex, and OpenCode.

## Golden rules (always apply)

- **Resume from continuity.** At the start of each session, read `CONTINUITY.md` first and
  continue from its **Next step** before anything else; if it names an active workflow,
  open that `.workflow/state.md` too. See `shared/rules/continuity.md`.
- **Load project rules.** Also read `PROJECT.md` — this project's persona, info,
  variables, and special rules. It **adds** to these global rules and **should not**
  override the safety/ship-gate baseline (advisory — nothing enforces this). See
  `shared/rules/project-rules.md`.
- **Never work on `main`.** Create a branch before changing code.
- **Pick the right workflow skill** for the task (see index below) and follow it.
- **`.workflow/state.md` is the source of truth** for the active workflow: its checklist
  gates shipping. Keep it updated as you progress. If it doesn't exist for the current
  task, start from `shared/state.template.md`.
- **Do not ship until the gates pass.** Before `git commit` / `git push` /
  `gh pr create`, every required box in `.workflow/state.md` must be checked. See
  `shared/rules/ship-gates.md`.
- **Challenge, don't flatter.** Push back on weak ideas; verify claims against the code.
- **Ground your claims.** State what you verified vs. inferred; cite `file:line`.
- **Capture learnings.** Save reusable solutions/decisions to the repo `docs/` (portable
  memory) — see `shared/rules/memory.md` and `shared/rules/docs-layout.md`.

## Workflow skills (canonical in `skills/`, discovered by all three harnesses)

- `prd` — write a product requirements doc (problem/users/goals) before designing
- `research` — pre-design research: check current docs + prior art, write a sourced brief
- `plan` — design step: clarify intent, compare approaches, write a reviewed plan
- `new-feature` — full feature workflow (brainstorm → plan → cross-review → TDD → review → verify → ship)
- `fix-bug` — bug fix with systematic debugging (reproduce → root cause → failing test → fix → review → verify → ship)
- `quick-fix` — trivial changes (<3 files, no behavior risk); escalate if scope grows
- `review` — cross-engine second opinion on a plan or diff (P0–P3 findings)
- `simplify` — post-green, behavior-preserving cleanup pass (reduce complexity, tests stay green)
- `verify-e2e` — execute API/CLI and UI user-journey use cases, write an evidence report, and bind the E2E ship-gate box to it
- `council` — multi-perspective decision analysis: several engines as advisors → synthesized verdict + minority report (for hard, expensive forks)
- `adr` — record an architecture decision (context, alternatives, consequences) → `docs/adr/`
- `finish-branch` — close out a branch: confirm gates → final verify → commit → push → PR
- `goal` — drive one feature objective autonomously (prd → plan → TDD → review → verify → ship) with only two human gates (approve PRD, create PR); orchestrates the other skills under owner=goal, doesn't replace them
- `checkpoint` — write a clean session handoff to `CONTINUITY.md` before closing / context reset
- `index` — generate/refresh `docs/index.md`, a high-level project map for fast orientation

## Discipline reference (`shared/rules/`)

- `workflow.md` — when to use which skill; the phases
- `severity.md` — P0–P3 rubric for review findings
- `ship-gates.md` — what must be true before commit/push/PR, and how each harness gates it
- `tdd.md` — red-green-refactor discipline
- `execution.md` — inline vs subagent-driven implementation (subagents are Claude-only)
- `research.md` — when to research and what a good brief contains
- `approach-comparison.md` — fixed-axes table for choosing an approach
- `memory.md` — what to save and where (repo-first, portable across engines)
- `docs-layout.md` — where each artifact lives (`docs/prds|plans|research|solutions|adr`, `CHANGELOG.md`)
- `continuity.md` — session handoff via `CONTINUITY.md`; how to resume on a new session
- `models.md` — default model per role (research/review/council); reviewer ≠ driver
- `project-rules.md` — how per-project `PROJECT.md` layers on the global baseline (precedence)
- `goal-state.md` — `/goal`'s `.workflow/state.md` line schemas (shared with `goal-state.sh`)
- `goal-autonomy-setup.md` — per-engine allow-entries to enable `/goal`'s unattended mode

## Enforcement model (read this — it's a deliberate trade)

There is **no hard conditional block** locally, and only one mechanism that CAN bind for
everyone once fully configured. Locally, it's discipline: the skills instruct you to run the gates before shipping
(**Advisory**), and `finish-branch` runs `shared/scripts/check-gates.sh` — a deterministic
check of `.workflow/state.md` that fails loudly on an unchecked box, but only when invoked
(**Attested**, local). On top of that, each harness applies a **coarse native approval** on
outward actions — a human-confirmation prompt, not a gate check:

- **Claude Code:** `git push` / `gh pr create` are `ask`-tier in `.claude/settings.json`
  (human approves).
- **Codex:** `approval_policy` in `.codex/config.toml` requires approval before running
  non-trivial shell commands.
- **OpenCode:** `permission.bash` in `opencode.json` sets `git push*` / `gh pr create*`
  to `ask` (and force-push to `deny`).

None of them read `.workflow/state.md` to decide — they always ask; you approve, after
checking the gates (or running `check-gates.sh`).

The **real hard gate** is the shipped CI template, `docs/ci-templates/gates.yml`: once copied
into `.github/workflows/`, its test step filled in, and made a **required status check** with
branch protection (bypass disabled), CI independently reruns your declared test command on the
PR merge result, outside any agent's turn (**Verified**). It only becomes bad-faith-
**resistant** — and can bind for every clone and every merge — once the repo is also fully
configured per `docs/ci-templates/README.md` (CODEOWNERS on the workflow file and on the
test-defining files, "Dismiss stale pull request approvals" enabled, strict/up-to-date checks
or a merge queue, and bypass disabled for admins too) — and even then it still depends on a
human actually reading those diffs. Repo/org admins can still bypass branch protection unless
you've configured otherwise. Until you activate it, nothing here actually blocks a bad ship. A
checked box in `.workflow/state.md` is *attested*, not *verified* — see
`shared/rules/ship-gates.md` for the full Verified/Attested/Advisory ladder.
