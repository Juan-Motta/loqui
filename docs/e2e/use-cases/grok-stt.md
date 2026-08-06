# Use cases — dictation with Grok (xAI)

Interface: **CLI** (`cmd/stt-probe`). The Settings UI is still a stub (phase 4), so there is no
UI journey to execute: the provider picker can be seen at `frontend/index.html:769` but responds
to nothing. The journey through the app (`fn` key → paste → history) will be covered once phase 4
exists and there is a key.

---

## UC-GROK-01 — I am new and have not put my API key in yet

- **Actor:** someone who has just chosen Grok in Settings and has not pasted their key yet.
- **Scenario:** they try to dictate without a credential, and then with one.
- **Interface:** CLI
- **Intent:** that the app says **what** is missing and **where** to fix it, instead of failing in
  a way that looks like a network or microphone problem.
- **Setup:** none. Configure no key (a new user's state is the default state — Setup cannot
  perform the action under test).
- **Steps:**
  1. `env -u XAI_API_KEY ./scripts/go.sh run ./cmd/stt-probe -provider grok -seconds 1`
  2. Repeat **with** a key present, to check that step 1's result came from reading the
     credential and not from always failing the same way.
- **Verification:** step 1 reports `NotConfigured` with a message that names Settings; step 2
  gets **past** configuration and fails on authentication. Two different outcomes ⇒ the key read
  is real.
- **Persistence:** the emitted code, classified by `internal/session`'s retry policy, must give
  `reconnect=false`: a dictation that cannot work must not sit retrying against a service that
  bills by the hour.

---

## UC-GROK-02 — I pasted the key wrong

- **Actor:** someone who pasted a badly copied key.
- **Scenario:** they dictate with a credential the real service rejects.
- **Interface:** CLI
- **Intent:** that they are told **it is the key**, and that the app does not sit retrying.
- **Setup:** export `XAI_API_KEY` with an invalid value. Against the **real** service, which is
  the only thing that can say how it actually rejects.
- **Steps:**
  1. `XAI_API_KEY=<invalid> ./scripts/go.sh run ./cmd/stt-probe -provider grok -seconds 2`
  2. Classify the emitted code with `session.ClassifyCancel` + `session.ShouldReconnect`.
- **Verification:** a single `canceled` whose message names the API key; **not** a generic
  "status 400", which would send the user off to audit their configuration.
- **Persistence:** `reconnect=false`.

---

## UC-GROK-03 — I dictate two sentences and they appear as one message *(BLOCKED)*

- **Actor:** someone with a valid xAI account.
- **Scenario:** they hold the key, say two sentences with a clear pause, release.
- **Interface:** CLI (and later the app)
- **Intent:** that both sentences arrive **joined in one message**, in order and **without
  duplicates** — the failure that Electron's event mapping would produce.
- **Setup:** `XAI_API_KEY` with a valid key.
- **Steps:**
  1. `XAI_API_KEY=<valid> ./scripts/go.sh run ./cmd/stt-probe -provider grok -seconds 20`,
     speaking two sentences with a clear pause.
  2. Log **every event verbatim** and compare against the assembled timeline.
- **Verification:** a single `FINAL` with both sentences, in order, with none repeated.
- **Persistence:** repeat it through the app and check that `history.jsonl` gains **one** record.
- **BLOCKED:** there is no xAI key. It is the same blocker Azure has (see `CONTINUITY.md`).
  This journey is also the experiment that confirms risk 1 in the plan (whether `start` is
  session-relative); it has to be run as soon as there is a credential.
