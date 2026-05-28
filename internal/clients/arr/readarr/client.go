// Package readarr is a placeholder Readarr client. M1 ships a stub satisfying
// ArrInstance; the real implementation arrives post-M5.
package readarr

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

// New constructs a Readarr stub client.
func New(opts Options) (*stub.Client, error) {
	return stub.New(stub.Options{
		Name: opts.Name, Type: triagearr.ArrTypeReadarr, BaseURL: opts.BaseURL,
		Poll: opts.Poll, Act: opts.Act,
	})
}
