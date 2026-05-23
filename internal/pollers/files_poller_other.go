//go:build !linux

package pollers

import "errors"

// DefaultStatNlink is the non-Linux stub. The daemon targets Linux; this
// exists so dev/test on macOS/Windows compiles. Callers will see an error and
// upsert nlink=NULL — the Decider then treats that torrent as "unknown,
// pre-filter abstains".
func DefaultStatNlink(_ string) (int64, int64, error) {
	return 0, 0, errors.New("stat nlink: unsupported platform")
}
