//go:build !linux

package pollers

import (
	"errors"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// errStatfsUnsupported is returned on non-Linux platforms. The daemon target
// is Linux (QNAP homelab); this stub exists so tests and dev work compile on
// macOS/Windows.
var errStatfsUnsupported = errors.New("statfs: unsupported platform")

func statfs(_ string) (triagearr.DiskUsage, error) {
	return triagearr.DiskUsage{}, errStatfsUnsupported
}
