package logging

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
)

// redactedValue is the placeholder substituted for any attribute whose key
// matches a secret-bearing name (api_key, apikey, password, bot_token …).
const redactedValue = "***"

// secretKeys matches attribute keys that should never appear in logs. The
// comparison is case-insensitive and substring-based to cover variants like
// `qbit.password`, `arr.api_key`, `telegram_bot_token`.
var secretKeys = []string{"api_key", "apikey", "password", "bot_token"}

// secretQueryParams matches query-string parameter names whose values must
// be scrubbed when a URL leaks into a log (typically via a wrapped error's
// message). Same matching policy as secretKeys.
var secretQueryParams = []string{"apikey", "api_key", "password", "token"}

// RedactHandler wraps an existing slog.Handler and rewrites every record's
// attributes through redactAttr before they reach the underlying writer.
// It honours the With/WithGroup compositional pattern of slog.Handler.
type RedactHandler struct {
	inner slog.Handler
}

// NewRedactHandler wraps inner with the redaction layer.
func NewRedactHandler(inner slog.Handler) *RedactHandler {
	return &RedactHandler{inner: inner}
}

// Enabled forwards to the inner handler.
func (h *RedactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle rebuilds the record with redacted attributes and forwards it. The
// original record is left intact for any caller still holding a reference.
func (h *RedactHandler) Handle(ctx context.Context, r slog.Record) error {
	cloned := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		cloned.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, cloned)
}

// WithAttrs is delegated after redacting the static attrs so an
// inadvertently-bound secret doesn't leak into every record produced by the
// derived logger.
func (h *RedactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &RedactHandler{inner: h.inner.WithAttrs(redacted)}
}

// WithGroup is a straight delegation — the group name itself doesn't carry
// values to redact.
func (h *RedactHandler) WithGroup(name string) slog.Handler {
	return &RedactHandler{inner: h.inner.WithGroup(name)}
}

// redactAttr replaces secret-bearing values with `***` and scrubs known
// secret query parameters from string values. Groups are recursed into so
// nested attrs (e.g. WithGroup("arr").Info(…, "api_key", …)) are covered.
func redactAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		out := make([]any, 0, 2*len(group))
		for _, sub := range group {
			r := redactAttr(sub)
			out = append(out, r.Key, r.Value.Any())
		}
		return slog.Group(a.Key, out...)
	}
	if isSecretKey(a.Key) {
		return slog.String(a.Key, redactedValue)
	}
	if a.Value.Kind() == slog.KindString {
		if scrubbed, changed := scrubSecretsInString(a.Value.String()); changed {
			return slog.String(a.Key, scrubbed)
		}
	}
	return a
}

func isSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, needle := range secretKeys {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// scrubSecretsInString rewrites secret query parameters in any URL-shaped
// substring of s. Non-URL strings are returned unchanged.
func scrubSecretsInString(s string) (string, bool) {
	// Cheap reject: needs an `=` and a `?` to look URL-ish.
	if !strings.ContainsAny(s, "?&") || !strings.Contains(s, "=") {
		return s, false
	}
	// Walk through `?…` and `&…` fragments. Re-encode values for matched
	// secret params; leave the rest of the string intact.
	parts := splitOnAny(s, []byte{'?', '&'})
	if len(parts) <= 1 {
		return s, false
	}
	changed := false
	for i := 1; i < len(parts); i++ {
		// parts[i] still carries its leading separator (?, &) — skip past it
		// when reading the param name.
		nameStart := 0
		if len(parts[i]) > 0 && (parts[i][0] == '?' || parts[i][0] == '&') {
			nameStart = 1
		}
		eq := strings.IndexByte(parts[i], '=')
		if eq <= nameStart {
			continue
		}
		name := parts[i][nameStart:eq]
		// Cut name at the first whitespace, which terminates the param in a free-form message.
		nameClean := name
		if ws := strings.IndexAny(name, " \t\r\n"); ws >= 0 {
			nameClean = name[:ws]
		}
		if !isSecretQueryParam(nameClean) {
			continue
		}
		valStart := eq + 1
		valEnd := len(parts[i])
		// End at the first whitespace or non-URL punctuation that wouldn't
		// belong in a query value, so we don't eat trailing log context.
		for j := valStart; j < valEnd; j++ {
			switch parts[i][j] {
			case ' ', '\t', '\r', '\n', '"', '\'':
				valEnd = j
			}
			if valEnd != len(parts[i]) {
				break
			}
		}
		parts[i] = parts[i][:valStart] + redactedValue + parts[i][valEnd:]
		changed = true
	}
	if !changed {
		return s, false
	}
	// Re-join with the original separators we recorded in splitOnAny.
	return joinWithSeps(parts), true
}

func isSecretQueryParam(name string) bool {
	// URL-decode to catch %5F etc. Fall back on raw if decoding fails.
	if decoded, err := url.QueryUnescape(name); err == nil {
		name = decoded
	}
	lower := strings.ToLower(name)
	for _, needle := range secretQueryParams {
		if lower == needle {
			return true
		}
	}
	return false
}

// splitOnAny splits s on any byte in seps, prefixing each non-first piece
// with the separator that introduced it so the original string can be
// reassembled by simple concatenation.
func splitOnAny(s string, seps []byte) []string {
	isSep := func(b byte) bool {
		for _, c := range seps {
			if b == c {
				return true
			}
		}
		return false
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if isSep(s[i]) {
			out = append(out, s[start:i])
			start = i // keep the separator in the next piece
		}
	}
	out = append(out, s[start:])
	return out
}

func joinWithSeps(parts []string) string {
	return strings.Join(parts, "")
}
