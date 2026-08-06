# Silent success is a bug (and it deleted a credential)

## Problem

The user, linking Azure from Settings → Connections: "when I put the key in and hit test connection,
use engine, delete key and save, none of the actions work". Later, in more detail: "the feedback for
use this engine is also strange, it only shows up for a very quick fraction of time"; "save is more
of the same, a very quick `...` that I can't even catch".

## Root cause — four, not one

The app's logs (`UI-ACTION`) proved that **two of the four buttons did call the backend and both
calls succeeded**. The symptom was single; the causes were four:

1. **"Test connection" was never ported.** `#test` with no handler and no bound method, even though
   `azure.TestConnection` existed and had been unit-tested since the port. Dead code from the user's
   side.
2. **A disabled button looked the same as an active one.** There was no `button.btn:disabled` rule at
   all, and `button.btn` sets colour, background and hover.
3. **Success was not stated.** `run()` painted `WriteResult.Error`, which on success is the empty
   string: the in-progress `…` was replaced by nothing.
4. **A success could change nothing visible.** Choosing Azure without a key is saved correctly, but
   the state is still `unconfigured`, so the badge and the button did not move.

## What makes this more than cosmetic

With a key saved, the user pressed **"Delete key"**. The call ran, succeeded, said nothing — and the
`azure-speech` item vanished from the Keychain. **A destructive action that completes in silence is
indistinguishable from a broken button**, and the difference is only discovered when the credential
is needed. The reported symptom was "it doesn't work"; the fact was "it worked and didn't tell you".

## Solution

- A setter's result carries **`Notice`** as well as `Error`, and the page paints `✓ notice` or
  `✗ error` with the classes the CSS already had. Go decides the text because it depends on facts
  only Go has: what was actually written, what state the engine ended in, what Azure replied.
- The in-progress text **names the activity** ("Guardando…", "Probando la conexión…") instead of a
  `…` that reads as a flicker.
- **A postcondition, not a narration**, when the action is idempotent: `store.DeleteKey` treats
  "there was nothing" as success, so the message is "La clave ya no está guardada" and not "Clave
  borrada", which would be false exactly when the user presses it on an empty slot.
- **`Field`** in the result names the input to flag; the page applies the class and deduces nothing.

## Prevention — the five rules this bug leaves behind

1. **An action with no observable result is not finished.** True of all four: the one that does not
   exist, the one that is off without looking it, the one that succeeds in silence and the one that
   succeeds without changing anything.
2. **If it is destructive, silence is a safety failure, not a UX one.**
3. **A message should describe the final state, not the action**, whenever the operation is
   idempotent or its scope depends on what was there.
4. **The three states of a credential do not collapse.** "There is no key", "the Keychain did not
   answer" and "the environment variable is blank" send the user to three different places. This
   change confused them in the plan **three times in a row** and all three were caught by the
   cross-engine review.
5. **What gets tested has to be what gets saved, byte for byte.** The probe trimmed the secret and
   the save did not: `" key "` went green and was persisted with the spaces. A green tick over a
   credential different from the stored one is worse than having no test button.

## What the process found, not the code

- **Six plan-review iterations** before a line was written. Four design failures of mine, one of
  which would have turned a "it tells me nothing" bug into a "it lies to me" one: my arbitration
  discarded the authoritative repaint from a Save, leaving the key saved and the card saying the
  opposite.
- **15 production mutations** against the new tests. **Two passed green** and exposed vacuous tests
  of mine: one asserted an order it did not check (it passed a written key, which short-circuits
  before reaching the Keychain), and another fix went untested until the mutation pointed at it.
  `CONTINUITY.md` already carried this lesson from the previous session; it still holds.
- **A one-shot modern `tsc`** over a project that does not type-check found
  `slot.status === "configured"` in the tutorial: `KeyStatus` is `present|absent|unreadable`, so the
  branch was dead and anyone who already had a key was asked to paste one.
