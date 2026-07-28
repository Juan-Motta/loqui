package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// The Wails system logger must never emit a binding call's arguments.
//
// Wails logs them at debug level (messageprocessor_call.go), and one bound method takes an API
// key — so enabling debug logging, which is the obvious move when chasing a UI problem, would
// print the user's credential. The level is not a safeguard: it is the thing being changed.
func TestTheWailsLoggerRedactsBindingArguments(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(redactArgsHandler{
		inner: slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	})

	const secret = "xai-super-secret-value"
	// Shaped like the real record, including the debug level a developer would turn on.
	logger.Debug("Binding call complete:",
		"id", 1234,
		"method", "SettingsService.SetKey",
		"args", `["grok","`+secret+`"]`,
		"result", "{}",
	)

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("the log contains the secret: %s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Errorf("args were dropped without a trace; want an explicit [redacted]: %s", out)
	}
	// What a binding problem is actually diagnosed from must survive.
	if !strings.Contains(out, "SettingsService.SetKey") {
		t.Errorf("the method name was lost: %s", out)
	}
}

// Records with no args must pass through untouched, so redaction cannot quietly eat other fields.
func TestTheWailsLoggerLeavesOtherRecordsAlone(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(redactArgsHandler{
		inner: slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	})
	logger.Info("window created", "name", "settings", "width", 980)

	out := buf.String()
	for _, want := range []string{"window created", "settings", "980"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in: %s", want, out)
		}
	}
}

// Enabled must delegate, or the handler would silently change which levels are logged.
func TestTheWailsLoggerDelegatesLevelChecks(t *testing.T) {
	var buf bytes.Buffer
	h := redactArgsHandler{
		inner: slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}),
	}
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Info reported as enabled by a Warn-level handler")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Error reported as disabled by a Warn-level handler")
	}
}

// Attributes bound once with slog.With bypass Handle entirely — they go to the inner handler
// through WithAttrs — so the redaction has to happen there as well. A handler that is only correct
// for the one call shape Wails uses today is not a safeguard.
func TestTheWailsLoggerRedactsPreAttachedArguments(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(redactArgsHandler{
		inner: slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	})

	const secret = "xai-super-secret-value"
	logger.With("args", `["grok","`+secret+`"]`).Debug("Binding call complete:")

	if out := buf.String(); strings.Contains(out, secret) {
		t.Fatalf("a pre-attached secret reached the log: %s", out)
	}
}

// And inside a group, which is the other way an attribute reaches the inner handler unexamined.
func TestTheWailsLoggerRedactsArgumentsNestedInAGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(redactArgsHandler{
		inner: slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	})

	const secret = "xai-super-secret-value"
	logger.Debug("Binding call complete:",
		slog.Group("call", slog.String("method", "SetKey"), slog.String("args", secret)),
	)

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("a grouped secret reached the log: %s", out)
	}
	if !strings.Contains(out, "SetKey") {
		t.Errorf("the rest of the group was lost: %s", out)
	}
}
