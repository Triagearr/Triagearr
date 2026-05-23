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
	// can't dominate the probe budget.
	seen := map[string]bool{}
	var probed int
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
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("preflight: qBit save_path %q (torrent %q) does not exist in Triagearr's namespace — the container mounts are inconsistent across the stack (ADR-0023)", t.SavePath, t.Name)
			}
			return fmt.Errorf("preflight: stat qBit save_path %q: %w", t.SavePath, err)
		}
	}
	slog.Info("preflight ok", "volume", volumePath, "qbit_paths_probed", probed)
	return nil
}
