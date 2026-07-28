# Severity Rubric

Tag every review finding with a severity. P0/P1/P2 must be fixed before shipping; P3 is
optional.

| Level | Meaning | Action |
| --- | --- | --- |
| **P0** | Broken — will crash, lose data, or create a security hole | Fix before proceeding |
| **P1** | Wrong — incorrect behavior, logic error, missing required case | Fix before proceeding |
| **P2** | Poor — code smell, maintainability, unclear intent, missing test | Fix before proceeding |
| **P3** | Nit — style, naming, minor suggestion | May fix; does not block |

**Plan-stage note:** at the plan stage, an omission that would cause the *wrong thing to
be built* (missing required behavior or acceptance criterion) is a **P1**, not a P2.

**Review loop:** run reviewers, collect findings, fix P0/P1/P2, repeat. Exit only when a
single pass yields no P0/P1/P2 from all reviewers.
