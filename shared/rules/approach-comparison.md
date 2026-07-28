# Approach Comparison

Before committing to a design, compare 2–3 real alternatives on fixed axes. This forces
the tradeoffs into the open instead of defaulting to the first idea.

## Fixed axes

Score each candidate approach (Low / Med / High, with a one-line why):

| Axis | Question |
| --- | --- |
| **Complexity** | How many moving parts / how much new code? |
| **Blast radius** | What breaks if it's wrong? How much does it touch? |
| **Reversibility** | How hard is it to undo or change later? |
| **Time to validate** | How fast can you prove it works (or fails)? |
| **Correctness/User risk** | Likelihood of a wrong result or bad UX? |

## How to use it

1. List 2–3 genuinely different approaches (not variations of one).
2. Fill the table; add a one-line rationale per cell.
3. Name the **default winner** and why. Prefer the simplest option that meets the bar
   (low complexity + low blast radius + high reversibility).
4. **Validate the default with a second opinion** (the `review` or `council` skill) before
   locking it in — do not self-certify the choice.
5. If the cheapest falsifying test is quick (say, under ~30 min), **spike it first** and
   let evidence decide.

Record the table and the chosen approach in the plan / `.workflow/state.md`.
