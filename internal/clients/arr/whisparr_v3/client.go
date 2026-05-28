// Package whisparr_v3 is a placeholder Whisparr v3 client (Whisparr's eros
// branch). M1 ships a stub satisfying ArrInstance; the real implementation
// arrives post-M5. v2 and v3 have incompatible APIs so they are separate packages.
package whisparr_v3

import (
	"github.com/Triagearr/Triagearr/internal/clients/arr/stub"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Options configures the stub client.
type Options struct {
	Name    string
	BaseURL string
	Poll    bool
	Act     bool
}

// New constructs a Whisparr v3 stub client.
func New(opts Options) (*stub.Client, error) {
	return stub.New(stub.Options{
		Name: opts.Name, Type: triagearr.ArrTypeWhisparrV3, BaseURL: opts.BaseURL,
		Poll: opts.Poll, Act: opts.Act,
	})
}
