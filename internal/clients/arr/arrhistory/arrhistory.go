// Package arrhistory parses Sonarr/Radarr `/api/v3/history` payloads.
// Sonarr and Radarr share the field shape for `downloadFolderImported` records
// (eventType=3) and only differ in the media-id field (`episodeId` vs
// `movieId`), which we don't need at the linker layer. Keeping the parsing
// here lets each client stay focused on its own resource types.
package arrhistory

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// GetJSON is the minimal HTTP contract the *arr clients already satisfy.
type GetJSON func(ctx context.Context, path string, out any) error

// PageSize bounds the per-request payload. Sonarr/Radarr both cap somewhere
// around 1000 records; 500 keeps payloads under ~1 MB on the worst case and
// well within typical homelab API limits.
const PageSize = 500

type page struct {
	TotalRecords int      `json:"totalRecords"`
	Records      []record `json:"records"`
}

type record struct {
	ID         int64      `json:"id"`
	Date       time.Time  `json:"date"`
	EventType  string     `json:"eventType"`
	DownloadID string     `json:"downloadId"`
	Data       recordData `json:"data"`
}

type recordData struct {
	FileID         string `json:"fileId"`
	DownloadClient string `json:"downloadClient"`
	DroppedPath    string `json:"droppedPath"`
	ImportedPath   string `json:"importedPath"`
}

// Fetch returns every history record strictly newer than sinceHistoryID with
// eventType `downloadFolderImported`, paginated and ordered ascending by id
// (oldest first) so the caller can upsert in stream order.
func Fetch(ctx context.Context, get GetJSON, sinceHistoryID int64) ([]triagearr.ImportRecord, error) {
	var collected []triagearr.ImportRecord
	pageNum := 1
	for {
		var pg page
		path := fmt.Sprintf("/api/v3/history?eventType=3&page=%d&pageSize=%d&sortKey=date&sortDirection=descending",
			pageNum, PageSize)
		if err := get(ctx, path, &pg); err != nil {
			return nil, fmt.Errorf("history page %d: %w", pageNum, err)
		}
		stop := false
		for _, r := range pg.Records {
			if r.ID <= sinceHistoryID {
				stop = true
				break
			}
			rec, ok := convert(r)
			if !ok {
				continue
			}
			collected = append(collected, rec)
		}
		if stop || len(pg.Records) < PageSize {
			break
		}
		pageNum++
	}
	// Reverse to ascending — easier upsert semantics downstream.
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return collected, nil
}

// convert maps a raw history row to the wire-agnostic ImportRecord. Returns
// ok=false for records we cannot use as linker entries (empty downloadId,
// non-qBittorrent client, missing fileId).
func convert(r record) (triagearr.ImportRecord, bool) {
	if r.DownloadID == "" || r.Data.FileID == "" {
		return triagearr.ImportRecord{}, false
	}
	if r.Data.DownloadClient != "" && !strings.EqualFold(r.Data.DownloadClient, "qBittorrent") {
		return triagearr.ImportRecord{}, false
	}
	fid, err := strconv.ParseInt(r.Data.FileID, 10, 64)
	if err != nil {
		return triagearr.ImportRecord{}, false
	}
	return triagearr.ImportRecord{
		HistoryID:    r.ID,
		FileID:       fid,
		DownloadID:   triagearr.Hash(strings.ToLower(r.DownloadID)),
		DroppedPath:  r.Data.DroppedPath,
		ImportedPath: r.Data.ImportedPath,
		ImportedAt:   r.Date.UTC(),
	}, true
}
