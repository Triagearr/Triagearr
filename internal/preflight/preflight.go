// Package preflight enforces ADR-0023 at boot: it samples a handful of qBit
// save_paths and stat()s them in Triagearr's namespace. A path the operator's
// qBit reports but Triagearr can't see means the container is mounted
// inconsistently — every downstream assumption (Decider prefix match, T3.5
// nlink stat, files poller) is broken, so we refuse to start.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Qbit is the subset of the qBit client preflight needs (one ListTorrents call).
type Qbit interface {
	ListTorrents(ctx context.Context) ([]triagearr.Torrent, error)
}

// StatFn is the namespace probe. os.Stat in production; injected in tests.
type StatFn func(path string) (os.FileInfo, error)

// SampleSize is how many qBit save_paths preflight probes. Five gives signal
// across distinct categories (tv/movies/manual/etc.) without spending more
// than a handful of syscalls.
const SampleSize = 5

// Validate runs the boot-time mount-convention check. Returns nil when the
// convention holds (or there is nothing to check yet — empty qBit, fresh
// deploy). Returns an error naming the offending path on a violation.
//
// volumePath is always stat'd: it must resolve as a directory in Triagearr's
// namespace, otherwise the mount is missing outright. qBit save_paths are
// probed up to SampleSize; the first unresolved path triggers a refuse-to-start
// with a diagnostic naming both the path and what Triagearr expected to see.
func Validate(ctx context.Context, qb Qbit, volumePath string, stat StatFn) error {
	if stat == nil {
		stat = os.Stat
	}

	if volumePath != "" {
		fi, err := stat(volumePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("preflight: configured volume.path %q does not exist in Triagearr's mount namespace — verify the container's volume mount matches the qBit/*arr stack (ADR-0023)", volumePath)
			}
			if errors.Is(err, fs.ErrPermission) {
				return fmt.Errorf("preflight: configured volume.path %q is unreadable by Triagearr's UID — adjust the container user (typically PUID/PGID of the media owner) or relax filesystem ACLs; this is a UID issue, not a mount layout issue", volumePath)
			}
			return fmt.Errorf("preflight: stat volume.path %q: %w", volumePath, err)
		}
		if !fi.IsDir() {
			return fmt.Errorf("preflight: volume.path %q is not a directory in Triagearr's namespace", volumePath)
		}
	}

	if qb == nil {
		slog.Info("preflight: qbit disabled, skipping save_path probe")
		return nil
	}
	tors, err := qb.ListTorrents(ctx)
	if err != nil {
		// qBit unreachable is a regular runtime concern handled by the qBit
		// poller — preflight does not fail boot on it (ADR-0023 §6 is about
		// the *mount* convention, not qBit connectivity).
		slog.Warn("preflight: qbit ListTorrents failed — skipping save_path probe", "err", err)
		return nil
	}

	// Sample distinct save_paths so a single misconfigured download category
	// can't dominate the probe budget. Permission errors fail-fast (UID
	// misconfig is a global issue, not a stale-torrent edge case); ENOENT is
	// tolerated up to a quorum threshold so one legitimately-stale qBit
	// entry (category dir manually removed) doesn't block boot.
	seen := map[string]bool{}
	var probed, missing int
	var firstMissing string
	var firstMissingTorrent string
	for _, t := range tors {
		if probed >= SampleSize {
			break
		}
		if t.SavePath == "" || seen[t.SavePath] {
			continue
		}
		seen[t.SavePath] = true
		probed++
		if _, err := stat(t.SavePath); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return fmt.Errorf("preflight: qBit save_path %q (torrent %q) is unreadable by Triagearr's UID — adjust the container user (typically PUID/PGID of the media owner) or relax filesystem ACLs; this is a UID issue, not a mount layout issue", t.SavePath, t.Name)
			}
			if errors.Is(err, os.ErrNotExist) {
				missing++
				if firstMissing == "" {
					firstMissing = t.SavePath
					firstMissingTorrent = t.Name
				}
				continue
			}
			return fmt.Errorf("preflight: stat qBit save_path %q: %w", t.SavePath, err)
		}
	}
	// Quorum: refuse boot only when EVERY probed path is missing — one
	// surviving path is enough signal that the mount layout itself is OK.
	if probed > 0 && missing == probed {
		return fmt.Errorf("preflight: every probed qBit save_path is missing in Triagearr's namespace (first: %q, torrent %q) — the container mounts are inconsistent across the stack (ADR-0023)", firstMissing, firstMissingTorrent)
	}
	if missing > 0 {
		slog.Warn("preflight: some qBit save_paths are missing — likely stale qBit entries, not a mount issue",
			"probed", probed,
			"missing", missing,
			"first_missing_path", firstMissing,
			"first_missing_torrent", firstMissingTorrent,
		)
	}
	slog.Info("preflight ok", "volume", volumePath, "qbit_paths_probed", probed, "qbit_paths_missing", missing)
	return nil
}
