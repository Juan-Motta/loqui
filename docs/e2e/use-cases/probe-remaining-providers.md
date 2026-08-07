# Use cases — "Probar conexión" for OpenAI, Grok and ElevenLabs

Interface: **UI** (the Conexiones cards in Ajustes). There is no Playwright journey: this is a Wails
WKWebView, not a page on a served origin. The driver is the project's own affordance —
`LOQUI_DEBUG_CONN_CLICK=<provider>:<action>[:<arg>]` dispatches real clicks on the same handlers a
mouse reaches, and `LOQUI_DEBUG_CONN_REPORT=<provider>` reports the state the card **settled** on.

**Two things that will bite whoever re-runs these:**

- Launch **unsandboxed**. Under a tool sandbox the app prints its platform line and then goes
  silent — no paint, no probe — which looks identical to a broken feature.
- Use **braces**: `"${p}:test"`, never `"$p:test"`. zsh reads `:t` as a tail modifier and the app is
  asked for a card named `grokest`.

Last run: 2026-08-07 — all nine PASS. See `docs/e2e/reports/2026-08-07-probe-remaining-providers.md`.

---

## UC-PROBE-01/02/03 — my key is good and I want to know before I dictate

- **Actor:** someone who has pasted a cloud key and wants to confirm it works.
- **Scenario:** they press "Probar conexión" on a card whose key is already stored.
- **Interface:** UI
- **Intent:** that they find out the credential is good **before** discovering it halfway through a
  dictation — and that "good" means the service positively opened a session, never merely that a
  socket opened.
- **Setup:** the key already stored for the slot. The key field left **empty**, so the probe resolves
  the credential the way dictation does. Setup must not press the button.
- **Steps:**
  1. `LOQUI_DEBUG_CONN_CLICK="${p}:test" LOQUI_DEBUG_CONN_REPORT="${p}" ./bin/loqui.app/Contents/MacOS/loqui`
     for `p` in `openai`, `grok`, `elevenlabs`.
  2. Read the settled `CONN-CARD` line, not the click-time one.
- **Verification:** the card shows `✓ Conexión correcta: <provider wording>` with `statusClass:status
  ok`, and the log shows `source=stored` and `ok=true`. The wording must **name the provider** — three
  identical sentences would leave the user unsure which card they tested.
- **Persistence:** none, and that is the assertion — see UC-PROBE-08.
- **Why it matters beyond the button:** `ok=true` is the only observation that confirms the readiness
  event names (`session.created`, `transcript.created`, `session_started`). They came from docs, not
  from observation, until 2026-08-07. If one were wrong the probe would **time out** — a visible, safe
  failure, never a false green.

## UC-PROBE-04/05/06 — I pasted my key wrong

- **Actor:** someone whose key is mistyped, revoked, or copied with a character missing.
- **Scenario:** they press "Probar conexión" with a bad credential in the field.
- **Interface:** UI
- **Intent:** that they are told **it is the key**, with the provider's own machine-readable code so
  they can search for it — and above all that they are never told it is fine.
- **Setup:** the affordance types a fixed invalid sentinel. **Never** pass a real key through the
  environment: it would land in the process environment and in every log that captures it.
- **Steps:** `LOQUI_DEBUG_CONN_CLICK="${p}:test:badkey" LOQUI_DEBUG_CONN_REPORT="${p}" …`
- **Verification:** `✗ <provider> rechazó la API key — revísala en Ajustes (<code>)`, with
  `statusClass:status err`, `ok=false`, and `field:` **empty** (a rejected credential is not a badly
  filled field, so no red border). Expected codes: `invalid_api_key` (OpenAI),
  `AuthenticationFailure` (xAI), `auth_error` (ElevenLabs).
- **Persistence:** none.
- **This is the regression that matters most.** OpenAI and ElevenLabs return **HTTP 101 — accepted**
  for a garbage key and refuse afterwards as an event; ElevenLabs then closes with **1000**, a normal
  close. Any future refactor that infers success from a completed handshake, or from a clean close,
  brings the false green back. These three cases are what catch it.

## UC-PROBE-07 — my key never turns up in a log or on screen

- **Actor:** anyone whose key the service echoes back in its rejection.
- **Scenario:** a probe is refused by a service that quotes the credential in its error.
- **Interface:** UI (plus the app's own log, which is the surface being audited)
- **Intent:** the P0 of this change. OpenAI masks the middle of the key and leaves **the last four
  characters intact**, so redacting the exact string is not enough.
- **Setup:** run UC-PROBE-04/05/06 and keep the logs.
- **Steps:**
  1. Grep every captured log for the invalid sentinel.
  2. Scan for any 8-char window of each real stored credential, **excluding the vendor prefix**
     (`sk-proj-`, `xai-`, `sk_`) — those are public format markers and produce false positives.
- **Verification:** zero occurrences of either.
- **Persistence:** N/A — an audit of output.

## UC-PROBE-08 — testing a connection changes nothing

- **Actor:** anyone who presses the button to find out, not to commit.
- **Scenario:** they run several probes, including failing ones.
- **Interface:** UI
- **Intent:** that a probe is a read. A failed test must leave a working configuration exactly as it
  was, and so must a successful one.
- **Setup:** record size, mtime and sha256 of `secrets.json` and `settings.json` first.
- **Steps:** run every probe above, then re-read both files.
- **Verification:** both files byte-identical, `provider` unchanged.
- **Persistence:** this **is** the persistence check for every other UC here.

## UC-PROBE-09 — a region is not demanded from a service that has none

- **Actor:** someone testing a Grok, OpenAI or ElevenLabs key while Azure's region picker holds a value.
- **Scenario:** they press "Probar conexión" on a non-Azure card.
- **Interface:** UI
- **Intent:** the trap the plan exists to avoid. `TestConnection` once resolved the region for every
  slot, so widening the allowlist alone would demand an Azure region of a service that has no such
  concept.
- **Setup:** an Azure region saved (`eastus2`), so the global setting is non-empty and could leak in.
- **Steps:** run any non-Azure probe and read the `PROBE` line.
- **Verification:** `region=` is **empty** for `openai`, `grok` and `elevenlabs`, while the card report
  still shows `region:eastus2` — the setting exists and is correctly ignored. Azure must still be
  asked for one.
- **Persistence:** none.

---

## Not yet a use case, and why

**The green path of dictation** for these three — that audio flows and text comes back. A green probe
proves the credential and that a session opens, nothing more. Now that credentials exist this is the
next journey worth writing, and it is the one that would move the `⬜` in `CONTINUITY.md`'s provider
table.
