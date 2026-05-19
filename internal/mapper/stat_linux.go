//go:build linux

package mapper

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// statInode returns the inode number and hardlink count for a path. Used by
// the mapper to confirm that a qBit-reported file and an *arr-reported file
// share an inode (the safe-deletion precondition).
func statInode(path string) (ino uint64, nlink uint64, err error) {
	var s unix.Stat_t
	if err := unix.Stat(path, &s); err != nil {
		return 0, 0, fmt.Errorf("stat %q: %w", path, err)
	}
	// Nlink is uint64 on amd64 but uint32 on arm64 — keep the explicit widen.
	return s.Ino, uint64(s.Nlink), nil //nolint:unconvert // platform-dependent type
}
