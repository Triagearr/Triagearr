// Package arrclient is the shared HTTP plumbing used by the Sonarr and Radarr
// (and any future *arr) clients. It owns the http.Client, request shaping,
// status-code handling, and the 5xx → triagearr.ErrTransient wrapping that
// the Actor's retry loop keys off.
//
// Each *arr package keeps its own ListMedia/ListMediaFiles types and API
// paths — the JSON shapes diverge enough that one shared model would be
// lossy. What's identical (auth header, GET decoding, DELETE with deleteFiles
// query, error wrapping) lives here.
package arrclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

const defaultTimeout = 30 * time.Second

// Options configures BaseClient. Each downstream package re-exports its own
// Options that embeds these and adds nothing today; kept as a shared type
// so cross-package call sites all see the same shape.
type Options struct {
	Label   string
	Name    string
	BaseURL string
	APIKey  string
	Poll    bool
	Act     bool
	Timeout time.Duration
}

// BaseClient is the embeddable HTTP plumbing for *arr clients.
type BaseClient struct {
	label   string
	name    string
	baseURL string
	apiKey  string
	poll    bool
	act     bool
	http    *http.Client
}

// New validates the common required fields and wires the HTTP client. Label
// (e.g. "sonarr") is used in error messages so wrap chains stay readable.
func New(opts Options) (*BaseClient, error) {
	if opts.Label == "" {
		return nil, errors.New("arrclient: Label is required")
	}
	if opts.Name == "" {
		return nil, fmt.Errorf("%s: Name is required", opts.Label)
	}
	if opts.BaseURL == "" {
		return nil, fmt.Errorf("%s: BaseURL is required", opts.Label)
	}
	if opts.APIKey == "" {
		return nil, fmt.Errorf("%s: APIKey is required", opts.Label)
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	return &BaseClient{
		label:   opts.Label,
		name:    opts.Name,
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		apiKey:  opts.APIKey,
		poll:    opts.Poll,
		act:     opts.Act,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

// Name returns the configured instance name.
func (c *BaseClient) Name() string { return c.name }

// Poll reports whether this instance is configured for read access.
func (c *BaseClient) Poll() bool { return c.poll }

// Act reports whether this instance is allowed to perform deletions.
func (c *BaseClient) Act() bool { return c.act }

// BaseURL is exposed for the few callers (e.g. arrhistory) that need to build
// custom requests around our auth.
func (c *BaseClient) BaseURL() string { return c.baseURL }

// Get GETs path, decoding the body into out when non-nil. Non-200 responses
// return a formatted error including the response body for diagnostics.
func (c *BaseClient) Get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("%s: building request %s: %w", c.label, path, err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: GET %s: %w", c.label, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: GET %s: HTTP %d: %s", c.label, path, resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decoding %s: %w", c.label, path, err)
	}
	return nil
}

// DeleteFile sends DELETE <basePath>/<fileID>?deleteFiles=...&addImportExclusion=...
// 5xx and transport failures are wrapped with triagearr.ErrTransient so the
// Actor's retry loop distinguishes them from hard failures (404, 401).
func (c *BaseClient) DeleteFile(ctx context.Context, basePath string, fileID int64, opts triagearr.DeleteOpts) error {
	path := fmt.Sprintf("%s/%d", basePath, fileID)
	q := url.Values{}
	if opts.DeleteFiles {
		q.Set("deleteFiles", "true")
	}
	if opts.AddImportExclusion {
		q.Set("addImportExclusion", "true")
	}
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("%s: building DELETE %s: %w", c.label, path, err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: DELETE %s: %w: %w", c.label, path, triagearr.ErrTransient, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%s: DELETE %s: HTTP %d: %s: %w", c.label, path, resp.StatusCode, string(body), triagearr.ErrTransient)
	}
	return fmt.Errorf("%s: DELETE %s: HTTP %d: %s", c.label, path, resp.StatusCode, string(body))
}

// TagEntry is the shared shape of /api/v3/tag rows.
type TagEntry struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// FetchTags loads /api/v3/tag and returns id→label.
func (c *BaseClient) FetchTags(ctx context.Context) (map[int]string, error) {
	var raw []TagEntry
	if err := c.Get(ctx, "/api/v3/tag", &raw); err != nil {
		return nil, err
	}
	out := make(map[int]string, len(raw))
	for _, t := range raw {
		out[t.ID] = t.Label
	}
	return out, nil
}

// ResolveTags maps a slice of tag IDs to labels, silently dropping unknowns.
func ResolveTags(ids []int, labels map[int]string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if l, ok := labels[id]; ok {
			out = append(out, l)
		}
	}
	return out
}
