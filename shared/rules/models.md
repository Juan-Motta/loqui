# Models

Default model + effort per engine for the cross-engine roles (research, review, council).
Maintained by hand — edit this table to change defaults; the skills read from here so
model IDs live in one place, not scattered across skills.

**Principle:** the reviewer/advisor must run on a **different engine than the driver**
(model diversity is the point). The driver's own model is whatever you opened the CLI
with — not pinned by this project.

## Per-engine defaults

| Engine | Model | Effort | Invocation |
| --- | --- | --- | --- |
| **Codex** | `gpt-5.6-sol` | `xhigh` | `codex exec -m gpt-5.6-sol -c model_reasoning_effort="xhigh" --sandbox read-only "<prompt>" < /dev/null` |
| **Claude** | `opus` | `high` | `claude -p --model opus --effort high "<prompt>"` |
| **OpenCode** | `opencode-go/glm-5.2` | default | `opencode run -m opencode-go/glm-5.2 "<prompt>"` |

<!-- codeforge:review-policy:start -->
<!-- DERIVED — do not edit. Re-rendered by the installers from `PROJECT.md` § Review policy,
     which is project-owned and survives `--upgrade`. Editing here is lost on the next install. -->
Default reviewer(s): codex (`gpt-5.6-sol` · xhigh), claude (`opus` · high)
Council advisors: codex (`gpt-5.6-sol` · xhigh), claude (`opus` · high)
<!-- codeforge:review-policy:end -->

Same model/effort per engine regardless of role — the role only decides **which
engine(s)** to use:

| Role | Engine(s) |
| --- | --- |
| **Driver** (implementation / TDD) | the CLI you open (not pinned) |
| **Reviewer** (design + code review) | the non-driver engine |
| **Research** (when delegated) | a non-driver, web/synthesis-capable engine |
| **Council advisors** | per the review policy above (default: all three, max diversity) |

## Running these from an agent (non-interactive)

When an **agent** (not a human at a terminal) runs these invocations through a shell tool,
stdio is not attached to a TTY. Two consequences, both fixed with flags — no wrapper or shim
needed (this stays skills + config):

- **`codex exec` blocks on stdin.** Even with the prompt passed as an argument, it prints
  `Reading additional input from stdin...` and waits forever. Always redirect **`< /dev/null`**
  (shown in the table) so it reads no input and proceeds.
- **A detached `codex exec` can drop its streamed stdout** ([openai/codex#19945](https://github.com/openai/codex/issues/19945)),
  so the agent sees an empty result even though the run succeeded. Capture the final message to
  a file with **`--output-last-message <file>`** and read that file instead of relying on
  stdout. Give each parallel advisor its **own** file (e.g. `/tmp/council-<advisor>.txt`) so
  concurrent `council` runs don't clobber each other.

Claude (`claude -p`) and OpenCode (`opencode run`) return their output normally in
non-interactive mode and need neither workaround.

## Read-only for reviewers / advisors

A reviewer (`review`) or council advisor must not modify the working tree it's judging.
Invoke it read-only: Codex `--sandbox read-only`; for Claude/OpenCode restrict to
read-only (no write/edit tools). Hand it the diff/plan as text and confirm the
working-tree diff is unchanged afterward.

## Cost note

These are quality-first defaults (top models, high effort) because review/council
decisions are where being wrong is expensive. If cost matters, downgrade here — e.g.
Codex `gpt-5.4-mini`, OpenCode `opencode-go/deepseek-v4-flash`, or a lower `--effort` /
`model_reasoning_effort` — and the skills follow automatically.
