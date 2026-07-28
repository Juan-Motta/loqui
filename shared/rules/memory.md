# Memory & Learning

Capture what's worth reusing so the next session (or the next engine) doesn't relearn it.

## What to save

When you solve a tricky bug, discover a project pattern/convention, or learn a durable
preference or constraint — write it down. Ask yourself before finishing a task:
**"Did I learn anything worth remembering?"** If yes, save it.

## Where to save it (portable-first)

Memory has two homes; the **repo** is the portable one that all three engines share:

1. **Durable project knowledge → the repo** (source of truth, works everywhere):
   - Solved bugs → `docs/solutions/<slug>.md` (symptom, root cause, fix, how verified).
   - Architecture decisions → `docs/adr/<NNN>-<slug>.md`.
   - Notable changes → `docs/CHANGELOG.md`.
2. **Personal / cross-session recall → the engine's own memory, where it has one**
   (Claude Code has a persistent memory; other engines vary — do NOT assume it exists).
   Use it for preferences and session continuity, but never as the *only* copy of
   something the team needs.

**Save solutions twice:** a reusable fix goes to `docs/solutions/` (portable) AND the
engine's personal memory if available.

## Rules

- Prefer the repo for anything another person or engine would need — it's the one place
  all three read.
- Keep entries concise and findable (clear title, one problem each).
- Don't duplicate what the code or git history already records; save the non-obvious
  *why*.
- Never store secrets in memory or docs.
