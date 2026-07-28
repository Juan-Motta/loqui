---
name: verify-e2e
description: Execute user-journey use cases (API, CLI, and UI) against the running app, classify each, and write a committed evidence report the ship-gate binds to. Use to verify a user-facing change end to end before shipping, under Claude Code, Codex, or OpenCode.
---

# verify-e2e

Run **user journeys** — not unit tests — against real interfaces, then bind the result to
the ship-gate with an evidence report. API, CLI, and UI journeys are all executed. The
driver runs this; it is not a cross-engine review.

## 0. Locate use cases

From the active plan (`new-feature` / `fix-bug`) or, in regression mode, from
`docs/e2e/use-cases/*.md`.

## 1. Validate journey shape (before executing)

Each use case MUST carry: **ID, Actor, Scenario, Interface (API|CLI|UI), Intent, Setup,
Steps (≥2), Verification, Persistence**. A **UI** use case additionally requires:

- **App root** — a repo-relative filesystem path to the owning package that has
  `@playwright/test` installed (never an absolute path).
- **App URL** — the served base origin (an env-var reference is allowed, e.g. `$E2E_APP_URL`).
- **Persistence mechanism** — one of `localStorage` | `sessionStorage` | `cookie` | `server`.

Reject a malformed UC with a reason code and stop — rewrite it, don't execute it:

- `MISSING_ACTOR` · `MISSING_SCENARIO` · `SCENARIO_FLUFF` · `CHEAT_SETUP` (Setup performs the
  action under test) · `THIN_VERIFICATION` (bare status/exit code; for UI, a single
  element-visible check with no outcome language) · `MISSING_PERSISTENCE` ·
  `TOO_SHALLOW` (<2 meaningful steps) · `NOT_USER_JOURNEY` (reads as a unit/contract test) ·
  `WRONG_INTERFACE` · `MISSING_APP_ROOT` (UI UC has no App root) · `MISSING_APP_URL` (UI UC
  has no App URL).

**UI Verification vocabulary:** sees / appears / is shown / the toast says / the row is
highlighted. A bare "the element is visible" check with no outcome language is
`THIN_VERIFICATION`.

## 2. ARRANGE — sanctioned setup only

Public API, signup/login, app CLI, or documented seed commands. **Forbidden:** raw DB writes
(`psql -c "INSERT"`, `mysql -e`, `mongosh --eval`), internal/undocumented endpoints,
file-injection on disk. A broken sanctioned path is a **finding, not a fix-it-here**: report
it (verify runs read-only) and loop the repair back through `fix-bug` / `new-feature` so the
app change gets its own tests and review — **never route around it** and never patch app code
inline during the verify phase. Credentials come from **env vars**, never hard-coded
(graduated use cases are committed).

## 3. Safety

- Default to a **non-production** target; require explicit confirmation otherwise.
- **Redact** secrets/tokens/PII from captured output before writing the report.
- Quote/escape UC-provided values; never `eval` raw UC text.
- Clean up resources you created, or note residual state in the report.
- **UI specifics:** default to a non-production target; redact screenshots and console
  output before writing the report; **traces are never committed** (gitignored, local-only
  artifacts, verified with `git check-ignore`); escape any UC-provided value interpolated
  into a generated script — never `eval` raw UC text; run **headless**.

## 4. ACT + VERIFY per interface

- **API** — `curl`/`httpie`: assert status, body, headers, AND a follow-up request (e.g. GET
  the resource a POST created via its `Location`).
- **CLI** — subprocess: assert stdout/stderr + exit code, AND a second invocation that
  observes the persisted state (e.g. `add` then `list`).
- **UI** — see "UI journeys" below.

No cheating in VERIFY: assert only through the interface under test.

### UI journeys

Playwright (`@playwright/test >= 1.59`, required for `page.ariaSnapshot({ mode: 'ai' })`) is
the documented target tooling. The embedded reference harness below is the canonical
starting point — **adapt ONLY the marked JOURNEY block; the harness around it is verbatim —
do not modify it.**

The flow, a–f:

a. **PREFLIGHT** — resolve `@playwright/test` from the UC's declared **App root**, confirm
   its version meets the `>=1.59` floor, and confirm the artifact directory passes
   `git check-ignore` (fail closed — never write a trace/screenshot to a trackable path).
   See the failure matrix below for how each preflight failure classifies.
b. **DISCOVER** — call `page.ariaSnapshot({ mode: 'ai' })` iteratively and boundedly: drive to
   the step whose controls aren't yet known, then re-snapshot to learn the next step's
   controls. On a failing step, allow **at most one repair re-discovery**; if that also
   fails, **re-run the whole journey from a clean Setup once** before classifying the UC as
   failing. Do not loop discovery indefinitely.
c. **ACT** — drive the journey's Steps through the discovered controls, using role/testid/text
   based locators only — never a raw CSS class selector.
d. **VERIFY** — assert the declared outcome using the UI Verification vocabulary (sees /
   appears / is shown / the toast says / the row is highlighted). A bare "element is visible"
   check with no outcome language is `THIN_VERIFICATION`.
e. **PERSIST** — re-verify per the UC's declared **Persistence mechanism**, using the exact
   reset/re-verify operation for that mechanism — do not substitute a different one:
   - `localStorage` → reload the page, OR open a new page in the **same** browser context;
     assert the value survives.
   - `sessionStorage` → **same-page reload only.** A new page/tab starts a fresh session
     storage and will false-fail a passing implementation.
   - `cookie` / auth session → reload or a new page, scoped to the cookie's actual scope
     (per-context vs. per-browser); reassert the authenticated/observable state.
   - `server` → open a **fresh browser context** (this discards client-side storage), perform
     the sanctioned UI re-login, then reassert the state from the server-backed source of
     truth.
f. **CAPTURE/CLEANUP** — on failure, best-effort capture a screenshot + trace from the
   *active* context/page; verify the artifact directory with `git check-ignore` before
   writing anything (fail closed if it is not ignored). Close all contexts and the browser in
   a `finally`. Traces and screenshots are **never committed** — see `## 6`.

**DETECT/PREFLIGHT failure matrix** — match this exactly, it is a classification, not a
suggestion:

| Condition | Classification | Retry |
| --- | --- | --- |
| `@playwright/test` missing, unresolvable, or `< 1.59` | `FAIL_INFRA` | never (not blind-retried) |
| App/infra navigation failure (server down, timeout) | `FAIL_INFRA` | once |
| Missing `App root` or `App URL` on the UC | `FAIL_INVALID_UC` | no |
| No applicable UI journey exists for this feature | `N/A` | — |

**Under `owner=goal`:** an **unrecoverable** failure halts the run — write a schema-valid
blocker line (`- [ ] BLOCKER — <phase> — <reason> — ts=<ISO-8601>`) and set `status=halted`.
"Unrecoverable" means (a) a browser-install/approval requirement that cannot self-serve, or
(b) a dependency/resolution/version/browser-absence `FAIL_INFRA` — tooling that genuinely
cannot be made to run. It does **not** include an app/infra navigation failure (server
down/timeout): per the matrix above, that is `FAIL_INFRA`, retried once, then a normal
ship-blocking result — not a halt.

### Reference harness (verbatim)

Embedded byte-for-byte from `tools/e2e-ui-ref/run-journey.mjs` — a drift test
(`tools/test/skill-embed-drift.test.mjs`) binds this block to that file, so it can never
silently go stale. Copy it as the starting point for a UI journey's runner script and adapt
only the marked JOURNEY block (the Steps/assertions inside `phase = 'journey'` and the
`PERSIST` branch); everything else carries the ship-gate guarantees (preflight, watchdog,
phase-based classification, capture/cleanup) and must not be modified.

<!-- e2e-ui-ref:start -->
```js
// codeforge verify-e2e — UI journey reference harness (normative; Plan B embeds this region
// verbatim). Adapt ONLY the marked JOURNEY block; the harness around it carries the ship-gate
// guarantees the framework depends on — do not modify it.
import { existsSync, readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { isAbsolute, join, resolve, sep, dirname } from 'node:path';
import { pathToFileURL, fileURLToPath } from 'node:url';
import { realpathSync } from 'node:fs';
import { execFileSync } from 'node:child_process';

import { writeSync } from 'node:fs';
// The last stdout line is ALWAYS the classification. Use SYNCHRONOUS writes: process.exit() can
// truncate async stdout, so writeSync(1, ...) guarantees the payload is flushed before exit.
function done(cls, code, diag) {
  if (diag) writeSync(1, diag.endsWith('\n') ? diag : diag + '\n');
  writeSync(1, `CLASSIFICATION: ${cls}\n`);
  process.exit(code);
}
// Pure, unit-testable version-floor check (exported note: Task 2 Step 3b unit-tests this directly).
export function meetsVersionFloor(v, floor = [1, 59, 0]) {
  const core = String(v).split('-'); // a prerelease (1.59.0-alpha) is BELOW 1.59.0
  const nums = core[0].split('.').map((n) => Number(n));
  if (nums.some((n) => !Number.isInteger(n))) return false; // malformed → fail closed
  for (let i = 0; i < 3; i++) {
    const a = nums[i] ?? 0, b = floor[i];
    if (a !== b) return a > b;
  }
  return core.length === 1; // exactly the floor with NO prerelease suffix passes; a prerelease fails
}

// Import-safety guard: everything below is side-effecting (reads env, may process.exit). Only run
// it when this file is the executed entry script (`node run-journey.mjs`, or the test harness's
// spawnSync(REF)) — NOT when another module imports it (e.g. to unit-test meetsVersionFloor above,
// which must stay import-safe with zero side effects). This is not a defensive no-op: the harness
// is ALWAYS executed as the entry script in real use (Plan B embeds this e2e-ui-ref region
// verbatim as TEXT into the generated harness, never via `import`), so the preamble runs for every
// real invocation. It is intentionally skipped only when this module is imported for unit-testing
// meetsVersionFloor — no else-branch is added here, since emitting output on import would fire
// during that unit test too.
const isMain = Boolean(process.argv[1]) && (() => {
  try { return realpathSync(process.argv[1]) === fileURLToPath(import.meta.url); }
  catch { return false; }
})();

if (isMain) {
  const REPO = process.cwd();
  // Guarantee #1 — App root: repo-relative required (reject absolute), realpath + containment,
  // package.json must exist. createRequire needs an ABSOLUTE package.json filename.
  const rel = process.env.E2E_APP_ROOT;
  if (!rel) done('FAIL_INFRA', 1, 'E2E_APP_ROOT is required (repo-relative owning app package)');
  if (isAbsolute(rel)) done('FAIL_INFRA', 1, `E2E_APP_ROOT must be repo-relative, got absolute: ${rel}`);
  let absAppRoot = resolve(REPO, rel);
  try { absAppRoot = realpathSync(absAppRoot); } catch { done('FAIL_INFRA', 1, `App root does not exist: ${rel}`); }
  if (!(absAppRoot === REPO || absAppRoot.startsWith(REPO + sep))) done('FAIL_INFRA', 1, `App root escapes repo: ${rel}`);
  const appPkg = join(absAppRoot, 'package.json');
  if (!existsSync(appPkg)) done('FAIL_INFRA', 1, `No package.json at App root: ${rel}`);

  // Guarantee #1 — resolve + import + normalize named exports; read version via fs (not a package
  // exports subpath). ALL of this is inside one guard so any failure classifies FAIL_INFRA.
  let chromium, expect;
  try {
    // Authoritative go/no-go is resolvability, not manifest declaration: require.resolve
    // ancestor-climbs node_modules (e.g. to a hoisted monorepo-workspace-root node_modules under
    // pnpm/npm workspaces), and a hoisted-but-resolvable dependency MUST pass — a direct manifest
    // entry in the App root's own package.json is neither required nor sufficient. If resolution
    // fails, enrich the error with an advisory manifest scan (never gate on it).
    const requireFromApp = createRequire(appPkg);
    let resolved;
    try {
      resolved = requireFromApp.resolve('@playwright/test');
    } catch (resolveErr) {
      let advisory = '';
      try {
        const appPkgJson = JSON.parse(readFileSync(appPkg, 'utf8'));
        const declared = { ...appPkgJson.dependencies, ...appPkgJson.devDependencies, ...appPkgJson.peerDependencies };
        if (!declared['@playwright/test']) advisory = ' (also not declared in the App root package.json)';
      } catch { /* advisory text only — never gate on manifest read failures */ }
      throw new Error(`@playwright/test is not resolvable from the App root${advisory}: ${resolveErr.message}`);
    }
    const mod = await import(pathToFileURL(resolved).href);
    ({ chromium, expect } = mod.default ?? mod);       // CJS entry may not surface named exports via import()
    if (!chromium || !expect) throw new Error('@playwright/test did not expose chromium/expect');
    // Locate @playwright/test's package.json by walking UP from the resolved entry to the first
    // package.json whose name === '@playwright/test' (robust to the entry not being at pkg root).
    let d = dirname(resolved), pkgJson;
    for (let i = 0; i < 8 && d !== dirname(d); i++, d = dirname(d)) {
      const p = join(d, 'package.json');
      if (existsSync(p)) { const j = JSON.parse(readFileSync(p, 'utf8')); if (j.name === '@playwright/test') { pkgJson = j; break; } }
    }
    if (!pkgJson) throw new Error('could not locate @playwright/test package.json');
    if (!meetsVersionFloor(pkgJson.version)) throw new Error(`@playwright/test ${pkgJson.version} < 1.59 (ariaSnapshot mode)`);
  } catch (e) {
    done('FAIL_INFRA', 1, `dependency preflight failed: ${e.message}`);
  }

  const APP_URL = process.env.E2E_APP_URL || done('FAIL_INFRA', 1, 'E2E_APP_URL required');
  const MODE = process.env.E2E_MODE ?? 'success';
  if (MODE === 'resolve-only') done('PASS', 0);  // used by the resolution acceptance test

  const ARTIFACT_DIR = process.env.E2E_ARTIFACT_DIR ?? '.';
  // D4/§4.2f: fail closed unless the artifact dir is confirmed git-ignored (traces hold DOM/network
  // /session data and must never be trackable).
  mkdirSync(ARTIFACT_DIR, { recursive: true });
  try {
    execFileSync('git', ['check-ignore', '-q', ARTIFACT_DIR], { cwd: REPO }); // exit 0 = ignored
  } catch {
    done('FAIL_INFRA', 1, `artifact dir failed git check-ignore (not ignored, or not a git repo); refusing to write a trackable path: ${ARTIFACT_DIR}`);
  }
  const PERSIST = process.env.E2E_PERSIST ?? 'client';
  const WATCHDOG_MS = Number(process.env.E2E_WATCHDOG_MS ?? 30000);
  const EXPECT_MS = Number(process.env.E2E_EXPECT_MS ?? 5000);
  const ACTION_MS = Number(process.env.E2E_ACTION_MS ?? 5000);
  const expectCfg = expect.configure({ timeout: EXPECT_MS });   // #4: bounds web-first assertions

  // #5: hard watchdog — cleared ONLY in the finally, after teardownAll() returns (both success and failure paths), so a hanging teardown lets it fire.
  let watchdog = setTimeout(() => { done('FAIL_INFRA', 3, 'watchdog: overall deadline exceeded'); }, WATCHDOG_MS);
  let exitInfo = { cls: 'FAIL_INFRA', code: 1, diag: 'unknown' };
  async function onFailure(err) {
    // #3: preserve the PRIMARY error; best-effort capture from the ACTIVE context/page. Teardown +
    // watchdog-clear happen in the `finally` (so a hanging capture is ALSO watchdog-guarded).
    try { if (activePage) await activePage.screenshot({ path: join(ARTIFACT_DIR, 'failure.png') }); } catch {}
    try { if (activeContext) await activeContext.tracing.stop({ path: join(ARTIFACT_DIR, 'trace.zip') }); } catch {}
    // #2 provenance: record WHICH context (by index into `contexts`) produced the trace above, so a
    // multi-context run (e.g. fail-newcontext) can be proven to have captured from the active
    // (possibly 2nd) context rather than merely asserting "some trace.zip exists".
    try {
      writeFileSync(join(ARTIFACT_DIR, 'trace-context-index.json'), JSON.stringify({ activeContextIndex: contexts.indexOf(activeContext) }));
    } catch {}
    // #6: classification is PHASE-based, not error-name-based. Only assertion phases are FAIL_BUG.
    const cls = (phase === 'journey' || phase === 'persist') ? 'FAIL_BUG' : 'FAIL_INFRA';
    exitInfo = { cls, code: 1, diag: String(err?.stack ?? err) };
  }

  let phase = 'launch';                    // #6: phase drives classification
  let browser, activeContext, activePage;
  const contexts = [];
  try {
    browser = await chromium.launch();
    activeContext = await browser.newContext();
    contexts.push(activeContext);
    activeContext.setDefaultTimeout(ACTION_MS);
    await activeContext.tracing.start({ screenshots: true, snapshots: true });
    activePage = await activeContext.newPage();
    activePage.setDefaultTimeout(ACTION_MS);  // #4: page action timeout (spec requires page.setDefaultTimeout)

    phase = 'nav';
    await activePage.goto(APP_URL);

    phase = 'journey';
    await activePage.getByTestId('note-input').fill('hello');
    await activePage.getByTestId('save').click();
    const target = MODE === 'expect-miss' ? 'never-present-value' : MODE === 'assert-fail' ? 'WRONG' : 'hello';
    await expectCfg(activePage.getByTestId('saved')).toHaveText(target);

    // #6 sub-phases: navigation/context ops during persist are INFRA (phase='nav'); only the
    // re-verify ASSERTION is a product check (phase='persist' → FAIL_BUG). A newContext()/goto()
    // failure here must classify FAIL_INFRA, not FAIL_BUG.
    if (PERSIST === 'client') {
      phase = 'nav';
      await activePage.reload();
      phase = 'persist';
      await expectCfg(activePage.getByTestId('saved')).toHaveText('hello'); // localStorage survives reload
    } else {
      phase = 'nav';
      const fresh = await browser.newContext();          // #2: fresh context becomes active for capture
      contexts.push(fresh);
      await fresh.tracing.start({ screenshots: true, snapshots: true });
      activeContext = fresh;
      activePage = await fresh.newPage();
      activePage.setDefaultTimeout(ACTION_MS);
      await activePage.goto(APP_URL);
      phase = 'persist';
      const want = MODE === 'fail-newcontext' ? 'hello' : '';  // fresh ctx has no localStorage → empty
      await expectCfg(activePage.getByTestId('saved')).toHaveText(want);
    }
    // #5: READY marker — journey has launched+navigated+asserted and is about to enter teardown.
    // Written synchronously so the hang-cleanup test can prove the watchdog fired DURING a hanging
    // teardown, not during cold Chromium launch (which would be a false pass for guarantee #5).
    writeSync(1, 'READY\n');
    exitInfo = { cls: 'PASS', code: 0 };
  } catch (err) {
    await onFailure(err);                    // sets exitInfo + captures (implemented in Task 4)
  } finally {
    // #2: real finally closes only what was created. #5: clear the watchdog ONLY after teardown
    // returns — so a hanging teardown lets the watchdog fire (uncancellable by cleanup).
    await teardownAll();
    clearTimeout(watchdog);                  // reached only if teardownAll() actually returns
  }
  done(exitInfo.cls, exitInfo.code, exitInfo.diag);

  async function teardownAll() {             // #2: close ALL contexts
    if (MODE === 'hang-cleanup') { await new Promise(() => {}); }  // never resolves → watchdog must fire
    for (const c of contexts) { try { await c.tracing.stop().catch(() => {}); await c.close(); } catch {} }
    try { await browser?.close(); } catch {}
  }
}
```
<!-- e2e-ui-ref:end -->

## 5. Classify + verdict

| Result | Blocks ship? | Retry |
| --- | --- | --- |
| `PASS` | no | — |
| `FAIL_BUG` (including a genuine UI assertion failure) | yes | no |
| `FAIL_STALE` (UC references a renamed interface) | yes, until UC updated | no |
| `FAIL_INFRA` — server/app down, navigation timeout | yes if still failing after 1 retry | once |
| `FAIL_INFRA` — UI tooling/dependency/version/browser-absence failure | yes | never (not blind-retried) |
| `FAIL_INVALID_UC` (including a UI UC missing App root or App URL) | yes | no |

**Top-level `VERDICT: PASS` only if every required UC is `PASS`.** Anything else →
`VERDICT: FAIL`.

## 6. Write the evidence report

`docs/e2e/reports/<YYYY-MM-DD>-<feature>.md` (committed). Include a header line
(feature, branch, ISO-8601 timestamp), the top-level `VERDICT: PASS|FAIL`, and one block per
UC (ID, classification, interface, trimmed+redacted output, persistence re-check).

**UI evidence block:** write a **durable text digest** into the committed report — the
finalized locator map (role/testid/text → control), the exact assertions made, sanitized
(redacted) observations, the `@playwright/test` package + browser versions, and the command +
exit status. Screenshot/trace artifact paths (under a gitignored directory such as
`.workflow/e2e-run/`) may be referenced but are **local-only, not reviewable proof** — they
are never committed, so a reviewer without local access cannot see them. The committed text
digest is the evidence that actually counts.

## 7. Graduate passing use cases

Upsert each `PASS` UC by its **ID** into `docs/e2e/use-cases/<feature>.md` so later sessions
re-run it. Then check the `E2E verified` ship-gate box (or record `— N/A: <reason>`).

## Common rationalizations

| Rationalization | Reality |
| --- | --- |
| "Tests pass, that's enough." | Unit tests miss wiring/integration/UX. A journey exercises the real interface. |
| "I'll assert the status code and move on." | A bare 200/exit-0 is `THIN_VERIFICATION`. Observe a real outcome + a next observable step. |
| "I'll seed the row straight into the DB." | Raw DB writes are forbidden ARRANGE. Use the sanctioned interface; if it's broken, report it as a finding and loop through `fix-bug`. |
| "Just check the box — the report can wait." | `check-gates` binds the box to a fresh `VERDICT: PASS` report; an empty claim fails the gate. |
| "It's a read endpoint, skip Persistence." | Only genuinely stateless reads may use `Persistence: N/A`. |
| "I asserted the CSS class is present." | Fragile and disconnected from what a user observes. Use role/testid/text-based assertions instead. |
| "I read the DB directly to confirm the UI worked." | That's a back channel, not the interface under test. Assert through the page. |
| "The screenshot proves it passed." | The assertion is the check. A screenshot is local-only, un-reviewable evidence — not proof. |

## Red flags

- A use case with no Actor/Scenario, or an Intent naming endpoints/tables/components.
- Setup that performs the very action the UC is meant to test.
- Checking the `E2E verified` box without a committed `VERDICT: PASS` report on this branch.
- Verifying through a back channel (DB/logs) instead of the interface under test.
- A UI assertion on a CSS class or selector shape instead of role, test id, or visible text.
- Confirming a UI outcome via a direct DB/API read instead of observing the page itself.
- Treating a screenshot or trace file as proof that a UI journey passed.

## Verification

- [ ] Every use case validated for shape before execution.
- [ ] API/CLI/UI journeys executed; VERIFY only through the interface under test.
- [ ] UI journeys: DISCOVER stayed within the one-repair-then-clean-rerun bound before
      classifying; PERSIST used the exact reset/re-verify operation for the declared
      Persistence mechanism.
- [ ] UI journeys: no CSS-class/selector assertions; screenshots/traces never committed.
- [ ] Report written to `docs/e2e/reports/` with a top-level verdict; secrets redacted.
- [ ] Passing UCs graduated to `docs/e2e/use-cases/<feature>.md` by ID.
- [ ] `E2E verified` gate box checked only with a fresh `VERDICT: PASS` report (or `N/A:`).
