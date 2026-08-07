package app

import (
	"strings"
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/store"
)

// REVEALING IS THE ONE PLACE THE SECRET IS ALLOWED TO CROSS, so every refusal is part of the
// contract rather than an edge case.
//
// The rest of this app is built the other way round: the payload carries presence and never the
// value (bootstrap.go:29), and the branch before this one closed two real leaks. RevealKey exists
// because the owner asked to be able to check which key is configured, and it is deliberately the
// NARROWEST possible hole — one slot, on an explicit press, never as part of a repaint.
func TestRevealKeyReturnsTheStoredCredential(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault, _, _ := probeService(t, st)
	vault.set(store.SlotOpenAI, "sk-proj-la-clave-guardada")

	res := svc.RevealKey("openai")

	if !res.OK {
		t.Fatalf("reveal failed: %q", res.Error)
	}
	if res.Key != "sk-proj-la-clave-guardada" {
		t.Errorf("key = %q, want the stored credential", res.Key)
	}
}

func TestRevealKeyRefusesWhatItCannotHonestlyShow(t *testing.T) {
	t.Run("an empty slot", func(t *testing.T) {
		st := store.NewAt(t.TempDir())
		svc, _, _, _ := probeService(t, st)

		res := svc.RevealKey("openai")

		if res.OK {
			t.Error("an empty slot must not report success")
		}
		if res.Key != "" {
			t.Errorf("key = %q, want empty", res.Key)
		}
		if !strings.Contains(res.Error, "guardada") {
			t.Errorf("error = %q — it must say there is nothing stored", res.Error)
		}
	})

	// The app does not hold an env-var credential: it did not store it and cannot delete it. Handing
	// its value back would also mean this button answers a different question depending on the slot
	// ("what did you save" vs "what is in my environment"), and the user cannot see which.
	t.Run("a key supplied by the environment", func(t *testing.T) {
		st := store.NewAt(t.TempDir())
		svc, vault, _, _ := probeService(t, st)
		vault.set(store.SlotGrok, "xai-la-guardada-que-nadie-usa")
		t.Setenv("LOQUI_GROK_KEY", "xai-desde-el-entorno")

		res := svc.RevealKey("grok")

		if res.OK {
			t.Error("an env-controlled slot must not be revealed")
		}
		if strings.Contains(res.Key, "xai") || strings.Contains(res.Error, "xai") {
			t.Errorf("neither credential may appear: key=%q error=%q", res.Key, res.Error)
		}
		if !strings.Contains(res.Error, "LOQUI_GROK_KEY") {
			t.Errorf("error = %q — it must name the variable in force", res.Error)
		}
	})

	t.Run("an unknown slot", func(t *testing.T) {
		st := store.NewAt(t.TempDir())
		svc, _, _, _ := probeService(t, st)

		res := svc.RevealKey("no-existe")

		if res.OK || res.Key != "" {
			t.Errorf("an unknown slot must be refused: ok=%v key=%q", res.OK, res.Key)
		}
	})

	// azure-openai is storable but not readable by any engine yet. Revealing there would show a
	// credential nothing uses, on a card whose form is meant to be inert.
	//
	// THE SLOT IS SEEDED ON PURPOSE. Without a key in it the refusal comes from "nothing stored", so
	// deleting the availability gate left this test green — it proved nothing about the gate. Caught
	// by mutation, which is the only reason it is written this way.
	t.Run("a slot no engine can read, WITH a key in it", func(t *testing.T) {
		st := store.NewAt(t.TempDir())
		svc, vault, _, _ := probeService(t, st)
		vault.set(store.SlotAzureOpenAI, "una-clave-que-nada-lee")

		res := svc.RevealKey("azure-openai")

		if res.OK || res.Key != "" {
			t.Errorf("an unusable slot must be refused: ok=%v key=%q", res.OK, res.Key)
		}
		if !strings.Contains(res.Error, "disponible") {
			t.Errorf("error = %q — it must be the availability refusal, not 'nothing stored'", res.Error)
		}
	})
}
