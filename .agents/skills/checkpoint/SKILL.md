---
name: checkpoint
description: Write a clean session handoff to CONTINUITY.md before closing a session or resetting context, so the next session knows the current state and the exact next step. Use when wrapping up under Claude Code, Codex, or OpenCode.
---

# checkpoint

Capture enough that a fresh session (or a different engine) can pick up without you
re-explaining. See `shared/rules/continuity.md`.

## 1. Assess current state

In one pass, determine: what's done, what's in progress, the **single next action** to
take on resume, and any blocker. Be concrete — "continue the feature" is useless; "write
the failing test for the empty-cart case in `cart_test`" is a handoff.

## 2. Update CONTINUITY.md

Write `CONTINUITY.md` at the repo root (create it from `CONTINUITY.template.md` if
missing) with: Focus, Next step, Blockers, Active-workflow pointer, Updated date, and 2–4
handoff notes (where we are, why, key context/decisions). Keep it tiny.

## 3. Sync the workflow pointer

If a workflow is active, make sure `.workflow/state.md` is current too, and that
`CONTINUITY.md`'s "Active workflow" points to it. Don't copy state.md's content into
`CONTINUITY.md` — just point.

## 4. Confirm

State a one-line summary of what the next session will pick up.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "'Continue the feature' is a fine next step." | A vague handoff defeats the purpose. The Next step must be a specific action ("write the failing test for the empty-cart case"). |
| "I'll copy state.md into CONTINUITY.md." | Point at state.md, don't duplicate it — two copies drift. Keep CONTINUITY.md tiny. |
| "No need to note the blocker." | The next session inherits the blocker blind if you don't record it. |

## Red flags

- The Next step is generic ("keep going", "finish it").
- CONTINUITY.md duplicates state.md instead of pointing at it.
- No Updated date, or it isn't today's.

## Verification

`CONTINUITY.md` exists with a **concrete Next step** (a specific action, not "continue")
and an Updated date of today. A vague handoff defeats the purpose — rewrite it.
