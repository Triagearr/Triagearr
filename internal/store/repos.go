package store

import "time"

// ts formats a time.Time for storage. We use ISO 8601 / RFC 3339 nanosecond
// precision in UTC so any sqlite tool (usql, sqlite3 CLI, Datasette, DBeaver)
// can render timestamps directly. modernc.org/sqlite parses this format back
// into time.Time transparently on read.
//
// The per-domain repositories live in their own *_repos.go files in this
// package; this file holds only the helpers shared across all of them.
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
