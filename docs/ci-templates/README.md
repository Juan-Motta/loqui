# codeforge Verified-tier CI template

`gates.yml` is codeforge's **Verified tier**: CI independently re-runs your project's declared
test command on the PR **merge result**, outside any agent's turn (local git hooks are per-clone
and skip server-side merges; see `shared/rules/ship-gates.md`). That alone is real, but it only
becomes **bad-faith-resistant** — never "proof" — against an actor trying to merge broken or
untested code once the repo is fully configured per the steps below. It does not defend against
a PR that also rewrites its own declared test command; see the CODEOWNERS note in step 4.

## Activate

1. Copy the workflow into place:
   ```bash
   cp docs/ci-templates/gates.yml .github/workflows/gates.yml
   ```
2. Edit the **Recompute** step — replace the placeholder with your real test command
   (`npm test`, `uv run pytest`, `go test ./...`, …). The placeholder `exit 1` is intentional so
   an un-configured check can never pass green.
3. In your repo settings, protect the base branch and make **`gates`** a **required status check**:
   - Enable **"Require status checks to pass before merging"** and select `gates`. When you pick
     it from the list, GitHub also shows an **expected source** for the check — pin it to
     **GitHub Actions**. Without pinning the source, anyone with write access can report a
     `gates` status of `success` from another source (e.g. a Commit Status API call, or a
     differently-named workflow) and satisfy the "required" check without this workflow ever
     having run.
   - Enable **"Require branches to be up to date before merging"** (strict status checks), or
     use a merge queue. Without this, GitHub allows merging after the base branch has advanced,
     so the merge-result commit that `gates` tested can differ from what actually lands — the
     "exact code being merged" assurance in `gates.yml`'s checkout comment only holds once this
     is on (or a merge queue is used).
   - Enable **"Do not allow bypassing the above settings"** and **disallow direct pushes** to the
     protected branch, so UI merges / `gh pr merge` / Dependabot cannot skip the check.
   - Enable **"Dismiss stale pull request approvals when new commits are pushed"**. Without this,
     an actor can get a PR approved, then push a follow-up commit that weakens `gates.yml` or the
     tests/config it runs — the earlier approval still counts, and the PR can merge without a
     fresh human look at the new diff.
4. Add a **CODEOWNERS** rule requiring review on the workflow file **and on the files that define
   what `gates` actually runs** — e.g. `/package.json` and any test config/harness
   (`pytest.ini`, `jest.config.*`, `go.mod`, …) — and turn on **"Require review from Code
   Owners"** in branch protection. Without this, a PR can edit `.github/workflows/gates.yml`
   (weaken the test step, or remove/rename the job), or edit the test command/config those files
   declare, in the same PR it's supposed to gate — and GitHub runs that PR's own version. Example
   `CODEOWNERS`:
   ```
   /.github/workflows/  @your-org/maintainers
   /CODEOWNERS          @your-org/maintainers
   /package.json        @your-org/maintainers
   ```
   **Even with all of this configured, the gate is only as good as a human reviewer actually
   reading the diffs to those files** — CODEOWNERS routes the review; it doesn't perform it.

## Honesty

Until the test command is filled, the check is required, **and** bypass is disabled, this is not
yet the Verified tier. Even then, the tier only becomes **bad-faith-resistant** (never "proof")
against a bad-faith or mistaken actor when **all** of the following hold: the required `gates`
check has its **expected source pinned to GitHub Actions** (step 3) — otherwise anyone with write
access can report a same-named status from elsewhere and satisfy the check without this workflow
running — the workflow file **and** the test-defining files are CODEOWNERS-protected (step 4),
"Dismiss stale pull request approvals when new commits are pushed" is enabled (step 3), "Require
branches to be up to date before merging" is enabled or a merge queue is used (step 3), and "Do
not allow bypassing the above settings" includes admins. Even fully configured, this only routes
the diff to a human
code owner — it still depends on that human actually reading the workflow/test-config change,
not rubber-stamping it. Repo/org **admins** (and some GitHub Apps) can still bypass branch
protection unless you've configured otherwise — decide who that should be.
