package store

import (
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// ts formats a time.Time for storage. We use ISO 8601 / RFC 3339 nanosecond
// precision in UTC so any sqlite tool (usql, sqlite3 CLI, Datasette, DBeaver)
// can render timestamps directly. modernc.org/sqlite parses this format back
// into time.Time transparently on read.
//
// The per-domain repositories live in their own *_repos.go files in this
// package; this file holds only the helpers shared across all of them.
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// hashPlaceholders builds the "?,?,…" fragment for an `IN (…)` clause over a
// set of torrent hashes, plus the matching positional args. Callers splice the
// fragment into their query and pass the args through. Returns an empty
// fragment for an empty set — guard against that before building the query.
func hashPlaceholders(hashes []triagearr.Hash) (string, []any) {
	placeholders := make([]string, len(hashes))
	args := make([]any, len(hashes))
	for i, h := range hashes {
		placeholders[i] = "?"
		args[i] = string(h)
	}
	return strings.Join(placeholders, ","), args
}
