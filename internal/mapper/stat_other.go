//go:build !linux

package mapper

import "errors"

// statInode is not implemented outside Linux. The mapper refuses to start on
// other platforms; this stub exists only so non-Linux builds compile (CI matrix,
// local dev on macOS for refactors).
func statInode(_ string) (uint64, uint64, error) {
	return 0, 0, errors.New("mapper: statInode is Linux-only")
}
