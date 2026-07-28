// The Wails system logger, with binding arguments redacted.
//
// WHY THIS FILE EXISTS. Wails logs every binding call at Debug level, arguments included:
//
//	m.Debug("Binding call complete:", "id", ..., "method", ..., "args", string(jsonArgs), ...)
//	(pkg/application/messageprocessor_call.go)
//
// One of this app's bound methods takes an API key. So turning on Wails' debug logging — the
// obvious thing to do while chasing a UI problem — would print the user's credential to the
// terminal and into whatever captured it. Relying on the log LEVEL to prevent that is not a
// safeguard: the level is exactly what someone changes when debugging.
//
// So the args are dropped at the handler, where no future bound method can leak through by being
// added without anyone remembering this. What is lost is worth less than what it protects: the
// method name and id are still logged, which is what a binding problem is actually diagnosed from.
package main

import (
	"context"
	"io"
	"log"
	"log/slog"
)

// redactArgsHandler drops the "args" attribute from every Wails system log record.
//
// Sanitises in BOTH Handle and WithAttrs, and descends into groups. Only Handle would be enough for
// how Wails logs today (one top-level "args"), but a handler that is correct only for the caller
// that happens to exist is not a safeguard: `logger.With("args", secret)` upstream, or an "args"
// nested in a group, would walk straight past it.
type redactArgsHandler struct{ inner slog.Handler }

// redactedValue is what replaces a redacted attribute — present rather than absent, so a reader can
// tell "withheld" from "there was nothing here".
const redactedValue = "[redacted]"

// sanitize replaces any attribute called "args" with the placeholder, recursing into groups.
func sanitize(a slog.Attr) slog.Attr {
	if a.Key == "args" {
		return slog.String("args", redactedValue)
	}
	if a.Value.Kind() == slog.KindGroup {
		grouped := a.Value.Group()
		out := make([]any, 0, len(grouped))
		for _, inner := range grouped {
			out = append(out, sanitize(inner))
		}
		return slog.Group(a.Key, out...)
	}
	return a
}

func (h redactArgsHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h redactArgsHandler) Handle(ctx context.Context, rec slog.Record) error {
	// Rebuilt rather than filtered in place: slog.Record's attributes cannot be removed, only
	// re-added to a fresh record.
	out := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(sanitize(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

// WithAttrs sanitises the pre-attached attributes too. Without this, attributes bound once with
// slog.With would be handed to the inner handler directly and never pass through Handle.
func (h redactArgsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		clean = append(clean, sanitize(a))
	}
	return redactArgsHandler{inner: h.inner.WithAttrs(clean)}
}

func (h redactArgsHandler) WithGroup(name string) slog.Handler {
	return redactArgsHandler{inner: h.inner.WithGroup(name)}
}

// logWriter is where the standard logger writes, so Wails shares the app's single output stream.
func logWriter() io.Writer { return log.Writer() }

// newWailsLogger builds the logger passed to application.Options.
func newWailsLogger() *slog.Logger {
	return slog.New(redactArgsHandler{
		// Through the standard logger's writer, so Wails' output lands in the same stream as
		// everything else this app logs.
		inner: slog.NewTextHandler(logWriter(), &slog.HandlerOptions{Level: slog.LevelInfo}),
	})
}
