// Package whisparr_v3 is a minimal Whisparr v3 API client used by the observation
// poller. Whisparr v3 (the eros branch) is a Radarr fork: it speaks the same
// /api/v3 surface with movies and movie files, so this package mirrors the
// Radarr client and only swaps the triagearr.ArrType identity.
//
// HTTP plumbing (auth header, GET decoding, DELETE error wrapping) lives in
// internal/clients/arr/arrclient.
package whisparr_v3

import (
	"context"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/clients/arr/arrclient"
	"github.com/Triagearr/Triagearr/internal/clients/arr/arrhistory"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Client speaks Whisparr v3's Radarr-derived /api/v3 REST API.
type Client struct {
	*arrclient.BaseClient
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

// New constructs a Whisparr v3 client.
func New(opts Options) (*Client, error) {
	base, err := arrclient.New(arrclient.Options{
		Label: "whisparr_v3", Name: opts.Name, BaseURL: opts.BaseURL, APIKey: opts.APIKey,
		Poll: opts.Poll, Act: opts.Act, Timeout: opts.Timeout,
	})
	if err != nil {
		return nil, err
	}
	return &Client{BaseClient: base}, nil
}

// Type identifies this client as a Whisparr v3 instance.
func (c *Client) Type() triagearr.ArrType { return triagearr.ArrTypeWhisparrV3 }

// HealthCheck pings GET /api/v3/health.
func (c *Client) HealthCheck(ctx context.Context) error {
	var ignored []any
	return c.Get(ctx, "/api/v3/health", &ignored)
}

type movieEntry struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	TitleSlug  string `json:"titleSlug"`
	Path       string `json:"path"`
	SizeOnDisk int64  `json:"sizeOnDisk"`
	Tags       []int  `json:"tags"`
}

// ListMedia returns the configured movies. Tags are resolved to label strings.
func (c *Client) ListMedia(ctx context.Context) ([]triagearr.MediaItem, error) {
	var movies []movieEntry
	if err := c.Get(ctx, "/api/v3/movie", &movies); err != nil {
		return nil, err
	}
	tags, err := c.FetchTags(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]triagearr.MediaItem, len(movies))
	for i, m := range movies {
		out[i] = triagearr.MediaItem{
			ID:        triagearr.MediaID(m.ID),
			ArrType:   triagearr.ArrTypeWhisparrV3,
			Title:     m.Title,
			TitleSlug: m.TitleSlug,
			Path:      m.Path,
			Size:      m.SizeOnDisk,
			Tags:      arrclient.ResolveTags(m.Tags, tags),
			LastSeen:  now,
		}
	}
	return out, nil
}

// movieFile mirrors Whisparr v3's /api/v3/moviefile entry.
type movieFile struct {
	ID      int64  `json:"id"`
	MovieID int64  `json:"movieId"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

// ListMediaFiles returns the movie file(s) attached to a movie. Whisparr, like
// Radarr, typically has one file per movie but the API returns an array; we
// propagate every row to keep the per-file shape consistent.
func (c *Client) ListMediaFiles(ctx context.Context, movieID triagearr.MediaID) ([]triagearr.MediaFile, error) {
	var raw []movieFile
	if err := c.Get(ctx, fmt.Sprintf("/api/v3/moviefile?movieId=%d", int64(movieID)), &raw); err != nil {
		return nil, err
	}
	out := make([]triagearr.MediaFile, len(raw))
	for i, m := range raw {
		out[i] = triagearr.MediaFile{
			ArrType: triagearr.ArrTypeWhisparrV3,
			FileID:  m.ID,
			MediaID: triagearr.MediaID(m.MovieID),
			Path:    m.Path,
			Size:    m.Size,
		}
	}
	return out, nil
}

// ListImports paginates Whisparr v3's history endpoint for `downloadFolderImported`
// events (eventType=3) — see Sonarr.ListImports for the shared semantics.
func (c *Client) ListImports(ctx context.Context, sinceHistoryID int64) ([]triagearr.ImportRecord, error) {
	return arrhistory.Fetch(ctx, c.Get, sinceHistoryID)
}

// DeleteMediaFile removes one movie file from Whisparr v3's library. See
// arrclient.BaseClient.DeleteFile for status-code and transient-error semantics.
func (c *Client) DeleteMediaFile(ctx context.Context, fileID int64, opts triagearr.DeleteOpts) error {
	return c.DeleteFile(ctx, "/api/v3/moviefile", fileID, opts)
}

var (
	_ triagearr.ArrInstance  = (*Client)(nil)
	_ triagearr.FileLister   = (*Client)(nil)
	_ triagearr.ImportLister = (*Client)(nil)
	_ triagearr.FileDeleter  = (*Client)(nil)
)
