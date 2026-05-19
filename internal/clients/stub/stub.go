// Package stub is shared scaffolding for *arr clients that are declared in
// the config schema but not yet implemented. M1 ships stubs for Lidarr,
// Readarr, Whisparr v2 and Whisparr v3 — they satisfy the ArrInstance
// interface so the registry can include them, but every operational method
// returns a "not implemented" error tagged with the milestone that owns it.
package stub

import (
	"context"
	"errors"
	"fmt"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Client is a non-functional *arr client. It carries identity and the
// configured Poll/Act flags so it shows up correctly in `inspect arrs`.
type Client struct {
	name string
	typ  triagearr.ArrType
	url  string
	poll bool
	act  bool
}

// Options configures a stub.
type Options struct {
	Name    string
	Type    triagearr.ArrType
	BaseURL string
	Poll    bool
	Act     bool
}

// New constructs a stub client.
func New(opts Options) (*Client, error) {
	if opts.Name == "" {
		return nil, errors.New("stub: Name is required")
	}
	if opts.Type == "" {
		return nil, errors.New("stub: Type is required")
	}
	return &Client{
		name: opts.Name,
		typ:  opts.Type,
		url:  opts.BaseURL,
		poll: opts.Poll,
		act:  opts.Act,
	}, nil
}

// Name returns the configured instance name.
func (c *Client) Name() string { return c.name }

// Type identifies the *arr flavour this stub represents.
func (c *Client) Type() triagearr.ArrType { return c.typ }

// Poll reports whether this instance is configured for read access.
func (c *Client) Poll() bool { return c.poll }

// Act reports whether this instance is allowed to perform deletions.
func (c *Client) Act() bool { return c.act }

// HealthCheck reports the stub as unhealthy with an explanatory error.
func (c *Client) HealthCheck(_ context.Context) error {
	return fmt.Errorf("%s client not implemented in M1", c.typ)
}

// ListMedia returns nil + an explanatory error.
func (c *Client) ListMedia(_ context.Context) ([]triagearr.MediaItem, error) {
	return nil, fmt.Errorf("%s ListMedia not implemented in M1", c.typ)
}

// DeleteMedia is destructive and explicitly not implemented.
func (c *Client) DeleteMedia(_ context.Context, _ triagearr.MediaID, _ triagearr.DeleteOpts) error {
	return fmt.Errorf("%s DeleteMedia not implemented (lands in M5)", c.typ)
}

var _ triagearr.ArrInstance = (*Client)(nil)
