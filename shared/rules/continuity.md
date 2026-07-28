# Continuity

`CONTINUITY.md` (repo root) is the **session handoff anchor** — always present, so it's
the reliable "read this first" on any new session or after a context reset.

## Roles (non-overlapping — no drift)

- **`CONTINUITY.md`** — cross-session state: current focus, the single Next step,
  blockers, and a pointer to the active workflow. Small (a handful of lines).
- **`.workflow/state.md`** — the active workflow's checklist/gates. Only exists while a
  workflow runs.

`CONTINUITY.md` **points to** `state.md`; it never duplicates it.

## On session start

Read `CONTINUITY.md` first and resume from **Next step** before doing anything else. If it
names an active workflow, open that `.workflow/state.md` too and continue there.

## Keeping it current

- Update it as the situation changes — especially the Next step.
- Before closing a session, or when context is getting tight, run the `checkpoint` skill to
  write a clean handoff.
- Keep it tiny: focus, next step, blockers, pointer, date + a few handoff lines. Details
  live in the workflow/docs, not here.

## Caveat

Saving the handoff **automatically** right before an unexpected context compaction needs a
per-engine hook and is not covered here. Keep `CONTINUITY.md` current manually so any reset
point already reflects reality.
