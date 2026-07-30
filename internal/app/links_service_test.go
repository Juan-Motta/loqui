package app

import (
	"errors"
	"strings"
	"testing"
)

// The URL is the point of this service, so the test asserts the URL — not merely that "something was
// opened". A test that only counted calls would pass with an empty string, and an empty string is
// exactly what a refactor slips in.
func TestOpenDonateOpensTheDonationPage(t *testing.T) {
	var opened []string
	svc := NewLinksService(func(u string) error {
		opened = append(opened, u)
		return nil
	})

	if err := svc.OpenDonate(); err != nil {
		t.Fatalf("OpenDonate: %v", err)
	}
	if len(opened) != 1 {
		t.Fatalf("se abrieron %d URLs, quería 1: %v", len(opened), opened)
	}
	if opened[0] != "https://buymeacoffee.com/jualopezmo" {
		t.Errorf("abrió %q, quería la página de donación del build de Electron", opened[0])
	}
}

// A browser that will not open must SAY so. The button spent this port doing nothing, and "nothing
// happened silently" is indistinguishable from that — which is how it went unnoticed.
func TestOpenDonateReportsAFailureToOpen(t *testing.T) {
	svc := NewLinksService(func(string) error { return errors.New("boom") })
	err := svc.OpenDonate()
	if err == nil {
		t.Fatal("un fallo del navegador se tragó sin reportarse")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("el error %q no conserva la causa original", err)
	}
}

// Constructed without an opener — a wiring mistake in main — must also be loud rather than a no-op.
func TestOpenDonateWithoutAnOpenerIsAnError(t *testing.T) {
	if err := (&LinksService{}).OpenDonate(); err == nil {
		t.Fatal("sin abridor devolvió nil, así que el botón parecería funcionar sin hacer nada")
	}
}
