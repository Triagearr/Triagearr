// Package linker maps a qBit torrent hash to the *arr files that *arr created
// when it hard-linked the torrent into its library (ADR-0012). It is the API-
// only successor to the inode-based mapper: no filesystem access, no path
// remap, no Linux-only syscalls — pure database join over what the *arr
// import history has told us.
package linker

import (
	"context"
	"fmt"
	"strings"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Source is the store contract the linker reads from. `*store.Store`
// implements it directly via `LinksByHash`; tests pass a fake.
type Source interface {
	LinksByHash(ctx context.Context, hash triagearr.Hash) ([]triagearr.Link, error)
}

// Linker resolves hashes to *arr file targets. Stateless aside from the
// Source it wraps; safe for concurrent use.
type Linker struct {
	src Source
}

// New returns a Linker backed by the given source.
func New(src Source) *Linker { return &Linker{src: src} }

// Links returns the per-file links for a qBit torrent. Empty slice when the
// torrent is an orphan (no *arr import history attached).
//
// Hash is normalised to lowercase — qBit reports lowercase, Sonarr/Radarr
// store uppercased downloadId; arr_imports is normalised on insert so the
// query is case-correct, but callers may pass either form.
func (l *Linker) Links(ctx context.Context, hash triagearr.Hash) ([]triagearr.Link, error) {
	normalized := triagearr.Hash(strings.ToLower(string(hash)))
	links, err := l.src.LinksByHash(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("linker: %w", err)
	}
	return links, nil
}
