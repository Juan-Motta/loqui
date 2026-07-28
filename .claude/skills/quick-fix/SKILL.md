---
name: quick-fix
description: Lightweight workflow for trivial changes (fewer than 3 files, no behavior risk) — branch, change, quick check, verify, ship. Use for typos, copy tweaks, small config edits under Claude Code, Codex, or OpenCode. Escalate to new-feature or fix-bug if scope grows.
---

# quick-fix

For genuinely trivial changes only. If the change touches 3+ files, alters behavior, or
turns out non-obvious, **stop and switch** to `new-feature` or `fix-bug`.

## 0. Set up tracking

- Confirm you are **not on `main`** — create a branch (`chore/<name>` or `fix/<name>`).
- A full `.workflow/state.md` is optional here; still record the branch and a one-line
  intent so the ship-gate has context. If you use state.md, set **Profile: light**.

## 1. Make the change

Apply the small edit. Keep it to the stated scope — no drive-by refactors.

## 2. Quick check

- If code with a runtime effect: add or run a relevant test.
- If docs/config: sanity-check syntax and that nothing references the old value.

## 3. Verify

Exercise the changed path (or render the doc / load the config) and confirm the intended
result. Trivial does not mean unverified.

## 4. Ship

Commit, then push / open PR. The native approval prompt on push/PR still applies — approve
only if the change is truly complete and correct.

> Scope guard: the moment this stops feeling trivial, abandon quick-fix and restart under
> `new-feature` / `fix-bug` with the full discipline.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "It's one line, no need to test or run it." | A one-line change with a runtime effect still needs the changed path exercised. Trivial does not mean unverified. |
| "It's grown to four files but I'm nearly done — I'll finish here." | 3+ files or a behavior change is the escalation trigger. Stop and restart under `fix-bug` / `new-feature`; don't finish under the light profile. |
| "While I'm in here I'll tidy up the nearby code." | Drive-by refactors break the no-behavior-risk contract. Keep to the stated scope or escalate. |
| "It's just a doc — nothing to check." | Confirm the doc renders and nothing still references the old value. |

## Red flags

- You're editing a third file, or changing behavior rather than copy/config.
- You skipped exercising the change because it "obviously works."
- You're reaching for a refactor "while you're here."
- The change stopped feeling trivial two edits ago and you kept going.

## Verification

- [ ] Change stayed in scope (<3 files, no behavior change) — otherwise escalated to `fix-bug` / `new-feature`.
- [ ] Changed path exercised (or doc rendered / config loaded); intended result observed.
- [ ] Nothing still references the old value.
- [ ] On a branch, not `main`; ship prompt approved only because the above hold.
