# TDD

Write the test before the implementation. Non-negotiable for any behavior change.

## Red → Green → Refactor

1. **Red** — write a test that fails for the right reason (the behavior doesn't exist yet,
   or the bug is present). Run it; confirm it fails with the expected message.
2. **Green** — write the minimum code to make it pass. No extra scope.
3. **Refactor** — clean up with the test still green. Improve names/structure, remove
   duplication.

## Rules

- One behavior per test; name it `test_<action>_<scenario>_<expected>`.
- Test both the success path and the meaningful error/edge cases.
- Mock only external systems (network, third-party APIs, time) — never your own code.
- A test you never saw fail proves nothing. If it passed the first time, break the code
  on purpose and confirm the test catches it.
- Never commit with a failing or skipped test unless the skip is justified in writing.
