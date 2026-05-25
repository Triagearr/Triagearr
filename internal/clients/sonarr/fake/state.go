// Package fake is an in-memory Sonarr v3 API server suitable for integration
// tests and dev fixtures. It implements the subset of endpoints consumed by
// [github.com/Triagearr/Triagearr/internal/clients/sonarr] and the shared
// history fetcher in
// [github.com/Triagearr/Triagearr/internal/clients/arrhistory].
//
// All state is in-memory; restarting the process resets it. Concurrent
// callers are serialized via a single RWMutex on State.
package fake

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Series mirrors the wire shape returned by /api/v3/series. Only fields the
// real client decodes are populated; scenarios can pre-fill EpisodeFiles for
// the per-series file endpoint.
type Series struct {
	ID         int64         `json:"id" yaml:"id"`
	Title      string        `json:"title" yaml:"title"`
	TitleSlug  string        `json:"titleSlug" yaml:"titleSlug"`
	Path       string        `json:"path" yaml:"path"`
	Tags       []int         `json:"tags" yaml:"tags"`
	Statistics SeriesStats   `json:"statistics" yaml:"statistics"`
	Files      []EpisodeFile `json:"-" yaml:"files,omitempty"`
}

// SeriesStats is the nested statistics object Sonarr returns. We only consume
// SizeOnDisk today; the rest is present for scenario authoring fidelity.
type SeriesStats struct {
	SizeOnDisk int64 `json:"sizeOnDisk" yaml:"sizeOnDisk"`
}

// EpisodeFile is one entry of /api/v3/episodefile.
type EpisodeFile struct {
	ID       int64  `json:"id" yaml:"id"`
	SeriesID int64  `json:"seriesId" yaml:"seriesId"`
	Path     string `json:"path" yaml:"path"`
	Size     int64  `json:"size" yaml:"size"`
}

// Tag is one /api/v3/tag entry.
type Tag struct {
	ID    int    `json:"id" yaml:"id"`
	Label string `json:"label" yaml:"label"`
}

// HistoryRecord is one /api/v3/history entry. The fake stores wire-level
// shape; consumers only care about eventType "downloadFolderImported"
// (EventType="downloadFolderImported", sometimes also typed as eventType=3
// numerically in the URL filter).
type HistoryRecord struct {
	ID         int64             `json:"id" yaml:"id"`
	Date       time.Time         `json:"date" yaml:"date"`
	EventType  string            `json:"eventType" yaml:"eventType"`
	DownloadID string            `json:"downloadId" yaml:"downloadId"`
	Data       HistoryRecordData `json:"data" yaml:"data"`
}

// HistoryRecordData carries the inner `data` blob Sonarr returns inline.
type HistoryRecordData struct {
	FileID         string `json:"fileId,omitempty" yaml:"fileId,omitempty"`
	DownloadClient string `json:"downloadClient,omitempty" yaml:"downloadClient,omitempty"`
	DroppedPath    string `json:"droppedPath,omitempty" yaml:"droppedPath,omitempty"`
	ImportedPath   string `json:"importedPath,omitempty" yaml:"importedPath,omitempty"`
	Size           string `json:"size,omitempty" yaml:"size,omitempty"`
}

// State is the mutable in-memory store.
type State struct {
	apiKey string

	mu      sync.RWMutex
	series  map[int64]*Series
	tags    map[int]*Tag
	history []HistoryRecord // ordered ascending by ID; handler reverses for the wire

	episodeFileDeletes atomic.Int64
}

// NewState builds an empty State. apiKey is the value clients must send in
// X-Api-Key; pass empty to disable the check.
func NewState(apiKey string) *State {
	return &State{
		apiKey: apiKey,
		series: make(map[int64]*Series),
		tags:   make(map[int]*Tag),
	}
}

// APIKey returns the configured API key (used by the request gate).
func (s *State) APIKey() string { return s.apiKey }

// AddSeries upserts a series, copying nested slices so callers can mutate
// their input safely after the call.
func (s *State) AddSeries(sr Series) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := sr
	cp.Tags = append([]int(nil), sr.Tags...)
	cp.Files = append([]EpisodeFile(nil), sr.Files...)
	s.series[sr.ID] = &cp
}

// AddTag upserts a tag.
func (s *State) AddTag(t Tag) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := t
	s.tags[t.ID] = &cp
}

// AddHistory appends one history record. Caller should pass monotonically
// increasing IDs; the fake does not enforce uniqueness but the wire output
// sorts by ID descending which assumes uniqueness.
func (s *State) AddHistory(h HistoryRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, h)
}

// ListSeries returns a copy of all series (without the nested Files, which
// are served by a separate endpoint).
func (s *State) ListSeries() []Series {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Series, 0, len(s.series))
	for _, sr := range s.series {
		cp := *sr
		cp.Files = nil
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ListTags returns all tags.
func (s *State) ListTags() []Tag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Tag, 0, len(s.tags))
	for _, t := range s.tags {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FilesForSeries returns the episode files attached to a series, or nil if
// the series is unknown.
func (s *State) FilesForSeries(seriesID int64) []EpisodeFile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sr, ok := s.series[seriesID]
	if !ok {
		return nil
	}
	return append([]EpisodeFile(nil), sr.Files...)
}

// DeleteEpisodeFile removes a file from its parent series. Returns true when
// the fileID existed.
func (s *State) DeleteEpisodeFile(fileID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sr := range s.series {
		for i, f := range sr.Files {
			if f.ID == fileID {
				sr.Files = append(sr.Files[:i], sr.Files[i+1:]...)
				sr.Statistics.SizeOnDisk -= f.Size
				if sr.Statistics.SizeOnDisk < 0 {
					sr.Statistics.SizeOnDisk = 0
				}
				s.episodeFileDeletes.Add(1)
				return true
			}
		}
	}
	return false
}

// EpisodeFileDeletes returns the number of successful DELETE /episodefile
// calls — useful for tests that assert ordering or call counts.
func (s *State) EpisodeFileDeletes() int64 {
	return s.episodeFileDeletes.Load()
}

// HistoryPage returns one page of history records sorted descending by ID,
// matching Sonarr's response shape.
func (s *State) HistoryPage(eventType string, pageNum, pageSize int) ([]HistoryRecord, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	filtered := make([]HistoryRecord, 0, len(s.history))
	for _, h := range s.history {
		if eventType == "" || h.EventType == eventType {
			filtered = append(filtered, h)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID > filtered[j].ID })
	total := len(filtered)
	if pageNum <= 0 {
		pageNum = 1
	}
	if pageSize <= 0 {
		pageSize = 500
	}
	start := (pageNum - 1) * pageSize
	if start >= total {
		return nil, total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return filtered[start:end], total
}
