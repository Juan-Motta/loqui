# Enabling /goal autonomous mode (per-engine permissions)

`/goal`'s capability preflight requires the loop's own actions — spawning the cross-engine
reviewer/council CLI, and the **post-GATE-2** `git push` / `gh pr create` — to be **prompt-free**,
because an autonomous loop cannot answer a native permission prompt (it would silently stall). The
human pause is the **explicit GATE 1 / GATE 2 approvals `/goal` issues itself**, not the native
prompt — so allow-listing these commands does NOT remove human control. **Force-push stays denied.**

Apply the entries for your engine, then re-run `/goal`.

## Claude Code — `.claude/settings.json`

Move push/PR from `ask` to `allow` and allow reviewer/council spawns (keep the force-push deny):

```json
{
  "permissions": {
    "allow": ["Bash(git push:*)", "Bash(gh pr create:*)", "Bash(codex exec:*)", "Bash(claude -p:*)", "Bash(opencode run:*)"],
    "deny": ["Bash(git push --force:*)", "Bash(git push -f:*)"]
  }
}
```

## OpenCode — `opencode.json`

Change `git push*` / `gh pr create*` from `ask` to `allow` (reviewer spawns are already covered by
the `"*": "allow"` default; keep the force-push deny):

```json
{ "permission": { "bash": { "git push*": "allow", "gh pr create*": "allow" } } }
```

## Codex — `.codex/config.toml`

`approval_policy = "on-request"` prompts when a command crosses the sandbox boundary (push/PR do).
For an unattended `/goal` run, set `approval_policy = "never"` (GATE 2 remains the human control),
or add an execpolicy `.rules` file that allows exactly `git push` / `gh pr create`. Do NOT relax
`sandbox_mode` beyond `workspace-write`.

If you do not want to grant these, do not use `/goal` — run the interactive workflows
(`new-feature` / `finish-branch`) where the native prompt is appropriate.
