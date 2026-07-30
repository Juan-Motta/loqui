package app

import (
	"errors"
	"fmt"
)

// The outward links the interface offers. Today: the donation page.
//
// THE URL LIVES HERE, not in the page, which is how the Electron build had it too (DONATE_URL in
// main.ts). A page-supplied URL would turn this into a general "open anything in the user's browser"
// binding, reachable by anything running script in that webview — a named action cannot be pointed
// somewhere else.
//
// The opener is INJECTED because internal/app must not import Wails: that constraint is what keeps
// the dictation logic buildable and testable without a GUI toolkit attached. It also makes this
// testable — the test asserts which URL is opened, without launching a browser.
type LinksService struct {
	open func(string) error
}

// DonateURL is the same page the Electron build links to.
const DonateURL = "https://buymeacoffee.com/jualopezmo"

func NewLinksService(open func(string) error) *LinksService {
	return &LinksService{open: open}
}

// OpenDonate opens the donation page in the user's default browser. Bound as Links.OpenDonate().
//
// Returns an error rather than swallowing it: this button did nothing at all for a while, and a
// failure that reports nothing is indistinguishable from a button that is not wired.
func (s *LinksService) OpenDonate() error {
	if s.open == nil {
		return errors.New("no hay forma de abrir enlaces en esta compilación")
	}
	if err := s.open(DonateURL); err != nil {
		return fmt.Errorf("no se pudo abrir el navegador: %w", err)
	}
	return nil
}
