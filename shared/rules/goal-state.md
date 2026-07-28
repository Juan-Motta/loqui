# /goal state & log schemas

Fixed-order, single-line records `/goal` writes to `.workflow/state.md` and the shipped
`shared/scripts/goal-state.sh` / `goal-digest.sh` parse. Single active loop (v1) — **no `nonce`**.

## `## /goal loop` (one key/value row per field)

| Field     | Value                                                              |
| --------- | ------------------------------------------------------------------ |
| goal      | one-line feature objective                                         |
| status    | active | awaiting-gate1 | awaiting-gate2 | halted | done           |
| phase     | preflight|prd|research|plan-review|tdd|code-review|verify|ship     |
| step      | `<phase>:<N>/<M>` task cursor (e.g. tdd:16/20)                     |
| reentries | code-review re-entry count (for the global cap)                    |
| base_sha  | merge-base SHA at loop start (the digest base)                     |
| gate1     | `approved ts=<ISO> prd=<path>` (empty until approved)              |

Read fields with `sh shared/scripts/goal-state.sh field <name>`.

## `## Review log` line

`- loop=plan|code — round=<N> — kind=round|recert|cert — reviewer=<engine|self> — result=clean|P0=a/P1=b/P2=c — digest=<sha> — ts=<ISO>`

The breaker counts `kind=round` lines per loop: `sh shared/scripts/goal-state.sh round-count <plan|code>`.
Certification digest = the `digest=` on the latest `kind=cert` line for the loop.

## `## /goal loop` markers + gate2 line

- Reviewer manifest (written at preflight): `- REVIEWERS set=<engine,…,self> — ts=<ISO>` — a round
  certifies only when exactly this set is clean at one digest.
- SIMPLIFY once: `- [x] SIMPLIFY done — digest=<sha> — ts=<ISO>`.
- GATE 2 authorization: `- [x] GATE2 authorized — action=push+pr — head=<committed sha> — branch=<b> — remote=<r> — ts=<ISO>`.

## `## Blockers` / `## Attempts`

- Blocker (HALT): `- [ ] BLOCKER — <phase> — <reason> — ts=<ISO>`. HALT is terminal for automation.
- Ship-red counter: `- ATTEMPT ship-red — n=<k> — ts=<ISO>`; bump with
  `sh shared/scripts/goal-state.sh ship-red-bump` before each ship-side `check-gates` red; `n>=2` → HALT.
