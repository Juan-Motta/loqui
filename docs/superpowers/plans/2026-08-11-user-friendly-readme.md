# User-friendly Bilingual README Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the maintainer-first landing page with an English product-first README, a complete linked Spanish translation, and the supplied Loqui banner.

**Architecture:** Keep the default GitHub entry point in `README.md`, place the semantically equivalent Spanish version beside it as `README.es.md`, and store the shared banner under `docs/assets/`. Preserve advanced build and release knowledge in progressively disclosed sections instead of leading with it.

**Tech Stack:** GitHub Flavored Markdown, HTML `<details>` disclosure blocks, PNG asset, existing Wails/Go/npm task wrappers, shell-based documentation checks.

## Global Constraints

- The default README is English and links prominently to the complete Spanish translation.
- The Spanish README has the same section order, links, commands, warnings, and factual coverage.
- The banner source is `/Users/juanmotta/Downloads/1cc336dd-303b-4e28-b1ee-1e19d57b4c22.png`, exactly 1672×941 RGB PNG; do not regenerate or alter it.
- The release requires Apple Silicon (`arm64`) and declares macOS 14 as its minimum, but public
  releases are currently tested on macOS 26; Apple Speech requires macOS 26 or newer. Do not claim
  runtime Sonoma verification until the separate macOS 14 E2E gate passes.
- Building or running from source requires Xcode 26 or matching Command Line Tools with the macOS 26
  SDK because `macos-stt` always targets `arm64-apple-macos26.0`; this build-host requirement is
  separate from the declared macOS 14 application runtime floor.
- Do not describe credentials as encrypted: stored provider keys live in `~/Library/Application Support/LoquiGo/secrets.json`, mode `0600`, in cleartext.
- Do not use bare `go` in documented project commands; use `scripts/go.sh` or `scripts/task.sh`.
- Preserve verbatim in `README.md` the exact literals enforced by
  `scripts/tests/github-release-workflow-test.sh`; `README.es.md` may translate surrounding prose:
  `GitHub release automation`, `MACOS_CERTIFICATE_P12_BASE64`,
  `MACOS_CERTIFICATE_PASSWORD`, `APP_STORE_CONNECT_API_KEY_P8`,
  `APP_STORE_CONNECT_KEY_ID`, `APP_STORE_CONNECT_ISSUER_ID`, ``Environment `release` ``, and
  `build/config.yml`.
- Do not add dependencies, badges, additional screenshots, application behavior, or unrelated documentation changes.
- Project ship gates prohibit intermediate commits; create one final commit only after plan review, content review, verification, E2E evidence, and workflow state are complete.

---

### Task 0: Cross-engine design review

**Files:**
- Review: `docs/superpowers/specs/2026-08-11-user-friendly-readme-design.md`
- Review: `docs/superpowers/plans/2026-08-11-user-friendly-readme.md`
- Modify: `.workflow/state.md` (ignored local workflow record)

**Interfaces:**
- Consumes: the owner-approved specification and this implementation plan.
- Produces: a review-clean design with every P0/P1/P2 resolved before implementation begins.

- [ ] **Step 1: Run the cross-engine design review**

Use the repository `review` skill with Claude as the reviewer and the approved specification plus
this plan as input.

- [ ] **Step 2: Resolve the review**

Apply every valid P0/P1/P2 correction to the specification and plan. Rebut any inapplicable finding
with exact repository evidence, then rerun the review until no P0/P1/P2 remains.

- [ ] **Step 3: Record the result**

Add the reviewer, iteration, verdict, and resolution summary to `.workflow/state.md`; do not begin
Task 1 until the design-review gate is clean.

### Task 1: Add the repository-owned banner

**Files:**
- Create: `docs/assets/loqui-banner.png`
- Source: `/Users/juanmotta/Downloads/1cc336dd-303b-4e28-b1ee-1e19d57b4c22.png`

**Interfaces:**
- Consumes: the owner-supplied PNG at the absolute source path.
- Produces: the stable relative asset path `docs/assets/loqui-banner.png` consumed by both READMEs.

- [ ] **Step 1: Verify the source before copying**

Run:

```bash
file /Users/juanmotta/Downloads/1cc336dd-303b-4e28-b1ee-1e19d57b4c22.png
sips -g pixelWidth -g pixelHeight /Users/juanmotta/Downloads/1cc336dd-303b-4e28-b1ee-1e19d57b4c22.png
```

Expected: PNG image data, `pixelWidth: 1672`, and `pixelHeight: 941`.

- [ ] **Step 2: Run the missing-asset check to establish RED**

Run:

```bash
test -f docs/assets/loqui-banner.png
```

Expected: exit 1 because the repository asset does not exist yet.
If the asset already exists from a prior execution of this plan, record the RED check as previously
satisfied rather than deleting a valid asset to recreate the failure.

- [ ] **Step 3: Create the asset directory and copy the exact bytes**

Run:

```bash
mkdir -p docs/assets
cp /Users/juanmotta/Downloads/1cc336dd-303b-4e28-b1ee-1e19d57b4c22.png docs/assets/loqui-banner.png
```

- [ ] **Step 4: Verify GREEN and byte identity**

Run:

```bash
test -f docs/assets/loqui-banner.png
cmp /Users/juanmotta/Downloads/1cc336dd-303b-4e28-b1ee-1e19d57b4c22.png docs/assets/loqui-banner.png
file docs/assets/loqui-banner.png
sips -g pixelWidth -g pixelHeight docs/assets/loqui-banner.png
```

Expected: every command exits 0; the repository file remains a 1672×941 PNG.
After this check, `docs/assets/loqui-banner.png` is the durable source of truth; the machine-local
Downloads path is only provenance for the initial copy.

### Task 2: Rewrite the canonical English README

**Files:**
- Modify: `README.md:1-265`

**Interfaces:**
- Consumes: `docs/assets/loqui-banner.png`, public release `v0.1.0`, existing task names from `Taskfile.yml`, and compatibility/privacy facts from the approved specification.
- Produces: the default GitHub landing page and canonical content structure mirrored by `README.es.md`.

- [ ] **Step 1: Establish RED for the missing product-first contract**

Run:

```bash
rg -q '^!\[Loqui — real-time multilingual dictation for macOS\]\(docs/assets/loqui-banner.png\)$' README.md && \
rg -q '^English · \[Español\]\(README\.es\.md\)$' README.md && \
rg -q '^## Download$' README.md && \
rg -q '^## Development$' README.md
```

Expected: non-zero because the existing README has no banner, Spanish link, or product-first download section.

- [ ] **Step 2: Replace the opening and user journey**

Rewrite the current README with the available editor tool (`apply_patch`, Edit, or Write) using this
exact heading order:

```markdown
![Loqui — real-time multilingual dictation for macOS](docs/assets/loqui-banner.png)

English · [Español](README.es.md)

# Loqui

## What is Loqui?
## Download
## Features
## Supported engines
## Install and start dictating
## macOS permissions
## Privacy and API keys
## Development
### Requirements
### Run locally
### Build the app
### Useful commands
### Troubleshooting
### Project structure
## Maintainer releases
```

The user-facing copy must include:

- “Hold the configured key, speak, and Loqui inserts the transcription wherever your cursor is.”
- A primary link to `https://github.com/Juan-Motta/loqui/releases/latest`, identification of the
  current release as `v0.1.0`, and the artifact name `Loqui-0.1.0-macos-arm64.dmg`.
- Compatibility stated precisely: Apple Silicon is required; Loqui declares macOS 14 as its minimum;
  public releases are currently tested on macOS 26; Apple Speech is available only on macOS 26+.
- The published `.sha256` asset as the checksum users can download alongside the DMG.
- A feature list for real-time insertion, multilingual engines, history, local engines, and optional cloud engines.
- A table covering Whisper, Apple Speech, Azure Speech, xAI/Grok, OpenAI Realtime, and ElevenLabs without claiming that cloud engines are offline.
- Installation steps: download the DMG, open it, copy Loqui to Applications, launch it,
  choose/configure an engine, explicitly start and finish the model download from Settings when
  choosing Whisper, grant permissions, and hold the configured key to dictate.
- A first-use note that Whisper requires a user-initiated, approximately 465 MB model download from
  Settings, the download is resumable, and Apple Speech may automatically download its selected
  language model on first use; neither model is bundled in the DMG.
- Permission consequences for Microphone, Accessibility, and Input Monitoring.
- The exact cleartext credential location and FileVault recommendation.

- [ ] **Step 3: Keep developer setup copy-pasteable**

Include these exact normal-path commands:

```bash
git clone https://github.com/Juan-Motta/loqui.git
cd loqui
cd frontend && npm install && cd ..
./scripts/task.sh dev
```

The Requirements subsection must list Go, Node.js/npm, and CMake; state “macOS 26 SDK” in English and
“SDK de macOS 26” in Spanish; explain that Xcode 26 or matching Command Line Tools provide it; and
distinguish this source-build requirement from Loqui's declared macOS 14 runtime floor.

Include these task commands with plain-language outcomes:

```bash
./scripts/task.sh build
./scripts/task.sh package
./scripts/task.sh check
./scripts/task.sh probe:devices
./scripts/task.sh probe:mic
```

State that `build` compiles the frontend and Go application, while `package` also compiles the native
helpers and produces an ad-hoc-signed `bin/loqui.app`. Keep the bare-`go` and Wails PATH
warnings under Troubleshooting, after the normal workflow. Also preserve the warning that ad-hoc
rebuilds require Microphone, Accessibility, and Input Monitoring permissions to be re-granted because
macOS binds those grants to the changing signature.

Document the five provider-specific, non-persisting credential escape hatches:
`LOQUI_AZURE_KEY`, `LOQUI_GROK_KEY`, `LOQUI_OPENAI_KEY`, `LOQUI_AZURE_OPENAI_KEY`, and
`LOQUI_ELEVENLABS_KEY`. State that each variable applies only to its matching provider and avoids
storing that credential in `secrets.json`, except that the Azure OpenAI realtime subservice is not
ported and no engine reads `LOQUI_AZURE_OPENAI_KEY` today. The Spanish version must say that the
“subservicio Azure OpenAI Realtime no está portado”.

Keep the two developer UI affordances (`LOQUI_DEBUG_OVERLAY`, `LOQUI_DEBUG_DICTATE`) and the focused
provider probe examples under Troubleshooting so contributors can diagnose UI and engine failures
without reverse-engineering the task graph.

- [ ] **Step 4: Preserve release operations behind progressive disclosure**

Put the Developer ID and protected GitHub Action instructions under:

```html
<details>
<summary>GitHub release automation and Developer ID setup</summary>

...

</details>
```

Preserve every operational warning and command from the current `README.md` release sections, not
only the following minimum list: the two identities; `loqui-notary`; the local command
`LOQUI_NOTARY_PROFILE=loqui-notary ./scripts/task.sh release:macos`; the five Environment secret
names; the exact phrase ``Environment `release` ``; branch restriction to `main`; required reviewer;
version bump in `build/config.yml`; `./scripts/patch-plists.sh`; `./scripts/task.sh check`; the Team
API-key requirement and explicit rejection of Individual API keys; the instruction “Never paste the
archive, its base64 form, or its password into logs, issues, commits, or pull requests”; the warning
not to park an approval indefinitely or dispatch multiple replacement runs; and the rule to
supersede public releases instead of deleting them. The Spanish version must carry idiomatic but
equally explicit translations, including “Nunca pegues el archivo” and “no dejes una aprobación
pendiente indefinidamente”.

Inside the single disclosure, separate the two runbooks with bold labels — **Local Developer ID
release** and **GitHub release automation** in English, with equivalent Spanish labels — rather than
adding more Markdown headings that would break the paired 9/6 heading hierarchy.

Under Project structure, retain links to `CONTINUITY.md` for current status and
`docs/plans/loqui-go-port.md` for the port/module map.

- [ ] **Step 5: Verify the English structure is GREEN**

Run the Step 1 command again.

Expected: exit 0.

### Task 3: Add the complete Spanish translation

**Files:**
- Create: `README.es.md`

**Interfaces:**
- Consumes: the final English heading order, relative paths, compatibility facts, command blocks, and warning semantics from Task 2.
- Produces: a complete Spanish README linked from the canonical English page.

- [ ] **Step 1: Establish RED for the missing translation**

Run:

```bash
test -f README.es.md
```

Expected: exit 1.
If `README.es.md` already exists from a prior execution of this plan, record the RED check as
previously satisfied rather than deleting a valid translation to recreate the failure.

- [ ] **Step 2: Create the translated README**

Create `README.es.md` with the available editor tool (`apply_patch`, Edit, or Write). Start with:

```markdown
![Loqui — dictado multilingüe en tiempo real para macOS](docs/assets/loqui-banner.png)

[English](README.md) · Español

# Loqui
```

Mirror the English section order with these headings:

```markdown
## ¿Qué es Loqui?
## Descargar
## Funcionalidades
## Motores compatibles
## Instalar y comenzar a dictar
## Permisos de macOS
## Privacidad y API keys
## Desarrollo
### Requisitos
### Ejecutar localmente
### Crear la aplicación
### Comandos útiles
### Solución de problemas
### Estructura del proyecto
## Releases para mantenedores
```

Translate prose idiomatically while preserving every URL, relative path, artifact name, command,
task name, environment variable, secret name, compatibility boundary, and warning from the English
version. The `## Releases para mantenedores` section must use the same `<details>`/`<summary>`
structure as English, with `<details>` at column 0 and the disclosure collapsed by default.

Use these exact copy fragments so focused verification is deterministic: `declara macOS 14`,
`probado en macOS 26`, `modelo de idioma`, `volver a conceder`, `Nunca pegues el archivo`, and
`no dejes una aprobación pendiente indefinidamente`, and `texto plano`. English must keep `do not
park an approval indefinitely`; the checks are case-insensitive so sentence capitalization may vary.

- [ ] **Step 3: Verify the language navigation and required Spanish sections**

Run:

```bash
rg -q '^!\[Loqui — dictado multilingüe en tiempo real para macOS\]\(docs/assets/loqui-banner\.png\)$' README.es.md && \
rg -q '^\[English\]\(README\.md\) · Español$' README.es.md && \
rg -q '^## Descargar$' README.es.md && \
rg -q '^## Desarrollo$' README.es.md && \
rg -q 'Loqui-0\.1\.0-macos-arm64\.dmg' README.es.md
```

Expected: exit 0.

### Task 4: Validate parity, links, commands, and rendered layout

**Files:**
- Modify if verification finds defects: `README.md`
- Modify if verification finds defects: `README.es.md`
- Inspect: `docs/assets/loqui-banner.png`

**Interfaces:**
- Consumes: both completed READMEs and the shared asset.
- Produces: verified GitHub-renderable documentation ready for review.

- [ ] **Step 1: Check Markdown structure and language parity**

Run:

```bash
test "$(rg -c '^## ' README.md)" -eq 9
test "$(rg -c '^## ' README.es.md)" -eq 9
test "$(rg -c '^### ' README.md)" -eq 6
test "$(rg -c '^### ' README.es.md)" -eq 6
diff <(rg -o '^#{2,3} ' README.md) <(rg -o '^#{2,3} ' README.es.md)
test "$(rg -c '^```' README.md)" -eq "$(rg -c '^```' README.es.md)"
test "$(rg -c '^<details>$' README.md)" -eq 1
test "$(rg -c '^<details>$' README.es.md)" -eq 1
! rg -n '[[:blank:]]+$' README.md README.es.md
git diff --check
```

Expected: all commands exit 0.

- [ ] **Step 2: Check local links and documented entry points**

Run:

```bash
test -f docs/assets/loqui-banner.png
test -f README.md
test -f README.es.md
test -f CONTINUITY.md
test -f docs/plans/loqui-go-port.md
test -x scripts/task.sh
test -x scripts/go.sh
test -x scripts/patch-plists.sh
rg -Fq 'https://github.com/Juan-Motta/loqui/releases/latest' README.md
rg -Fq 'https://github.com/Juan-Motta/loqui/releases/latest' README.es.md
rg -Fq 'Loqui-0.1.0-macos-arm64.dmg' README.md
rg -Fq 'Loqui-0.1.0-macos-arm64.dmg' README.es.md
rg -Fq 'Loqui-0.1.0-macos-arm64.dmg.sha256' README.md
rg -Fq 'Loqui-0.1.0-macos-arm64.dmg.sha256' README.es.md
rg -Fq 'CONTINUITY.md' README.md
rg -Fq 'CONTINUITY.md' README.es.md
rg -Fq 'docs/plans/loqui-go-port.md' README.md
rg -Fq 'docs/plans/loqui-go-port.md' README.es.md
tr '\n' ' ' < README.md | rg -Fiq 'declares macOS 14'
tr '\n' ' ' < README.md | rg -Fiq 'tested on macOS 26'
tr '\n' ' ' < README.es.md | rg -Fiq 'declara macOS 14'
tr '\n' ' ' < README.es.md | rg -Fiq 'probado en macOS 26'
rg -Fq 'macOS 26 SDK' README.md
rg -Fq 'SDK de macOS 26' README.es.md
tr '\n' ' ' < README.md | rg -Fiq '465 MB'
tr '\n' ' ' < README.es.md | rg -Fiq '465 MB'
tr '\n' ' ' < README.md | rg -Fiq 'language model'
tr '\n' ' ' < README.es.md | rg -Fiq 'modelo de idioma'
tr '\n' ' ' < README.md | rg -Fiq 're-granted'
tr '\n' ' ' < README.es.md | rg -Fiq 'volver a conceder'
rg -Fq 'secrets.json' README.md
rg -Fq 'secrets.json' README.es.md
rg -Fq '0600' README.md
rg -Fq '0600' README.es.md
rg -Fiq 'cleartext' README.md
rg -Fiq 'texto plano' README.es.md
tr '\n' ' ' < README.md | rg -Fiq 'Never paste the archive'
tr '\n' ' ' < README.es.md | rg -Fiq 'Nunca pegues el archivo'
tr '\n' ' ' < README.md | rg -Fiq 'do not park an approval indefinitely'
tr '\n' ' ' < README.es.md | rg -Fiq 'no dejes una aprobación pendiente indefinidamente'
tr '\n' ' ' < README.md | rg -Fiq 'Azure OpenAI realtime subservice is not ported'
tr '\n' ' ' < README.es.md | rg -Fiq 'subservicio Azure OpenAI Realtime no está portado'
for value in LOQUI_AZURE_KEY LOQUI_GROK_KEY LOQUI_OPENAI_KEY LOQUI_AZURE_OPENAI_KEY LOQUI_ELEVENLABS_KEY LOQUI_DEBUG_OVERLAY LOQUI_DEBUG_DICTATE; do
  rg -Fq "$value" README.md || exit 1
  rg -Fq "$value" README.es.md || exit 1
done
rg -q '^  dev:' Taskfile.yml
rg -q '^  build:' Taskfile.yml
rg -q '^  package:' Taskfile.yml
rg -q '^  release:macos:' Taskfile.yml
rg -q '^  check:' Taskfile.yml
rg -q '^  probe:devices:' Taskfile.yml
rg -q '^  probe:mic:' Taskfile.yml
./scripts/tests/github-release-workflow-test.sh
```

Expected: all commands exit 0.

- [ ] **Step 3: Validate external URLs without downloading release assets**

Run:

```bash
gh release view v0.1.0 --repo Juan-Motta/loqui --json url,tagName,isDraft,isPrerelease,assets \
  | jq -e '.isDraft == false and .isPrerelease == false and .tagName == "v0.1.0"
           and ([.assets[].name] | index("Loqui-0.1.0-macos-arm64.dmg") != null)
           and ([.assets[].name] | index("Loqui-0.1.0-macos-arm64.dmg.sha256") != null)' >/dev/null
git ls-remote https://github.com/Juan-Motta/loqui.git HEAD
```

Expected: release `v0.1.0` exists, is public and non-prerelease; the repository resolves.

- [ ] **Step 4: Render and visually inspect both READMEs**

Render each file through GitHub's Markdown API:

```bash
mkdir -p /tmp/loqui-readme-render/docs/assets
cp docs/assets/loqui-banner.png /tmp/loqui-readme-render/docs/assets/loqui-banner.png
jq -Rs '{text: ., mode: "gfm"}' README.md \
  | gh api -X POST /markdown --input - > /tmp/loqui-readme-render/readme-en.html
jq -Rs '{text: ., mode: "gfm"}' README.es.md \
  | gh api -X POST /markdown --input - > /tmp/loqui-readme-render/readme-es.html
```

Open both temporary HTML files in a browser. Confirm the banner is visible, language links are near
the top, tables fit, code fences are closed, lists render correctly, and the maintainer section is
collapsed by default. If a defect is found, fix both language files where applicable and repeat the
render. The E2E report must describe this as banner-path and local-render evidence, not as GitHub-side
rendering.

### Task 5: Review, evidence, full verification, and final commit

**Files:**
- Create: `docs/e2e/reports/2026-08-11-user-friendly-readme.md`
- Create: `docs/e2e/use-cases/user-friendly-readme.md`
- Modify: `docs/CHANGELOG.md`
- Modify: `.workflow/state.md` (ignored local workflow record)
- Include in final commit: `README.md`, `README.es.md`, `docs/assets/loqui-banner.png`,
  `docs/superpowers/specs/2026-08-11-user-friendly-readme-design.md`,
  `docs/superpowers/plans/2026-08-11-user-friendly-readme.md`, the graduated E2E use cases, and the
  E2E report, plus the ship-time changelog entry.

**Interfaces:**
- Consumes: the reviewed documentation from Tasks 1–4 and the standard workflow gate.
- Produces: one review-clean, verified commit on `docs/user-friendly-readme`.

- [ ] **Step 1: Run implementation review**

After Tasks 1–4, use the repository `review` skill on the complete diff. Resolve every P0/P1/P2 and
record the clean result in `.workflow/state.md`.

- [ ] **Step 2: Record E2E evidence**

Use the repository `verify-e2e` skill to write
`docs/e2e/reports/2026-08-11-user-friendly-readme.md` with top-level `VERDICT: PASS`. Cover English
download discovery, Spanish navigation, fresh-clone developer commands, permission/privacy warnings,
banner path plus local rendering, and advanced release disclosure. Graduate passing journeys into
`docs/e2e/use-cases/user-friendly-readme.md`.

- [ ] **Step 3: Run the full project gate**

Run:

```bash
./scripts/task.sh check
git diff --check
git status --short --branch
```

Expected: project check exits 0, diff check is clean, and status lists only the planned documentation
files on `docs/user-friendly-readme`.

- [ ] **Step 4: Complete and validate workflow state**

Check every standard-profile box in `.workflow/state.md`, binding E2E to the fresh report path, then
run:

```bash
./shared/scripts/check-gates.sh
```

Expected: exit 0 with every required gate satisfied.

- [ ] **Step 5: Create the single final commit**

Run:

```bash
git add README.md README.es.md docs/assets/loqui-banner.png \
  docs/CHANGELOG.md \
  docs/superpowers/specs/2026-08-11-user-friendly-readme-design.md \
  docs/superpowers/plans/2026-08-11-user-friendly-readme.md \
  docs/e2e/use-cases/user-friendly-readme.md \
  docs/e2e/reports/2026-08-11-user-friendly-readme.md
git commit -m "docs: add user-friendly bilingual README"
```

- [ ] **Step 6: Verify the committed result**

Run:

```bash
git status --short --branch
git show --stat --oneline HEAD
```

Expected: clean working tree on `docs/user-friendly-readme`; the commit contains exactly the planned
documentation, E2E evidence, and banner files. Do not push or open a PR until the owner chooses that
outward action.
