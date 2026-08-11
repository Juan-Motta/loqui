# User-friendly bilingual README design

## Context

Loqui has reached its first public macOS release, but its README still opens with cgo and Wails
troubleshooting. That information is useful to maintainers, yet it makes a new visitor learn the
build system before learning what Loqui does, whether their Mac is supported, or how to install it.

The project also has a finished promotional banner and needs a Spanish version without making the
default GitHub landing page visually repetitive.

## Goals

- Explain Loqui and its main capabilities before discussing implementation details.
- Give non-developers a direct path from the repository to the public macOS release.
- Give contributors a short, accurate path to prerequisites, local development, packaging, and
  verification.
- Keep English as the default README and provide a complete Spanish translation through a prominent
  language link.
- Preserve the operational warnings that prevent broken builds, permission confusion, or misleading
  privacy expectations.

## Non-goals

- Change application behavior, packaging, release automation, or supported platforms.
- Produce a marketing website or duplicate the full internal design history.
- Add badges, screenshots beyond the supplied banner, or installation mechanisms that do not exist.
- Rewrite project plans, E2E evidence, or continuity records as part of this change.

## Audience and reading order

The README serves two audiences in this order:

1. A macOS user deciding whether to download and use Loqui.
2. A contributor or maintainer who wants to run, package, test, or release it.

The page must follow a product-first progression:

1. Banner and language navigation.
2. One-paragraph product explanation.
3. Public download and compatibility.
4. Features and supported speech engines.
5. Installation, first use, permissions, and privacy.
6. Development prerequisites and local setup.
7. Development, build, package, test, and diagnostic commands.
8. Focused troubleshooting and repository structure.
9. Advanced maintainer release process.

## Files and navigation

- `README.md`: canonical English version and default GitHub landing page.
- `README.es.md`: complete Spanish translation with the same section order and command blocks.
- `docs/assets/loqui-banner.png`: repository-owned copy of the supplied 1672x941 banner.

Both READMEs display the same banner using a relative path. Immediately below it, a compact language
line links to the other language; the current language is plain text or emphasized so the reader can
see where they are.

## User-facing content

The opening must describe Loqui in plain language: hold the configured key, speak, and Loqui inserts
the transcription wherever the cursor is. It should state that processing can use local or optional
cloud engines without implying that every engine is local.

The download section links to GitHub's stable `/releases/latest` URL and identifies the current
public release as `v0.1.0`, including its concrete artifact. It
must distinguish declaration from runtime evidence: Loqui requires Apple Silicon and declares macOS
14 as its minimum, while public releases have so far been tested on macOS 26. Apple Speech is
available only on macOS 26 or newer. The README must not present the deployment declaration as proof
of runtime Sonoma support until the separate macOS 14 E2E gate passes.
The download section also names the published `.sha256` asset so users can verify the DMG.

The feature summary covers real-time dictation, multilingual engines, insertion into the active app,
history, local Whisper, Apple Speech, and the Azure, xAI/Grok, OpenAI Realtime, and ElevenLabs cloud
providers. Claims stay factual and avoid unsupported promises such as universal automatic language
detection, zero latency, or complete offline operation.

First-use instructions explain mounting the DMG, moving Loqui to Applications, launching it, choosing
an engine, and granting Microphone, Accessibility, and Input Monitoring permissions. The copy must
explain the consequence of each permission, especially that transcription can succeed while text
insertion silently fails without Accessibility.

The first-use path must also set download expectations for local engines. Whisper requires the user
to start a resumable, one-time download of its approximately 465 MB model from Settings before
dictating. Apple Speech may automatically download the selected language model on first use. Neither
model is bundled in the DMG.

Privacy receives a visible, concise section. Local engines do not send audio to a cloud provider.
Cloud engines require their own credentials. Stored credentials live in
`~/Library/Application Support/LoquiGo/secrets.json`, mode `0600`, in cleartext; FileVault is the
recommended protection at rest. The README must not call this storage encrypted or imply that one
provider's key can be reused for another.

For contributors and temporary runs, preserve the documented per-provider environment-variable
escape hatches. Each functional variable applies only to its matching provider, and using one is the
supported way to run without persisting that credential in `secrets.json`. Keep
`LOQUI_AZURE_OPENAI_KEY` for configuration completeness but state that the Azure OpenAI realtime
subservice is not ported and no dictation engine reads that slot today.

## Developer content

Prerequisites are macOS, Apple Silicon for the supported package path, Go, Node.js/npm, CMake, and
Xcode 26 or matching Command Line Tools with the macOS 26 SDK. The build-host SDK requirement is
separate from the application's declared macOS 14 runtime floor: the `macos-stt` helper is always
compiled for `arm64-apple-macos26.0`. Setup uses the project-owned wrappers:

```bash
git clone https://github.com/Juan-Motta/loqui.git
cd loqui
cd frontend && npm install && cd ..
./scripts/task.sh dev
```

The instructions distinguish the main developer outcomes:

- `./scripts/task.sh dev` for hot reload.
- `./scripts/task.sh build` to compile.
- `./scripts/task.sh package` to create an ad-hoc-signed `bin/loqui.app`.
- `./scripts/task.sh check` for the complete project gate.
- `./scripts/task.sh probe:devices` and `probe:mic` for focused diagnostics.

The developer guidance preserves the warning that ad-hoc rebuilds require macOS permissions to be
granted again, plus `LOQUI_DEBUG_OVERLAY` and `LOQUI_DEBUG_DICTATE` for focused UI testing.

The README keeps two build invariants prominent but moves them out of the opening: do not use bare
`go`, because the Azure SDK cgo flags come from `scripts/go.sh`; and prefer `scripts/task.sh`, because
it resolves the pinned Wails CLI even when `wails3` is absent from `PATH`.

The advanced release section remains at the end inside an HTML `<details>` block. It preserves the
Developer ID, notarization, protected GitHub Environment, and version-release workflow information
without making it part of the normal setup path. Preserve every operational warning from the current
release instructions, including safe `.p12`/base64 handling, the Team API-key requirement, serialized
approval behavior, the local `release:macos` entry point, and the rule never to delete a public
release. Secret names may be documented, but secret values must never appear.

The project-structure section retains maintainer orientation links to `CONTINUITY.md` and
`docs/plans/loqui-go-port.md`.

## Translation contract

The Spanish README is a full semantic translation rather than a summary. Headings, links, command
blocks, warnings, compatibility statements, and section order must remain paired. Product names,
paths, task names, environment variables, and filenames are not translated.

Natural language should be idiomatic in each language instead of matching sentence structure word
for word. Any factual change made during implementation must be applied to both files.

## Verification

Verification is documentation-focused:

- Confirm the banner is a valid PNG at the documented relative path.
- Render both Markdown files and visually inspect the banner, language links, headings, lists,
  tables, code blocks, and `<details>` section. The local render must preserve the banner's relative
  path so a visible image is real evidence rather than a broken `/tmp` reference.
- Check that every repository-relative link resolves.
- Confirm every documented script/task exists and that the commands match `Taskfile.yml`.
- Compare the heading and code-block structure of both languages.
- Run `git diff --check` and the repository's normal documentation-safe validation.
- Confirm the change is limited to the two READMEs, banner, design/plan artifacts, workflow evidence,
  and local ignored workflow state.

## Success criteria

- A first-time visitor can understand the product, compatibility, and download path before seeing
  developer internals.
- A contributor can go from a fresh clone to development mode or an ad-hoc application package using
  commands copied directly from the README.
- English and Spanish readers receive equivalent instructions and warnings.
- The banner renders from a repository-owned path on GitHub.
- No existing operationally important build, privacy, permission, or release warning is lost.
