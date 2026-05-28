// Package radarr is a minimal Radarr v3 API client used by the observation poller.
//
// HTTP plumbing (auth header, GET decoding, DELETE error wrapping) lives in
// internal/clients/arrclient. This package keeps only what is Radarr-specific:
// the movie/moviefile JSON shapes, the path conventions, and the
// triagearr.ArrType identity.
package radarr

import (
	"context"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/clients/arr/arrclient"
	"github.com/Triagearr/Triagearr/internal/clients/arr/arrhistory"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Client speaks Radarr's v3 REST API.
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

// New constructs a Radarr client.
func New(opts Options) (*Client, error) {
	base, err := arrclient.New(arrclient.Options{
		Label: "radarr", Name: opts.Name, BaseURL: opts.BaseURL, APIKey: opts.APIKey,
		Poll: opts.Poll, Act: opts.Act, Timeout: opts.Timeout,
	})
	if err != nil {
		return nil, err
	}
	return &Client{BaseClient: base}, nil
}

// Type identifies this client as a Radarr instance.
func (c *Client) Type() triagearr.ArrType { return triagearr.ArrTypeRadarr }

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
			ArrType:   triagearr.ArrTypeRadarr,
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

// movieFile mirrors Radarr's /api/v3/moviefile entry. Only the fields used
// by the linker and the actor (M5) are captured.
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
	if err := c.Get(ctx, fmt.Sprintf("/api/v3/moviefile?movieId=%d", int64(movieID)), &raw); err != nil {
		return nil, err
	}
	out := make([]triagearr.MediaFile, len(raw))
	for i, m := range raw {
		out[i] = triagearr.MediaFile{
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
	return arrhistory.Fetch(ctx, c.Get, sinceHistoryID)
}

// DeleteMediaFile removes one movie file from Radarr's library. See
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
