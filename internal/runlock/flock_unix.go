//go:build unix

package runlock

import (
	"os"
	"syscall"
)

// tryFlock takes a non-blocking exclusive advisory lock on f. It returns an
// error (EWOULDBLOCK) when another process already holds it.
func tryFlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unflock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
