//go:build linux

package pollers

import (
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// statfs reads filesystem statistics via the Linux statfs(2) syscall.
// The "available to unprivileged" count (Bavail) is what we surface as free —
// it matches what `df` reports and what real applications can consume.
func statfs(path string) (triagearr.DiskUsage, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return triagearr.DiskUsage{}, fmt.Errorf("statfs %q: %w", path, err)
	}
	// s.Bsize is the filesystem block size — always positive on Linux. The
	// gosec G115 warning about int64→uint64 conversion is a false positive here.
	bsize := uint64(s.Bsize) //nolint:gosec // Bsize is non-negative by syscall contract
	total := s.Blocks * bsize
	free := s.Bavail * bsize
	used := total - free
	var pct float64
	if total > 0 {
		pct = 100.0 * float64(free) / float64(total)
	}
	return triagearr.DiskUsage{
		TotalBytes:  total,
		UsedBytes:   used,
		FreeBytes:   free,
		FreePercent: pct,
	}, nil
}
