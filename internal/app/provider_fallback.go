// Moving off an engine that cannot dictate, onto the one that can.
//
// WHY THIS EXISTS. Choosing an engine and being able to USE it are two different things, and the gap
// between them is silent: the settings page stores the choice, and the failure surfaces at the next
// dictation — a key press that produces nothing, far from whatever caused it. The commonest cause is
// the user deleting the credential of the engine they were using.
//
// THE DEFAULT IS WHISPER, and not because it is first in a list: it is the only engine that needs no
// credential and no network (store.DefaultSettings). Falling back to a cloud engine would mean
// picking one of the user's other keys for them.
//
// TWO THINGS IT MUST NEVER DO, and they are half the design:
//
//   - Move the user off an engine because the app could not CHECK it. A credentials file that is
//     corrupt, truncated or unreadable after a restore says nothing about whether the key is there —
//     treating "I could not read your keys" as "you have no key" would take a working configuration
//     away over a damaged file. (Under the Keychain backend this was the COMMON case, not the rare
//     one, which is why the branch exists at all.)
//   - Move onto an engine that cannot dictate either. Whisper needs a model file the connection model
//     knows nothing about, so its row can read "Disponible" while it transcribes nothing.
//
// A RESIDUAL THAT USED TO LIVE HERE IS GONE, and it is worth recording why rather than leaving the
// next reader to wonder what the retry logic is defending against. While the credentials lived in the
// Keychain, a call that timed out was ABANDONED rather than cancelled — the cgo call could not be
// stopped — so it could land seconds later and leave the app on an engine whose credential had just
// vanished. Every decision here had to treat a failed credential operation as a possible change.
// The credentials are now a file (store/secrets.go): a failed read never reached the write, and a
// rename either happened or it did not, so there is no late arrival to defend against.
//
// WHAT REMAINS is the ordinary kind of staleness, and the retry still earns its place: the payload
// this decides from takes several reads to build, and a user clicking in Ajustes can finish a save
// inside that window. The check is NOT re-run periodically — the trigger fires once per launch (see
// the ui:painted hook in wiring.go), so between launches the only thing that re-decides is deleting
// the key of the engine in use. Widening that is a separate change; pretending it is already wide
// would be worse than the gap.
package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/Juan-Motta/loqui-go/internal/i18n"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// EngineCheck is the outcome of making sure the active engine can dictate.
//
// Changed is separate from Notice because there are three outcomes, not two: it changed, it did not
// change and that is fine, and it did not change but the user needs to know why. Reporting the third
// as a success — which a bare Notice invites — puts a green tick in front of "no se pudo comprobar".
type EngineCheck struct {
	Payload SettingsPayload `json:"payload"`
	// Changed is whether the stored engine actually moved.
	Changed bool `json:"changed"`
	// Notice is what to tell the user, empty when there is nothing worth saying.
	Notice string `json:"notice"`
	// Error is set only when the check itself failed.
	Error string `json:"error"`
}

// EnsureUsableEngine puts the app on an engine that can actually dictate, and says so when it had to
// change anything. Bound as Settings.EnsureUsableEngine().
//
// Idempotent and quiet when there is nothing wrong: it runs at every launch, so a notice for the
// healthy case would be noise at every launch.
func (s *SettingsService) EnsureUsableEngine() EngineCheck {
	// RETRIED, not abandoned, when a setter lands mid-decision. The launch check runs once per
	// session: giving up because the user happened to be saving something would leave the app on an
	// unusable engine for the rest of the run, which is the very thing this exists to prevent.
	//
	// Three attempts because each one rebuilds the whole payload and the interference it guards against
	// is a person clicking, not a loop: two users' worth of coincidence is already generous.
	const attempts = 3
	var last EngineCheck
	for i := 0; i < attempts; i++ {
		// Read BEFORE the payload: anything from here on is newer than the world being judged.
		gen := s.readinessNow()
		p := s.bootstrap.Payload()
		out, err := s.repairEngine(p, repairEvidence{sampled: true, readiness: gen})
		if err != nil {
			return EngineCheck{Payload: p, Error: err.Error()}
		}
		if !out.deferred {
			// out.after is the snapshot taken while the decision still held the lock, so the sentence
			// and the state it describes are the same moment. Rebuilding it here instead would let a
			// selection made in between arrive with a newer revision and a contradicting message —
			// macOS on screen, "se cambió a Whisper" underneath it.
			if out.after != nil {
				return EngineCheck{Payload: *out.after, Changed: out.changed, Notice: out.notice}
			}
			return EngineCheck{Payload: p, Notice: out.notice}
		}
		last = EngineCheck{Payload: p}
	}
	// Every attempt was overtaken. Saying nothing is right — the user is clearly in the middle of
	// configuring something, and whatever they are doing is more current than this check.
	return last
}

// repairEvidence is what the caller already knows that a fresh payload cannot tell.
type repairEvidence struct {
	// sampled says whether readiness below was actually read. A zero counter is a legitimate value —
	// a service that has written nothing yet — so it cannot double as "no sample taken".
	sampled bool
	// readiness is the value of the counter when the payload was taken. Anything that could make an
	// engine usable — a key, a region, a switch — bumps it, so a mismatch means this decision is about
	// a world that no longer exists.
	readiness uint64
	// deletedSlot is a credential this very operation removed. It makes the slot's absence a FACT,
	// which matters because the payload is computed afterwards: if that read comes back "unreadable"
	// the ordinary rule would refuse to act, and the engine would stay selected with a credential
	// that is provably gone.
	deletedSlot store.KeySlot
}

type engineRepair struct {
	changed bool
	notice  string
	// after is the state as it was when the decision was made, taken before the lock was let go. Nil
	// when nothing was written and the caller's own payload still describes the world.
	after *SettingsPayload
	// deferred means no decision was reached because the world moved while it was being read. It is
	// NOT the same as "nothing to do", and collapsing the two is how a check that never ran comes to
	// look like a check that passed.
	deferred bool
}

// repairEngine decides, and writes only when it decides to move.
func (s *SettingsService) repairEngine(p SettingsPayload, ev repairEvidence) (engineRepair, error) {
	// Nothing is decided about a world that was already moving. This guards the NOTICES as well as the
	// write: telling the user "Azure no está listo" while they are in the middle of configuring it is
	// its own kind of wrong, even when nothing is overwritten.
	if !s.readinessStillMatches(ev) {
		return engineRepair{deferred: true}, nil
	}
	active := providerRow(p, p.Provider)

	// An engine this build does not know is as unusable as a misconfigured one, and worse to leave
	// alone: nothing in the app validates a provider read off disk, so a settings file from another
	// version — or edited by hand — would never dictate again and never say why.
	if active.ID == "" {
		return s.moveToDefault(p, ev, s.note(p.Locale, "El motor guardado (%q) no existe en esta versión", p.Provider))
	}
	// The row's verdict is about the credential the SETTINGS model associates with this engine, which
	// is not always the one dictation will read: Azure's row follows the selected sub-service, while
	// buildProvider always opens Speech. When they disagree, "active" describes a configuration that
	// is not the one about to run — and a settings file naming the unported sub-service produces
	// exactly that.
	if runtime, ok := store.RuntimeKeySlotFor(active.ID); ok && string(runtime) != active.KeySlot {
		return s.moveToDefault(p, ev, s.note(p.Locale, "%s está configurado para un servicio que esta versión no puede usar", active.Name))
	}
	if active.State == store.ConnActive {
		// An active DEFAULT engine is not automatically a working one: Whisper's row says "Activo" as
		// soon as its helper is present, and the model it needs is invisible to that model. On a fresh
		// install that is the whole state of the app — nothing dictates, and nothing says why. There is
		// nowhere to fall back to from the fallback, so this reports and changes nothing.
		if active.ID == store.DefaultProvider {
			if problem := s.defaultEngineProblem(p.Locale); problem != nil {
				return s.confirmNotice(ev, s.note(p.Locale, "%s no puede dictar: %v. Descárgalo o elige otro motor en Ajustes", active.Name, problem)), nil
			}
		}
		return engineRepair{}, nil
	}

	// "Could not check" is not "not configured" — unless this very operation proved the credential is
	// gone, in which case there is nothing left to doubt.
	proven := ev.deletedSlot != "" && string(ev.deletedSlot) == active.KeySlot
	// And it only shields the engine when the KEY is the one thing in doubt. Azure without a region
	// cannot dictate whatever its key turns out to be, so unreadable credentials must not be allowed to
	// hide a requirement that is already, definitely, missing.
	onlyTheKeyIsUnknown := store.HasNonSecretConfig(active.ID, s.store().LoadSettings())
	if !proven && onlyTheKeyIsUnknown && keyStateIn(p, active.KeySlot).Status == store.KeyUnreadable {
		return s.confirmNotice(ev, s.note(p.Locale, "No se pudo comprobar la clave de %s: no se pudieron leer las claves guardadas, así que el motor no se ha cambiado", active.Name)), nil
	}

	return s.moveToDefault(p, ev, s.note(p.Locale, "%s no está listo para dictar", active.Name))
}

// moveToDefault switches to the default engine when it can take over, and explains itself when it
// cannot. `because` is the reason the current engine has to go, phrased as a clause.
func (s *SettingsService) moveToDefault(p SettingsPayload, ev repairEvidence, because string) (engineRepair, error) {
	fallback := providerRow(p, store.DefaultProvider)
	if fallback.ID == "" {
		return engineRepair{}, nil
	}
	if fallback.State == store.ConnUnsupported {
		return s.confirmNotice(ev, s.note(p.Locale, "%s, y %s tampoco funciona en este sistema — configura un motor en Ajustes", because, fallback.Name)), nil
	}
	if problem := s.defaultEngineProblem(p.Locale); problem != nil {
		return s.confirmNotice(ev, s.note(p.Locale, "%s, y %s no puede sustituirlo: %v. Configura un motor en Ajustes", because, fallback.Name, problem)), nil
	}

	// CONDITIONAL. The payload this decision rests on takes several reads to build, and the user is in
	// front of a live window the whole time: if they picked an engine while this was thinking, that
	// choice is newer than this decision and wins.
	// The lock spans the last comparison AND the write. Checking and then writing leaves a gap a
	// setter can start in, which is the whole failure this decision is trying not to be.
	if ev.sampled {
		s.readinessMu.Lock()
		defer s.readinessMu.Unlock()
		if s.readiness != ev.readiness {
			return engineRepair{deferred: true}, nil
		}
	}
	moved := false
	if err := s.store().UpdateSettings(func(cfg *store.Settings) error {
		// The provider name catches the other way this can be stale: somebody selected a different
		// engine. (Configuring the SAME one leaves the name identical, which is what the count is for.)
		if cfg.Provider != p.Provider {
			return nil
		}
		cfg.Provider = store.DefaultProvider
		moved = true
		return nil
	}); err != nil {
		return engineRepair{}, fmt.Errorf(i18n.T(i18n.Locale(p.Locale), "no se pudo cambiar de motor: %w", nil), err)
	}
	if !moved {
		return engineRepair{}, nil
	}
	// NOT counted as a readiness change, deliberately, even though switching engines is one. Nothing
	// can observe it: the write happens with the lock held, and the only two callers are the launch
	// check — one per launch, and single-flighted on top of that — and DeleteKey, which already counted
	// its own delete before getting here. Bumping would be free but it would also be untested code
	// standing in for an invariant nothing reads. If a third caller ever appears, or the check starts
	// running more than once, this is the line that has to change.
	//
	// Taken while the lock is still held — deferred above, released only when this function returns —
	// so nothing can slip between the write and the snapshot that reports it. It costs a credential read
	// with the lock held, which is the price of a message that cannot contradict its own screen.
	after := s.bootstrap.Payload()
	return engineRepair{
		changed: true,
		notice:  fmt.Sprintf("%s: se cambió a %s", because, fallback.Name),
		after:   &after,
	}, nil
}

// readinessStillMatches reports whether the world this decision was taken about is still the current
// one: nothing has changed since the payload was sampled, and nothing was mid-change when it was.
//
// A setter that completed since the sample changes the count; one that is still running holds the
// lock, so this call waits for it and then sees the new count.
//
// Unsampled evidence is taken at face value, and that is right for the caller that has it: a setter
// repairing the engine from INSIDE its own critical section, whose payload was taken microseconds
// earlier in the same call. Taking the lock there would deadlock, and there is no stale window to
// defend against.
func (s *SettingsService) readinessStillMatches(ev repairEvidence) bool {
	if !ev.sampled {
		return true
	}
	return s.readinessNow() == ev.readiness
}

// confirmNotice re-checks under the lock that the world the sentence describes is still the current
// one, and withdraws the sentence if it is not.
//
// A NOTICE NEEDS THE SAME GUARD AS A WRITE, which is easy to miss because nothing is being written.
// The check at the top of repairEngine releases the lock immediately, and what follows is slow — a
// settings read, a stat of a 465 MB file — so a setter can complete inside that gap. Then the app says
// "Whisper no puede dictar: falta su modelo" a moment after the user selected a configured Azure: the
// state on screen is right and the sentence under it is about the engine they just left. Withdrawing is
// the correct outcome because the caller retries, and the retry decides about the world that now
// exists.
//
// Unsampled evidence skips it for the same reason readinessStillMatches does: the only caller with it
// is a setter already inside its own critical section, where taking the lock would deadlock and there
// is no gap to defend.
func (s *SettingsService) confirmNotice(ev repairEvidence, notice string) engineRepair {
	if !ev.sampled {
		return engineRepair{notice: notice}
	}
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	if s.readiness != ev.readiness {
		return engineRepair{deferred: true}
	}
	return engineRepair{notice: notice}
}

// providerRow is one engine's row, zero when the payload does not list it.
func providerRow(p SettingsPayload, id string) store.ConnectionRow {
	for _, row := range p.Connections {
		if row.ID == id {
			return row
		}
	}
	return store.ConnectionRow{}
}

// defaultEngineProblem reports what stops the default engine from dictating, or nil.
//
// It exists because "available" in the connection model means the platform and the helper are there,
// which is not the whole story for Whisper: without its model file the helper starts and immediately
// gives up. A unit test must not depend on whether the developer happens to have downloaded 465 MB.
// The LOCALE is a parameter because this error is not for a log — it is embedded as the reason inside
// a sentence the user reads ("%s no puede dictar: %v"). Left untranslated it produced half a sentence
// in each language, which is worse than either one alone. Found by the widened coverage scan.
func (s *SettingsService) defaultEngineProblem(locale string) error {
	if s.defaultProblem != nil {
		return s.defaultProblem()
	}
	model := WhisperModelPath(s.store().Dir())
	info, err := os.Stat(model)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("falta su modelo")
		}
		return fmt.Errorf(i18n.T(i18n.Locale(locale), "no se pudo leer su modelo: %w", nil), err)
	}
	// Existing is not the same as usable. A directory at that path, or a download that stopped
	// halfway, would send the app onto an engine that fails when the helper tries to load it — a
	// fallback onto a second silent failure, which is the one outcome this check exists to prevent.
	if !info.Mode().IsRegular() {
		return errors.New("su modelo no es un archivo")
	}
	// The EXACT size, in BOTH directions, which is how the original classifies it. A floor would accept
	// a download that stopped at 300 MB; accepting anything larger would bless a different file
	// altogether — a mirror serving the medium model, say. Neither loads, and the point of this check
	// is not to hand the app a second engine that fails.
	if info.Size() != WhisperModelBytes {
		return fmt.Errorf(i18n.T(i18n.Locale(locale), "su modelo no es el esperado (%d de %d MB)", nil),
			info.Size()/(1024*1024), int64(WhisperModelBytes)/(1024*1024))
	}
	return nil
}

// note is this file's phrase(): translate the FORMAT, then fill in the values.
//
// It exists because these sentences embed engine names and error text, so the finished string can
// never be a catalogue key — only the format can. Same reasoning as SettingsService.phrase, and the
// reason it is not simply that function is that the notices here are built before any WriteResult.
func (s *SettingsService) note(locale string, format string, args ...any) string {
	return fmt.Sprintf(i18n.T(i18n.Locale(locale), format, nil), args...)
}
