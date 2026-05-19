// Package lidarr is a placeholder Lidarr client. M1 ships a stub that satisfies
// the ArrInstance interface; the real implementation lands when Lidarr support
// becomes a priority (post-M5).
package lidarr

import (
	"github.com/Triagearr/Triagearr/internal/clients/stub"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Options configures the stub client.
type Options struct {
	Name    string
	BaseURL string
	Poll    bool
	Act     bool
}

// New constructs a Lidarr stub client.
func New(opts Options) (*stub.Client, error) {
	return stub.New(stub.Options{
		Name: opts.Name, Type: triagearr.ArrTypeLidarr, BaseURL: opts.BaseURL,
		Poll: opts.Poll, Act: opts.Act,
	})
}
