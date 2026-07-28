# Project Rules (layering)

Two layers of rules apply to every session, both always-on:

1. **Global baseline** — `CLAUDE.md` golden rules + `shared/rules/*`. The framework's
   discipline. Applies **always, without exception**.
2. **Project rules** — `PROJECT.md` at the repo root (from `PROJECT.template.md`):
   **Persona · Project info · Variables · Special rules**. Project-specific and editable.

## Load order

Read `PROJECT.md` alongside the global rules at session start (golden rule). All three
engines auto-load `CLAUDE.md`/`AGENTS.md`, which points here; OpenCode also force-loads
`PROJECT.md` via `opencode.json` `instructions`.

## Precedence (important)

- The **global safety baseline** — branch safety, the ship-gate, and anything that
  prevents data loss or an unreviewed ship — **should not** be overridden by `PROJECT.md`.
  Note: this is **advisory** — nothing physically enforces it, so review your own
  `PROJECT.md` against `ship-gates.md` and don't write project rules that weaken safety.
- `PROJECT.md` **adds and refines**: persona/tone, project context, variables, and
  special behavior.
- On a genuine conflict, treat the global safety baseline as winning; surface the conflict
  rather than silently following the weaker rule.

## Adding project rules

Copy `PROJECT.template.md` → `PROJECT.md`, fill the four sections, commit it. That's the
whole mechanism — no per-engine config needed (the golden rule + `instructions` handle
loading).
