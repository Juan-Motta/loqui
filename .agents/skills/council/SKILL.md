---
name: council
description: Multi-perspective decision analysis — consult two or more engines (Claude, Codex, OpenCode) as independent advisors on one hard, expensive decision, then synthesize a verdict with a mandatory minority report. Use for architecture choices, approach forks, or any fork-in-the-road where being wrong is costly. Not for routine choices with an obvious default.
---

# council

Turn an expensive, hard-to-reverse decision into a diverse, auditable vote. The value is
**model diversity**: independent advisors from *different* engines, synthesized by the
driver acting as chairman.

## When to use

- Architecture decisions, approach forks, tech/tradeoff choices where being wrong is
  costly or hard to undo.

**Do NOT use** for routine choices with an obvious default — that's just the driver
deciding. Council is for genuine ambiguity with real stakes.

## 1. Frame the question

Write ONE decision-shaped question with the concrete options and the constraints that
matter. Not "how do I do X" — rather "should we do A or B, given C, D?". List the options
explicitly. A vague prompt produces vague advice.

## 2. Pick the advisors (must span engines)

Choose **at least two distinct engines** as advisors; the more model diversity, the
better. The driver may include itself as one voice, but the panel must not be a single
engine talking to itself. Optionally assign each advisor a different lens so they don't
all reason the same way, e.g.:

- **Simplicity** — the least-moving-parts option.
- **Blast radius / risk** — what breaks, how reversible.
- **Longevity** — maintainability and cost over time.

## 3. Consult each advisor independently

Send each advisor the **same framed question** (plus its lens), and do it
**independently** — an advisor must not see another's answer, or you lose the diversity.
Use each engine's **non-interactive** mode (Codex `codex exec`, Claude `claude -p`, OpenCode
`opencode run`), spanning all three engines for max diversity. The exact **model IDs, effort,
and read-only invocation** for each engine live in one place — `shared/rules/models.md` —
so use them from there; do not hard-code them here. Advisors must run **read-only** (they
advise, they don't edit) per the read-only invocation in `models.md`.

Capture from each: its position, key reasoning, and a one-line recommendation. If an
advisor's output is missing or unparseable, re-run it — do not invent a position for it.

## 4. Synthesize (chairman)

As the driver, produce the verdict:

- **Agreement** — where advisors converge (the strongest signal).
- **Divergence** — where and why they differ.
- **Verdict** — the recommended decision and the reason.
- **Minority report (mandatory)** — the strongest dissenting view, stated fairly. If every
  advisor agreed, say so explicitly and note the biggest residual risk. Never drop this —
  it is the guard against groupthink.

## 5. Record

Write the framed question, each advisor's one-line position, the verdict, and the minority
report into `.workflow/state.md` (or the relevant plan/decision doc), so the decision is
auditable later.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "I'll ask one engine twice for the two views." | A panel of one engine is groupthink. Advisors must span ≥2 distinct engines or it isn't a council. |
| "Let the advisors see each other's answers to build on them." | Independence is the whole point — shared answers collapse the diversity into one view. Consult each blind. |
| "They all agreed, so I'll drop the minority report." | The minority report is mandatory. If all agreed, state the biggest residual risk — it's the guard against groupthink. |
| "This is a routine call — run a council to be safe." | Council is for expensive, hard-to-reverse forks. For an obvious default, just decide; don't burn the ceremony. |

## Red flags

- All advisors are the same engine.
- Advisors saw each other's positions before answering.
- The verdict has no minority report / residual-risk note.
- You convened a council for a decision that had an obvious default.

## Verification

Before returning, confirm the output contains: the framed question, ≥2 distinct-engine
advisor positions, a verdict, and a non-empty minority report. Missing any of these means
the council did not actually run — redo it rather than presenting a thin verdict.
