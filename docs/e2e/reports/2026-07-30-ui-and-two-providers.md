# E2E evidence — UI adjustments, inert views and two new providers

VERDICT: PARTIAL

> **Deliberately NOT `PASS`.** `check-gates.sh` only accepts `VERDICT: PASS` to count the box as
> met, and this report cannot declare it: **ElevenLabs and OpenAI realtime have not been run
> against their real service** — there are no credentials for either on this machine. Everything
> else —every interface adjustment, the tutorial, About, the links, the activation at launch— was
> verified against the packaged app. Putting `PASS` would be signing off as tested a port whose
> network path nobody has actually walked. The ship-gate box goes as `N/A:` with the reason in
> plain sight.

- **Date:** 2026-07-30
- **Branch:** `fix/home-engine-select-height` (the name was born for a dropdown and ended up
  carrying ten topics)
- **Target:** the packaged app (`bin/loqui.app`), with the environment-variable-gated probes, and
  **local** WebSocket servers for the two new providers' lifecycle.
- **Result:** **8 journeys PASS**, **2 BLOCKED** for lack of a credential.

## How it was measured, and why that is worth saying

Three things invalidated measurements during this session and changed the method:

1. **Fixed coordinates in screenshots are worthless.** The window appears in a different place on
   every launch. A claim of mine —that "About" opened offset— was **false**: a crop 30 px out of
   place. The crops here are **anchored to the logo's gradient**, and the DOM measured from inside
   wins over the pixel.
2. **Other apps steal the foreground.** Several screenshots came out occluded; one measured a photo
   of a cable car. The script retries by comparing a known region and checks that the app is
   visible.
3. **Dock icons cannot be measured in screenshots.** Tile size depends on how many icons there are
   and launching the app reorders them: three attempts gave 38, 61 and 32 px for the same thing.
   The **generated files** are compared instead.

## UC-01 — control heights → **PASS**

Measured in the packaged app, top and bottom edge in pixels:

```
Home dropdown                 444..478   35 px
History — search              478..512   35 px
History — date select         478..512   35 px
History — options button      478..512   35 px
System — language             696..730   35 px
System — microphone           851..885   35 px
```

Before: Home's was 20 px, the date one 20, System's 21. Cause: WKWebView draws a native popup and
discards the padding (`systemtray_darwin.m` is the equivalent for the tray; here it is the native
control).

## UC-02 — the date column in Recent activity → **PASS**

All four dates end at the same right edge, including the two-line row. Before, they stuck to the
text when the entry was short.

## UC-03 — System saves without a button → **PASS**

```
SYS-PROBE   map[control:appearance value:light via:radio]   ← through the real control, not the binding
SYS         map[appearance:light ...]
=== app closed, reading from disk ===
appearance = 'light'
```

Starting from `dark`, one click on the radio → the app dead and the value on disk is `light`. The
probe **clicks the radio**, it does not call the setter: driving the binding would pass with the
listener absent.

## UC-04 — About reports for real → **PASS**

```
ABOUT   map[pathRows:3 systemRows:4 version:Versión 0.1.0]
```

On screen: `macOS 26.5.2 (arm64)`, `en-CO`, `go1.26.5`, `v3.0.0-alpha2.119` and the three real
paths.

## UC-05 — the tutorial shows, both ways → **PASS**

The footer button, with the flag already `true` so that auto-open does not fake the result:

```
WIZARD  map[open:true step:0 steps:6]
WIZARD  map[configControls:1 engines:6 permRows:5 prefsControls:4 step:0 steps:6]
```

First time, flag `false` and no probe: it opens by itself. On finishing, `onboarded` goes from
`False` to `True` on disk with the app closed. The probe **clicks the real button** in the footer.

## UC-06 — the window comes to the front at launch → **PASS**

```
open  #1 #2 #3 : visible=true miniaturized=false key=true appActive=true
term  #1 #2 #3 : visible=true miniaturized=false key=true appActive=true
```

Three runs per route. Before, from the terminal: `key=false appActive=false` — visible and
**behind**, never minimised. The first measurement after the fix gave `appActive=false` with `open`
and looked like a regression; repeating showed it was another app stealing the foreground.

## UC-07 — the donation links open → **PASS**

```
DONATE  map[found:true probe:openDonate]    DONATE  map[from:openDonate ok:true]
DONATE  map[found:true probe:aboutDonate]   DONATE  map[from:aboutDonate ok:true]
```

It opened two real tabs in the browser. The probes click the real buttons.

## UC-08 — the engine picker does not offer what does not work → **PASS**

```
whisper                = Whisper — local (offline)
macos                  = macOS — local (offline)
azure       [disabled] = Azure Speech — sin configurar
openai      [disabled] = OpenAI — sin configurar
grok        [disabled] = Grok (xAI) — sin configurar
elevenlabs  [disabled] = ElevenLabs — sin configurar
```

It matches the Connections cards (`CONN … azure=unconfigured openai=unconfigured …`). With
`provider=azure` saved and no key, that option stays visible and the hint says *"Este motor
necesita configuración — ábrela en Ajustes"*: a `<select>` cannot show a value with no option, and
hiding it would make the picker lie about the engine in effect.

## UC-09 — dictating with ElevenLabs → **BLOCKED**

There is no ElevenLabs key. **Not executed.**

What is verified, against a local WebSocket server (real sockets, not simulated interfaces — what
fails in these providers is order and timing, and a stub does not reach that): handshake, audio as
JSON with base64, ordering of audio before the handshake, release before the handshake, several
`committed_transcript` joined without truncation, classification of 401/403/429/503 and of the 400
that is really an invalid key, buffer cap, ordering of the closing events. **12 network journeys.**

**Declared gap:** swapping `flush` and `finalize` in the stop branch passes the whole suite. Noted
in the code; a test that repeated the race 24 rounds also passed with the order swapped and was
deleted for claiming coverage it did not have.

## UC-10 — dictating with OpenAI realtime → **BLOCKED**

There is no OpenAI key. **Not executed.**

Verified against a local server, **14 journeys**, including the one that only exists in this
provider:

```
480 samples arrived for 320 sent   (16 kHz -> 24 kHz)
```

The audio is **resampled on the wire**. Without that, the service accepts 16 kHz in a session
declared at 24 and transcribes a sped-up voice — it does not error, so nothing else would report
it. Also: `session.update` before any audio (including the case with audio already in the buffer,
which is the only one that distinguishes the order), the key in the subprotocols and not in the
URL, deltas shown as a growing phrase, the `completed` with no transcript falling back to the
deltas, and unclosed deltas making it to the final.

## What this report does not say

- That the two new providers transcribe. **It has not been checked.** With a key, a real dictation
  through `cmd/stt-probe` would close it.
- That the focus ring on the Home picker is resolved. It is not.
- That the ordering gap in ElevenLabs is covered. It is not, and it is written where it can be
  seen.
