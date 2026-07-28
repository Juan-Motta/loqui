# Project rules

> Project-specific rules for THIS project. Always loaded alongside the global baseline
> (`CLAUDE.md` golden rules + `shared/rules/*`). These **add and refine** — they
> **should not** override the global safety rules (branch safety, ship-gate) — advisory;
> nothing enforces it. See `shared/rules/project-rules.md`.
> Copy this file to `PROJECT.md` and fill it in.

## Persona

<How the agent should behave here: tone, stance, what to optimize for. One short paragraph.>

## Project info

<What this project is, its stack, layout, and anything an agent needs to orient. A few lines.>

## Variables

<Stable facts the agent should reuse: repo URL, service URLs/ports, key names, entry points.>

- `<KEY>`: `<value>`

## Special rules

Do not add coauthored with in the commits.

## Review policy

<!-- Managed by the codeforge setup wizard — and the SOURCE OF TRUTH for these three values.
     PROJECT.md is project-owned, so it survives `--upgrade`; `shared/rules/models.md` and
     `shared/state.template.md` are MANAGED (refreshed by name on every install), so a value
     written only there is silently reset. The installers re-render both FROM this section on
     every run. Edit here, or re-run the wizard. See `shared/rules/project-rules.md`. -->

Default reviewer(s): codex (`gpt-5.6-sol` · xhigh), claude (`opus` · high)
Council advisors: codex (`gpt-5.6-sol` · xhigh), claude (`opus` · high)
Gate profile: standard

## Execution

Execution: subagent-driven (model: opus)
