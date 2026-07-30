//go:build darwin

package macos

import (
	"strconv"
	"strings"
	"syscall"
)

// ProductVersionMajor is the macOS product major version — 26 for Tahoe — or 0 when it cannot be
// read.
//
// WHY IT MATTERS: Apple's SpeechAnalyzer, which the on-device engine uses, arrived in macOS 26. The
// same build installs on macOS 15, where that framework is simply absent and the helper cannot
// launch. So this is a RUNTIME question about the machine in front of the user, never a build-time
// one, and the Conexiones list needs the answer to avoid offering an engine that cannot run.
//
// Zero on failure rather than a guess: the connection model treats an unknown capability as "does
// not disqualify", which is the right way to be wrong — a machine that cannot be interrogated
// behaves as it did before this check existed.
func ProductVersionMajor() int {
	// kern.osproductversion is the user-facing version ("26.5.2"), not the Darwin kernel version
	// (25.x) — those two differ, and the kernel one would compare against the wrong threshold.
	raw, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return 0
	}
	major, _, _ := strings.Cut(strings.TrimSpace(raw), ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}

// ProductVersion is the full user-facing macOS version ("26.5.2"), or "" when it cannot be read.
//
// Same source as ProductVersionMajor, and deliberately NOT built from it: Acerca de is what someone
// pastes into a bug report, and "26" would drop the patch level that usually decides whether a
// framework bug applies. Empty rather than a guess, so the view can show an em dash.
func ProductVersion() string {
	raw, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}
