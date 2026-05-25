// Package qbit is a thin client for the qBittorrent WebUI API v2.
//
// We rely on cookie-based auth (POST /api/v2/auth/login) and a small subset of
// endpoints sufficient for M1 observation. The DeleteOpts-bearing Delete method
// is declared on the interface but explicitly not wired in M1 — it lives in M5.
package qbit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Client is a qBittorrent WebUI v2 client.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client

	mu       sync.Mutex
	loggedIn bool
}

// Options configures the client. Username/Password are optional (some setups
// bind qBit's WebUI to a private network and disable auth).
type Options struct {
	BaseURL  string
	Username string
	Password string
	Timeout  time.Duration
}

// New constructs a Client. The HTTP cookie jar carries the session cookie
// returned by /api/v2/auth/login across calls.
func New(opts Options) (*Client, error) {
	if opts.BaseURL == "" {
		return nil, errors.New("qbit: BaseURL is required")
	}
	if _, err := url.Parse(opts.BaseURL); err != nil {
		return nil, fmt.Errorf("qbit: parsing BaseURL: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("qbit: cookiejar: %w", err)
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL:  strings.TrimRight(opts.BaseURL, "/"),
		username: opts.Username,
		password: opts.Password,
		http:     &http.Client{Jar: jar, Timeout: timeout},
	}, nil
}

// ensureLogin performs /api/v2/auth/login on the first call (or after a session
// is invalidated). When username is empty, we assume auth-bypass and skip.
func (c *Client) ensureLogin(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loggedIn || c.username == "" {
		c.loggedIn = true
		return nil
	}
	form := url.Values{"username": {c.username}, "password": {c.password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("qbit: building login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.baseURL)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("qbit: login: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qbit: login: HTTP %d: %s", resp.StatusCode, string(body))
	}
	if strings.TrimSpace(string(body)) != "Ok." {
		return fmt.Errorf("qbit: login: unexpected response %q", string(body))
	}
	c.loggedIn = true
	return nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	if err := c.ensureLogin(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("qbit: building request %s: %w", path, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("qbit: GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusForbidden {
		// Session expired — force re-login next call.
		c.mu.Lock()
		c.loggedIn = false
		c.mu.Unlock()
		return fmt.Errorf("qbit: GET %s: HTTP 403 (session expired)", path)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qbit: GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("qbit: decoding %s: %w", path, err)
	}
	return nil
}

// torrentInfo mirrors the fields we consume from /api/v2/torrents/info.
// See https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API
type torrentInfo struct {
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	Category      string  `json:"category"`
	SavePath      string  `json:"save_path"`
	Size          int64   `json:"size"`
	AddedOn       int64   `json:"added_on"`
	CompletionOn  int64   `json:"completion_on"`
	Ratio         float64 `json:"ratio"`
	Uploaded      int64   `json:"uploaded"`
	NumSeeds      int     `json:"num_seeds"`
	NumComplete   int     `json:"num_complete"`
	NumLeechs     int     `json:"num_leechs"`
	NumIncomplete int     `json:"num_incomplete"`
	State         string  `json:"state"`
	LastActivity  int64   `json:"last_activity"`
	Private       bool    `json:"private"`
	Tags          string  `json:"tags"`
}

// ListTorrents returns the current set of torrents tracked by qBit.
func (c *Client) ListTorrents(ctx context.Context) ([]triagearr.Torrent, error) {
	var raw []torrentInfo
	if err := c.getJSON(ctx, "/api/v2/torrents/info", &raw); err != nil {
		return nil, err
	}
	out := make([]triagearr.Torrent, len(raw))
	for i, t := range raw {
		// qBit reports "num_seeds" as the count of *connected* seeds. The full
		// swarm size (which is what the scorer cares about) is "num_complete".
		seeders := t.NumComplete
		if seeders == 0 {
			seeders = t.NumSeeds
		}
		leechers := t.NumIncomplete
		if leechers == 0 {
			leechers = t.NumLeechs
		}
		var completion time.Time
		if t.CompletionOn > 0 {
			completion = time.Unix(t.CompletionOn, 0).UTC()
		}
		out[i] = triagearr.Torrent{
			Hash:         triagearr.Hash(t.Hash),
			Name:         t.Name,
			Category:     t.Category,
			SavePath:     t.SavePath,
			Size:         t.Size,
			AddedOn:      time.Unix(t.AddedOn, 0).UTC(),
			CompletionOn: completion,
			Ratio:        t.Ratio,
			Uploaded:     t.Uploaded,
			Seeders:      seeders,
			Leechers:     leechers,
			State:        triagearr.TorrentState(t.State),
			LastActivity: time.Unix(t.LastActivity, 0).UTC(),
			Private:      t.Private,
			Tags:         t.Tags,
		}
	}
	return out, nil
}

type torrentFile struct {
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
}

// TorrentFiles returns the list of files inside the torrent identified by hash.
func (c *Client) TorrentFiles(ctx context.Context, h triagearr.Hash) ([]triagearr.TorrentFile, error) {
	var raw []torrentFile
	if err := c.getJSON(ctx, "/api/v2/torrents/files?hash="+url.QueryEscape(string(h)), &raw); err != nil {
		return nil, err
	}
	out := make([]triagearr.TorrentFile, len(raw))
	for i, f := range raw {
		out[i] = triagearr.TorrentFile{Name: f.Name, Size: f.Size, Progress: f.Progress}
	}
	return out, nil
}

// trackerInfo mirrors /api/v2/torrents/trackers entries. The `status` field
// is qBit's enum (0=disabled, 1=not_contacted, 2=working, 3=updating, 4=not_working).
// qBit prepends three synthetic "trackers" for DHT / PEX / LSD with empty URLs;
// we drop those — they aren't real announce endpoints.
type trackerInfo struct {
	URL    string `json:"url"`
	Status int    `json:"status"`
	Msg    string `json:"msg"`
}

// ListTrackers returns the trackers attached to a torrent, excluding the
// synthetic DHT/PEX/LSD pseudo-trackers (URL `**` etc.).
func (c *Client) ListTrackers(ctx context.Context, h triagearr.Hash) ([]triagearr.TrackerInfo, error) {
	var raw []trackerInfo
	if err := c.getJSON(ctx, "/api/v2/torrents/trackers?hash="+url.QueryEscape(string(h)), &raw); err != nil {
		return nil, err
	}
	out := make([]triagearr.TrackerInfo, 0, len(raw))
	for _, t := range raw {
		if !looksLikeURL(t.URL) {
			continue
		}
		host := parseTrackerHost(t.URL)
		out = append(out, triagearr.TrackerInfo{
			URL:    t.URL,
			Host:   host,
			Status: triagearr.TrackerStatus(t.Status),
			Msg:    t.Msg,
		})
	}
	return out, nil
}

// looksLikeURL rejects qBit's synthetic ** ** ** DHT/PEX/LSD entries.
func looksLikeURL(s string) bool {
	if s == "" {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Host != ""
}

// parseTrackerHost returns the lowercased host (no port) of a tracker URL.
// Inputs are pre-filtered by looksLikeURL, so url.Parse always succeeds here.
func parseTrackerHost(raw string) string {
	u, _ := url.Parse(raw)
	return strings.ToLower(u.Hostname())
}

// Delete removes a torrent from qBit. When opts.DeleteFiles is true qBit
// unlinks the on-disk files (last hardlink reference on a TRaSH-guides
// hardlink layout); the *arr-side delete must already have run — see
// HARDLINK_TOPOLOGY.md and ADR-0003 for why the order matters.
//
// 5xx and transport failures are wrapped with triagearr.ErrTransient so the
// Actor's retry loop can pick them up.
func (c *Client) Delete(ctx context.Context, h triagearr.Hash, opts triagearr.DeleteOpts) error {
	if err := c.ensureLogin(ctx); err != nil {
		return err
	}
	form := url.Values{"hashes": {string(h)}, "deleteFiles": {"false"}}
	if opts.DeleteFiles {
		form.Set("deleteFiles", "true")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v2/torrents/delete", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("qbit: building DELETE request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("qbit: POST /torrents/delete: %w: %w", triagearr.ErrTransient, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusForbidden {
		c.mu.Lock()
		c.loggedIn = false
		c.mu.Unlock()
		return fmt.Errorf("qbit: POST /torrents/delete: HTTP 403 (session expired): %w", triagearr.ErrTransient)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("qbit: POST /torrents/delete: HTTP %d: %s: %w", resp.StatusCode, string(body), triagearr.ErrTransient)
	}
	return fmt.Errorf("qbit: POST /torrents/delete: HTTP %d: %s", resp.StatusCode, string(body))
}

var _ triagearr.TorrentClient = (*Client)(nil)
