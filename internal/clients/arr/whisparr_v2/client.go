// Package whisparr_v2 is a minimal Whisparr v2 API client used by the observation
// poller. Whisparr v2 (the stable branch) is a Sonarr fork: it speaks the same
// /api/v3 surface with series and episode files, so this package mirrors the
// Sonarr client and only swaps the triagearr.ArrType identity.
//
// HTTP plumbing (auth header, GET decoding, DELETE error wrapping) lives in
// internal/clients/arr/arrclient.
package whisparr_v2

import (
	"context"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/clients/arr/arrclient"
	"github.com/Triagearr/Triagearr/internal/clients/arr/arrhistory"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Client speaks Whisparr v2's Sonarr-derived /api/v3 REST API.
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

// New constructs a Whisparr v2 client.
func New(opts Options) (*Client, error) {
	base, err := arrclient.New(arrclient.Options{
		Label: "whisparr_v2", Name: opts.Name, BaseURL: opts.BaseURL, APIKey: opts.APIKey,
		Poll: opts.Poll, Act: opts.Act, Timeout: opts.Timeout,
	})
	if err != nil {
		return nil, err
	}
	return &Client{BaseClient: base}, nil
}

// Type identifies this client as a Whisparr v2 instance.
func (c *Client) Type() triagearr.ArrType { return triagearr.ArrTypeWhisparrV2 }

// HealthCheck pings GET /api/v3/health.
func (c *Client) HealthCheck(ctx context.Context) error {
	var ignored []any
	return c.Get(ctx, "/api/v3/health", &ignored)
}

type seriesEntry struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	TitleSlug  string `json:"titleSlug"`
	Path       string `json:"path"`
	Tags       []int  `json:"tags"`
	Statistics struct {
		SizeOnDisk int64 `json:"sizeOnDisk"`
	} `json:"statistics"`
}

// ListMedia returns the configured series. Tags are resolved to label strings.
func (c *Client) ListMedia(ctx context.Context) ([]triagearr.MediaItem, error) {
	var series []seriesEntry
	if err := c.Get(ctx, "/api/v3/series", &series); err != nil {
		return nil, err
	}
	tags, err := c.FetchTags(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]triagearr.MediaItem, len(series))
	for i, s := range series {
		out[i] = triagearr.MediaItem{
			ID:        triagearr.MediaID(s.ID),
			ArrType:   triagearr.ArrTypeWhisparrV2,
			Title:     s.Title,
			TitleSlug: s.TitleSlug,
			Path:      s.Path,
			Size:      s.Statistics.SizeOnDisk,
			Tags:      arrclient.ResolveTags(s.Tags, tags),
			LastSeen:  now,
		}
	}
	return out, nil
}

// episodeFile mirrors Whisparr v2's /api/v3/episodefile entry.
type episodeFile struct {
	ID       int64  `json:"id"`
	SeriesID int64  `json:"seriesId"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
}

// ListMediaFiles returns the episode files attached to a series.
func (c *Client) ListMediaFiles(ctx context.Context, seriesID triagearr.MediaID) ([]triagearr.MediaFile, error) {
	var raw []episodeFile
	if err := c.Get(ctx, fmt.Sprintf("/api/v3/episodefile?seriesId=%d", int64(seriesID)), &raw); err != nil {
		return nil, err
	}
	out := make([]triagearr.MediaFile, len(raw))
	for i, e := range raw {
		out[i] = triagearr.MediaFile{
			ArrType: triagearr.ArrTypeWhisparrV2,
			FileID:  e.ID,
			MediaID: triagearr.MediaID(e.SeriesID),
			Path:    e.Path,
			Size:    e.Size,
		}
	}
	return out, nil
}

// ListImports paginates Whisparr v2's history endpoint for `downloadFolderImported`
// events (eventType=3) — see Sonarr.ListImports for the shared semantics.
func (c *Client) ListImports(ctx context.Context, sinceHistoryID int64) ([]triagearr.ImportRecord, error) {
	return arrhistory.Fetch(ctx, c.Get, sinceHistoryID)
}

// DeleteMediaFile removes one episode file from Whisparr v2's library. See
// arrclient.BaseClient.DeleteFile for status-code and transient-error semantics.
func (c *Client) DeleteMediaFile(ctx context.Context, fileID int64, opts triagearr.DeleteOpts) error {
	return c.DeleteFile(ctx, "/api/v3/episodefile", fileID, opts)
}

var (
	_ triagearr.ArrInstance  = (*Client)(nil)
	_ triagearr.FileLister   = (*Client)(nil)
	_ triagearr.ImportLister = (*Client)(nil)
	_ triagearr.FileDeleter  = (*Client)(nil)
)
