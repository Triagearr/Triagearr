// Package torrentregistry builds the live torrent-client instance from
// configuration. The registry mirrors internal/clients/registry but for
// torrent clients (ADR-0025): exactly one instance per kind, with kind as
// the sole identity.
//
// Today only the qbittorrent kind has a backend. transmission/deluge/rtorrent
// are scaffolded (UI shows tiles, HTTP rejects PUTs with 400, daemon refuses
// to start with enabled=true) until their clients land.
package torrentregistry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/clients/qbit"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Kind names the known torrent client flavours.
type Kind string

// Known torrent client kinds. Only KindQbittorrent is implemented today.
const (
	KindQbittorrent  Kind = "qbittorrent"
	KindTransmission Kind = "transmission"
	KindDeluge       Kind = "deluge"
	KindRtorrent     Kind = "rtorrent"
)

// KnownKinds enumerates every kind the registry recognises (implemented or
// scaffolded). Doubles as the validation whitelist for the HTTP layer.
var KnownKinds = []Kind{KindQbittorrent, KindTransmission, KindDeluge, KindRtorrent}

// KnownKind reports whether kind names a torrent client flavour the registry
// recognises — including scaffolded ones.
func KnownKind(kind string) bool {
	for _, k := range KnownKinds {
		if string(k) == kind {
			return true
		}
	}
	return false
}

// ImplementedKind reports whether kind has a backend implementation today.
// The HTTP layer rejects PUTs for known-but-unimplemented kinds with 400.
func ImplementedKind(kind string) bool {
	return Kind(kind) == KindQbittorrent
}

// Registry owns the constructed TorrentClient (at most one in V1). The kind
// is preserved so callers can identify what backend is running.
type Registry struct {
	kind   Kind
	client triagearr.TorrentClient
}

// BuildFromConfig instantiates the torrent client described by cfg.
// At most one instance is built (the enabled kind, if any); disabled kinds
// are silently skipped. An enabled but unimplemented kind returns an error.
func BuildFromConfig(cfg *config.Config) (*Registry, error) {
	tc := cfg.TorrentClients
	if inst := tc.Qbittorrent; inst.Enabled {
		c, err := qbit.New(qbit.Options{
			BaseURL:  inst.URL,
			Username: inst.Username,
			Password: inst.Password,
			Timeout:  inst.Timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("torrentregistry: building qbittorrent: %w", err)
		}
		return &Registry{kind: KindQbittorrent, client: c}, nil
	}
	for _, group := range []struct {
		kind Kind
		inst config.TorrentClientInstanceConfig
	}{
		{KindTransmission, tc.Transmission},
		{KindDeluge, tc.Deluge},
		{KindRtorrent, tc.Rtorrent},
	} {
		if group.inst.Enabled {
			return nil, fmt.Errorf("torrentregistry: kind %q is scaffolded but has no backend yet", group.kind)
		}
	}
	return &Registry{}, nil
}

// Active returns the constructed torrent client. The second return is false
// when no torrent client is enabled — callers treat that as "no destructive
// path available, observation-only mode".
func (r *Registry) Active() (triagearr.TorrentClient, bool) {
	if r.client == nil {
		return nil, false
	}
	return r.client, true
}

// ActiveKind returns the kind of the active torrent client, if any.
func (r *Registry) ActiveKind() (Kind, bool) {
	if r.client == nil {
		return "", false
	}
	return r.kind, true
}

// TestConnection builds an ephemeral client for kind and probes connectivity,
// returning the underlying error so the UI can show the operator why the test
// failed. It does not touch the live registry. Scaffolded kinds return a
// "not implemented" error.
func TestConnection(ctx context.Context, kind, baseURL, username, password string, timeout time.Duration) error {
	switch Kind(kind) {
	case KindQbittorrent:
		c, err := qbit.New(qbit.Options{
			BaseURL:  baseURL,
			Username: username,
			Password: password,
			Timeout:  timeout,
		})
		if err != nil {
			return err
		}
		// A successful ListTorrents proves auth + reachability in one round-trip.
		if _, err := c.ListTorrents(ctx); err != nil {
			return err
		}
		return nil
	case KindTransmission, KindDeluge, KindRtorrent:
		return errors.New("not implemented yet — backend coming in a future release")
	default:
		return fmt.Errorf("unknown torrent client kind %q", kind)
	}
}
