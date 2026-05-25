// Package sonarr is a minimal Sonarr v3 API client used by the observation poller.
//
// M1 scope: HealthCheck + ListMedia. DeleteMedia is declared by the interface
// but returns "not implemented" — destructive ops land in M5.
package sonarr

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

	"github.com/Triagearr/Triagearr/internal/clients/arrhistory"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Client speaks Sonarr's v3 REST API.
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

// New constructs a Sonarr client.
func New(opts Options) (*Client, error) {
	if opts.Name == "" {
		return nil, errors.New("sonarr: Name is required")
	}
	if opts.BaseURL == "" {
		return nil, errors.New("sonarr: BaseURL is required")
	}
	if opts.APIKey == "" {
		return nil, errors.New("sonarr: APIKey is required")
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

// Type identifies this client as a Sonarr instance.
func (c *Client) Type() triagearr.ArrType { return triagearr.ArrTypeSonarr }

// Poll reports whether this instance is configured for read access.
func (c *Client) Poll() bool { return c.poll }

// Act reports whether this instance is allowed to perform deletions.
func (c *Client) Act() bool { return c.act }

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("sonarr: building request %s: %w", path, err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sonarr: GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sonarr: GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("sonarr: decoding %s: %w", path, err)
	}
	return nil
}

// HealthCheck pings GET /api/v3/health. The endpoint returns 200 with a (possibly
// empty) array of health warnings — we don't surface those in M1.
func (c *Client) HealthCheck(ctx context.Context) error {
	var ignored []any
	return c.get(ctx, "/api/v3/health", &ignored)
}

type seriesEntry struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Path       string `json:"path"`
	Tags       []int  `json:"tags"`
	Statistics struct {
		SizeOnDisk int64 `json:"sizeOnDisk"`
	} `json:"statistics"`
}

type tagEntry struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// ListMedia returns the configured series. Tags are resolved to label strings
// so downstream consumers don't need to know about Sonarr's numeric tag ids.
func (c *Client) ListMedia(ctx context.Context) ([]triagearr.MediaItem, error) {
	var series []seriesEntry
	if err := c.get(ctx, "/api/v3/series", &series); err != nil {
		return nil, err
	}
	tags, err := c.fetchTags(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]triagearr.MediaItem, len(series))
	for i, s := range series {
		labels := make([]string, 0, len(s.Tags))
		for _, id := range s.Tags {
			if label, ok := tags[id]; ok {
				labels = append(labels, label)
			}
		}
		out[i] = triagearr.MediaItem{
			ID:       triagearr.MediaID(s.ID),
			ArrType:  triagearr.ArrTypeSonarr,
			Title:    s.Title,
			Path:     s.Path,
			Size:     s.Statistics.SizeOnDisk,
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

// episodeFile mirrors Sonarr's /api/v3/episodefile entry. Only the fields used
// by the linker and the actor (M5) are captured.
type episodeFile struct {
	ID       int64  `json:"id"`
	SeriesID int64  `json:"seriesId"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
}

// ListMediaFiles returns the episode files attached to a series.
// Implements triagearr.FileLister, type-asserted by the arr poller for fan-out.
func (c *Client) ListMediaFiles(ctx context.Context, seriesID triagearr.MediaID) ([]triagearr.MediaFile, error) {
	var raw []episodeFile
	if err := c.get(ctx, fmt.Sprintf("/api/v3/episodefile?seriesId=%d", int64(seriesID)), &raw); err != nil {
		return nil, err
	}
	out := make([]triagearr.MediaFile, len(raw))
	for i, e := range raw {
		out[i] = triagearr.MediaFile{
			ArrType: triagearr.ArrTypeSonarr,
			FileID:  e.ID,
			MediaID: triagearr.MediaID(e.SeriesID),
			Path:    e.Path,
			Size:    e.Size,
		}
	}
	return out, nil
}

// ListImports paginates Sonarr's history endpoint for `downloadFolderImported`
// events (eventType=3) and returns records strictly newer than sinceHistoryID.
// Sonarr returns history.id monotonically increasing; we stop paginating as
// soon as we cross the cursor, keeping the delta-sync cost proportional to
// the number of new imports.
func (c *Client) ListImports(ctx context.Context, sinceHistoryID int64) ([]triagearr.ImportRecord, error) {
	return arrhistory.Fetch(ctx, c.get, sinceHistoryID)
}

// DeleteMediaFile removes one episode file from Sonarr's library. With
// opts.DeleteFiles=true Sonarr unlinks the on-disk file (drops the *arr-side
// hardlink reference); with opts.AddImportExclusion=true the release is added
// to the import exclusion list so *arr won't re-grab the same release.
//
// 5xx and transport failures are wrapped with triagearr.ErrTransient so the
// Actor's retry loop can distinguish them from hard failures (404, 401).
func (c *Client) DeleteMediaFile(ctx context.Context, fileID int64, opts triagearr.DeleteOpts) error {
	path := fmt.Sprintf("/api/v3/episodefile/%d", fileID)
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
		return fmt.Errorf("sonarr: building DELETE %s: %w", path, err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sonarr: DELETE %s: %w: %w", path, triagearr.ErrTransient, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		return fmt.Errorf("sonarr: DELETE %s: HTTP %d: %s: %w", path, resp.StatusCode, string(body), triagearr.ErrTransient)
	}
	return fmt.Errorf("sonarr: DELETE %s: HTTP %d: %s", path, resp.StatusCode, string(body))
}

var (
	_ triagearr.ArrInstance  = (*Client)(nil)
	_ triagearr.FileLister   = (*Client)(nil)
	_ triagearr.ImportLister = (*Client)(nil)
	_ triagearr.FileDeleter  = (*Client)(nil)
)
