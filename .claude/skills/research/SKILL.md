---
name: research
description: Pre-design research — check current docs and prior art for the libraries/APIs/problems a change touches, then write a sourced brief that the plan builds on. Use before designing anything involving unfamiliar or external technology, under Claude Code, Codex, or OpenCode.
---

# research

Don't design on stale, recalled-from-memory assumptions. Model knowledge has a cutoff;
verify against current sources first. See `shared/rules/research.md` for the full standard.

## When to run

Before `plan` / `new-feature` design when the change touches a new or external library,
API, protocol, third-party service, or a well-trodden problem worth learning from. Skip
for changes fully contained in code you already understand.

## 1. Scope the unknowns

List exactly what you need to find out: which libraries/APIs/versions, which behaviors or
constraints, and any "how do others do this" questions. A vague scope yields a useless
brief.

## 2. Consult current sources

Use the engine's available tools (web search / fetch / docs lookup) to gather **current,
versioned** facts. Prefer official docs over blog posts. Note the version and the date
checked. Also look at how established tools/products solved the same problem. If you
delegate the research to another engine, use the research model from
`shared/rules/models.md`.

## 3. Write the brief

Create `docs/research/<YYYY-MM-DD>-<topic>.md` with: the questions, sourced findings
(cite URL + date; separate verified from inferred), prior art, implications for the design,
and open questions. Follow the shape in `shared/rules/research.md`.

## 4. Hand off to design

The brief feeds `plan` / `new-feature`. The resulting design must be consistent with it.
If research surfaced a blocker or a changed assumption, say so before designing.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "I already know this library." | Model knowledge has a cutoff and APIs move. Verify the current, versioned docs before designing on memory. |
| "The blog post is good enough." | Prefer official, versioned docs and note the date checked. A confident blog can be stale or wrong. |
| "I'll cite the sources later." | Cite as you go, with dates, separating verified from inferred. An unsourced brief is just recalled assumptions. |
| "No time to research — just build." | Designing on a wrong assumption costs more than the lookup. If it's genuinely known code, skip research *explicitly* — don't fake it. |

## Red flags

- The brief asserts version-specific behavior with no source or date.
- Verified facts and your own inferences are mixed together.
- You're about to design and haven't checked whether the assumption still holds.

## Verification

The brief exists at `docs/research/<date>-<topic>.md`, every non-obvious claim cites a
source with a date, and inferences are labeled as such. A brief full of unsourced
assertions is not research — redo it.
