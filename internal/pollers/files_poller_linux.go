//go:build linux

package pollers

import (
	"fmt"
	"os"
	"syscall"
)

// DefaultStatNlink is the production StatNlink for Linux. It reads st_nlink
// from the syscall.Stat_t backing os.Stat. Safe under ADR-0023: the qBit
// save_path resolves identically in Triagearr's namespace, so we can stat the
// inode the operator's whole stack already shares.
func DefaultStatNlink(path string) (int64, int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.Size(), 0, fmt.Errorf("stat %q: unexpected Sys() type %T", path, fi.Sys())
	}
	//nolint:gosec // G115: st_nlink fits in int64 on every supported Linux arch
	return fi.Size(), int64(sys.Nlink), nil
}
