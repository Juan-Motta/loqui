# Research

Model knowledge has a cutoff. Before designing anything that touches an external library,
API, protocol, or a problem others have already solved, **check current sources first** —
don't build on stale assumptions.

## When research is needed

- A new or unfamiliar library / framework / API / CLI is involved.
- The behavior depends on a spec, protocol, or third-party service.
- The problem is a well-trodden one worth learning from prior art before reinventing it.

Skip it only for changes fully contained in code you already understand.

## What a good brief contains

Write the brief to `docs/research/<YYYY-MM-DD>-<topic>.md`:

1. **Question(s)** — what you needed to find out.
2. **Findings** — current, sourced facts (cite the doc/URL and the date checked).
   Separate verified facts from inference.
3. **Prior art** — how established tools/products solved this, and what to borrow or avoid.
4. **Implications for the design** — what the findings change about the approach.
5. **Open questions** — what remains unverified and how to close it.

## Rules

- Cite sources; never present recalled-from-memory API details as current fact.
- Prefer official docs over blog posts; note the version/date.
- The brief feeds the plan — the design must be consistent with it.
