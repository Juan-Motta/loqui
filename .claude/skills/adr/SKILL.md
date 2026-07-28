---
name: adr
description: Record an architecture decision as an ADR (`docs/adr/<NNN>-<slug>.md`) — capture the context, the decision, the alternatives considered, and the consequences, so the reasoning survives. Use after making a significant, hard-to-reverse choice (a dependency, a boundary, a data model, a protocol) under Claude Code, Codex, or OpenCode.
---

# adr

Write down *why*, not just *what*. An Architecture Decision Record captures a significant
choice and the reasoning behind it so the next session — or the next engine, or a teammate —
doesn't re-litigate a settled question or undo it without knowing the cost. This records a
decision already made (by `plan`, `council`, or you); it doesn't make one.

## 1. Decide if it warrants an ADR

Write one when the choice is **significant and hard to reverse**: a framework/dependency, a
module boundary, a data model, an API/protocol, a security or migration strategy. Skip it for
reversible, local, or obvious choices — an ADR for those is noise.

## 2. Number and name it

Find the highest existing `docs/adr/<NNN>-*.md` and use the next sequential number. Name it
`docs/adr/<NNN>-<slug>.md` (`shared/rules/docs-layout.md`), slug after the decision
(`012-event-sourcing-for-orders.md`).

## 3. Write it

Keep it short and specific:

- **Status** — `accepted` (or `proposed` / `superseded by NNN`).
- **Context** — the forces at play: constraints, requirements, what made this a real fork.
- **Decision** — what you're doing, in active voice ("We will …").
- **Alternatives considered** — the other real options and **why each lost**. This is the part
  that stops a future re-litigation.
- **Consequences** — the trade-offs: what becomes easier, what becomes harder, what you accept.

## 4. Link it

Reference the ADR from the plan and/or `.workflow/state.md` so it's discoverable. If it
replaces an earlier decision, mark the old ADR `superseded by <NNN>` and link both ways —
ADRs are append-only history, never rewritten.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "The decision is obvious — no need to write it down." | It's obvious *today*. The ADR is for whoever asks "why is it this way?" six months from now with none of today's context. |
| "I'll just capture it in the commit message." | Commit messages aren't browsable as decisions. ADRs live in `docs/adr/` where the next session actually looks. |
| "I'll state the decision but skip the alternatives." | The alternatives and why they lost are the most valuable part — without them the ADR can't defend itself when someone wants to reverse it. |
| "We changed our mind — I'll edit the old ADR." | ADRs are append-only. Supersede the old one and write a new one; don't rewrite the past. |

## Red flags

- The ADR states the decision but not the context/forces that drove it.
- No alternatives, or alternatives with no reason they lost.
- You edited a prior ADR's decision instead of superseding it.
- It's recording a trivial, easily-reversed choice (ADR overkill).

## Verification

`docs/adr/<NNN>-<slug>.md` exists with the next sequential number and states Status, Context,
Decision, Alternatives (with why each lost), and Consequences. If it replaces an earlier
decision, that ADR is marked superseded and linked. The record is referenced from the plan or
`.workflow/state.md`. An ADR missing its alternatives or consequences is incomplete — finish it.
