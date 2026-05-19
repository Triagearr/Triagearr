// Package radarr is a minimal Radarr v3 API client used by the observation poller.
//
// M1 scope: HealthCheck + ListMedia. DeleteMedia is declared but returns
// "not implemented" — destructive ops land in M5.
package radarr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/clients/arrhistory"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Client speaks Radarr's v3 REST API.
type Client struct {
	name    string
	baseURL string
	apiKey  string
	poll    bool
	act     bool
	http    *http.Client
}

// Options configures the client.
type Options struct {
	Name    string
	BaseURL string
	APIKey  string
	Poll    bool
	Act     bool
	Timeout time.Duration
}

// New constructs a Radarr client.
func New(opts Options) (*Client, error) {
	if opts.Name == "" {
		return nil, errors.New("radarr: Name is required")
	}
	if opts.BaseURL == "" {
		return nil, errors.New("radarr: BaseURL is required")
	}
	if opts.APIKey == "" {
		return nil, errors.New("radarr: APIKey is required")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		name:    opts.Name,
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		apiKey:  opts.APIKey,
		poll:    opts.Poll,
		act:     opts.Act,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

// Name returns the configured instance name.
func (c *Client) Name() string { return c.name }

// Type identifies this client as a Radarr instance.
func (c *Client) Type() triagearr.ArrType { return triagearr.ArrTypeRadarr }

// Poll reports whether this instance is configured for read access.
func (c *Client) Poll() bool { return c.poll }

// Act reports whether this instance is allowed to perform deletions.
func (c *Client) Act() bool { return c.act }

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("radarr: building request %s: %w", path, err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("radarr: GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("radarr: GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("radarr: decoding %s: %w", path, err)
	}
	return nil
}

// HealthCheck pings GET /api/v3/health.
func (c *Client) HealthCheck(ctx context.Context) error {
	var ignored []any
	return c.get(ctx, "/api/v3/health", &ignored)
}

type movieEntry struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Path       string `json:"path"`
	SizeOnDisk int64  `json:"sizeOnDisk"`
	Tags       []int  `json:"tags"`
}

type tagEntry struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// ListMedia returns the configured movies. Tags are resolved to label strings.
func (c *Client) ListMedia(ctx context.Context) ([]triagearr.MediaItem, error) {
	var movies []movieEntry
	if err := c.get(ctx, "/api/v3/movie", &movies); err != nil {
		return nil, err
	}
	tags, err := c.fetchTags(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]triagearr.MediaItem, len(movies))
	for i, m := range movies {
		labels := make([]string, 0, len(m.Tags))
		for _, id := range m.Tags {
			if label, ok := tags[id]; ok {
				labels = append(labels, label)
			}
		}
		out[i] = triagearr.MediaItem{
			ID:       triagearr.MediaID(m.ID),
			ArrName:  c.name,
			ArrType:  triagearr.ArrTypeRadarr,
			Title:    m.Title,
			Path:     m.Path,
			Size:     m.SizeOnDisk,
			Tags:     labels,
			LastSeen: now,
		}
	}
	return out, nil
}

func (c *Client) fetchTags(ctx context.Context) (map[int]string, error) {
	var raw []tagEntry
	if err := c.get(ctx, "/api/v3/tag", &raw); err != nil {
		return nil, err
	}
	out := make(map[int]string, len(raw))
	for _, t := range raw {
		out[t.ID] = t.Label
	}
	return out, nil
}

// movieFile mirrors Radarr's /api/v3/moviefile entry. Only the fields used
// by the mapper (M2) and the actor (M5) are captured.
type movieFile struct {
	ID      int64  `json:"id"`
	MovieID int64  `json:"movieId"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

// ListMediaFiles returns the movie file(s) attached to a movie. Radarr
// typically has exactly one file per movie, but the API returns an array;
// we propagate every row to keep the per-file shape consistent with Sonarr.
func (c *Client) ListMediaFiles(ctx context.Context, movieID triagearr.MediaID) ([]triagearr.MediaFile, error) {
	var raw []movieFile
	if err := c.get(ctx, fmt.Sprintf("/api/v3/moviefile?movieId=%d", int64(movieID)), &raw); err != nil {
		return nil, err
	}
	out := make([]triagearr.MediaFile, len(raw))
	for i, m := range raw {
		out[i] = triagearr.MediaFile{
			ArrName: c.name,
			ArrType: triagearr.ArrTypeRadarr,
			FileID:  m.ID,
			MediaID: triagearr.MediaID(m.MovieID),
			Path:    m.Path,
			Size:    m.Size,
		}
	}
	return out, nil
}

// ListImports paginates Radarr's history endpoint for `downloadFolderImported`
// events (eventType=3) — see Sonarr.ListImports for the shared semantics.
func (c *Client) ListImports(ctx context.Context, sinceHistoryID int64) ([]triagearr.ImportRecord, error) {
	return arrhistory.Fetch(ctx, c.get, sinceHistoryID)
}

// DeleteMedia is not wired in M1 — destructive ops live in the M5 Actor milestone.
func (c *Client) DeleteMedia(_ context.Context, _ triagearr.MediaID, _ triagearr.DeleteOpts) error {
	return errors.New("radarr: DeleteMedia not implemented in M1")
}

var (
	_ triagearr.ArrInstance  = (*Client)(nil)
	_ triagearr.FileLister   = (*Client)(nil)
	_ triagearr.ImportLister = (*Client)(nil)
)
