---
name: index
description: Generate or refresh docs/index.md — a high-level project map (structure, key locations, entry points, conventions) so an agent orients fast without re-scanning the whole tree. Use when onboarding to a project or after a significant structural change, under Claude Code, Codex, or OpenCode.
---

# index

Give the agent a curated map instead of re-scanning the tree every session. Keep it
**high-level** so it stays fresh — structure and conventions change slowly; line-level
detail churns and goes stale fast.

## 1. Scan the shape

List the top-level layout, the key directories, and the notable files. Identify the
**entry points** (how you run / build / test / install it) and the main modules or areas.

## 2. Find the conventions

Note the conventions an agent must know to work here: naming, where each kind of thing
lives, patterns, and gotchas. Pull them from `PROJECT.md`, `README.md`, and the code —
don't invent them.

## 3. Write docs/index.md

Write a concise map: what the project is (one line + link to `README.md`), entry points, a
"where things live" table (directory → purpose), the key files, and the conventions. Do
**not** enumerate every file or go line-level — that is what goes stale.

## 4. Note freshness

Add an `Updated:` date. Refresh after a **significant structural change** (a new top-level
area, moved/renamed modules) — not on every edit.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "I'll list every file so the map is complete." | Line-level detail goes stale fast. Keep the map high-level — structure and conventions, not an inventory. |
| "I'll infer the conventions." | Pull conventions from `PROJECT.md` / `README.md` / the code — don't invent them. |
| "No need to check the paths are real." | An index that lists paths that don't exist is worse than none. Spot-check they exist. |

## Red flags

- The map enumerates individual files or drops to line-level detail.
- Conventions are asserted with no source in the repo.
- Listed paths don't exist, or there's no `Updated:` date.

## Verification

`docs/index.md` names **real, current** top-level paths and entry points (spot-check they
exist), stays high-level, and carries an `Updated:` date. If it lists paths that don't
exist, it's stale — regenerate it.
