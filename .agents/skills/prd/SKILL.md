---
name: prd
description: Write a product requirements doc before designing — the spec for what to build and who it's for: the problem, users, goals/non-goals, requirements, and success criteria, then hand off to plan. Use at the start of a substantial feature to spec out what a system should do, under Claude Code, Codex, or OpenCode.
---

# prd

Define *what* and *why* before *how*. A PRD keeps design and implementation anchored to a
real user outcome. Feeds the `plan` skill.

Use for substantial features; skip for small changes (go straight to `plan` or
`quick-fix`).

## 1. Clarify the problem

Establish who has the problem and what outcome they need. Ask the user one question at a
time only when genuinely ambiguous; otherwise state assumptions. Avoid jumping to a
solution — this phase is about the need, not the design.

## 2. Draft the PRD

Write `docs/prds/<feature>.md` with:

- **Problem** — what's broken/missing and why it matters.
- **Users / personas** — who this is for; the situation they're in.
- **Goals** and **Non-goals** — what success includes, and what it deliberately excludes.
- **Requirements** — the capabilities the feature must provide (user-facing, not code).
- **Success criteria** — how you'll know it worked, observably.
- **Open questions** — what's still undecided.

## 3. Review + confirm

Sanity-check with the user (and optionally the `review` skill) that the PRD captures the
real intent. Resolve contradictions and vague requirements now — a gap here misdirects
everything downstream.

## 4. Hand off

The PRD feeds `plan` (design) → `new-feature` (build). Reference it from
`.workflow/state.md`.

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "The requirements are obvious — just start designing." | The PRD anchors design to a real user outcome; skipping it lets the build drift from what the user needs. |
| "I'll write the endpoints and tables as the requirements." | Requirements are user-facing capabilities, not code. A PRD that reads like a design doc has skipped the what/why. |
| "Non-goals are unnecessary." | Naming what's deliberately excluded is how scope stays bounded. Omit them and scope creeps. |
| "Success criteria can wait." | Without observable success criteria you can't tell if it worked. Define them up front. |

## Red flags

- The PRD names components/endpoints/tables as the goal.
- No users/personas, or no non-goals.
- No observable success criteria.
- It reads like a design doc rather than a statement of need.

## Verification

`docs/prds/<feature>.md` exists and states the problem, users, goals/non-goals,
requirements, and success criteria in user terms (no endpoints, tables, or code as the
goal). A PRD that reads like a design doc has skipped the "what/why" — rewrite it.
