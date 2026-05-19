// Package whisparr_v2 is a placeholder Whisparr v2 client (Whisparr's stable
// branch). M1 ships a stub satisfying ArrInstance; the real implementation
// arrives post-M5. v2 and v3 have incompatible APIs so they are separate packages.
package whisparr_v2

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

// New constructs a Whisparr v2 stub client.
func New(opts Options) (*stub.Client, error) {
	return stub.New(stub.Options{
		Name: opts.Name, Type: triagearr.ArrTypeWhisparrV2, BaseURL: opts.BaseURL,
		Poll: opts.Poll, Act: opts.Act,
	})
}
