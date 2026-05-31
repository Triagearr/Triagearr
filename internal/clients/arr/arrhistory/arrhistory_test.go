package arrhistory_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/arr/arrhistory"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// stubServer hands out canned history pages; the URL path determines which
// page (and thus which set of records) is returned. Exercises pagination,
// the sinceHistoryID cursor, and the record-filter rules in one harness.
func stubServer(t *testing.T, pages map[int][]map[string]any) (*httptest.Server, arrhistory.GetJSON) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var pageNum int
		fmt.Sscanf(q.Get("page"), "%d", &pageNum)
		if pageNum == 0 {
			pageNum = 1
		}
		recs, ok := pages[pageNum]
		if !ok {
			recs = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"page":         pageNum,
			"pageSize":     arrhistory.PageSize,
			"totalRecords": 999,
			"records":      recs,
		})
	}))
	t.Cleanup(srv.Close)
	get := func(ctx context.Context, path string, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return srv, get
}

func TestFetch_NormalisesAndFilters(t *testing.T) {
	pages := map[int][]map[string]any{
		1: {
			{
				"id": 102, "date": "2026-05-15T12:00:00Z", "eventType": "downloadFolderImported",
				"downloadId": "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
				"data": map[string]any{
					"fileId": "42", "downloadClient": "qBittorrent",
					"droppedPath": "/files/torrents/x.mkv", "importedPath": "/files/media/x.mkv",
				},
			},
			{ // empty downloadId — must be dropped
				"id": 101, "date": "2026-05-15T11:00:00Z", "eventType": "downloadFolderImported",
				"downloadId": "",
				"data": map[string]any{
					"fileId": "41", "downloadClient": "qBittorrent",
					"droppedPath": "", "importedPath": "/files/media/y.mkv",
				},
			},
			{ // non-qBit client — must be dropped
				"id": 100, "date": "2026-05-15T10:00:00Z", "eventType": "downloadFolderImported",
				"downloadId": "1234567890ABCDEF1234567890ABCDEF12345678",
				"data": map[string]any{
					"fileId": "40", "downloadClient": "Deluge",
					"droppedPath": "", "importedPath": "",
				},
			},
		},
	}
	_, get := stubServer(t, pages)

	recs, err := arrhistory.Fetch(context.Background(), get, 0)
	require.NoError(t, err)
	require.Len(t, recs, 1, "two records should be filtered out (empty downloadId + non-qBit)")
	require.Equal(t, int64(102), recs[0].HistoryID)
	require.Equal(t, int64(42), recs[0].FileID)
	require.Equal(t, triagearr.Hash("abcdef0123456789abcdef0123456789abcdef01"), recs[0].DownloadID,
		"downloadId must be lowercased on the way in")
}

func TestFetch_StopsAtSinceCursor(t *testing.T) {
	mk := func(id int) map[string]any {
		return map[string]any{
			"id": id, "date": "2026-05-15T12:00:00Z", "eventType": "downloadFolderImported",
			"downloadId": strings.Repeat("a", 40),
			"data": map[string]any{
				"fileId": fmt.Sprintf("%d", id*10), "downloadClient": "qBittorrent",
				"droppedPath": "", "importedPath": "",
			},
		}
	}
	// Records descending by id; cursor=200 should stop after id 201.
	pages := map[int][]map[string]any{1: {mk(203), mk(202), mk(201), mk(200), mk(199)}}
	_, get := stubServer(t, pages)
	recs, err := arrhistory.Fetch(context.Background(), get, 200)
	require.NoError(t, err)
	require.Len(t, recs, 3)
	require.Equal(t, int64(201), recs[0].HistoryID, "result must be ascending; oldest first")
	require.Equal(t, int64(203), recs[2].HistoryID)
}

func TestFetch_Paginates(t *testing.T) {
	mk := func(id int) map[string]any {
		return map[string]any{
			"id": id, "date": "2026-05-15T12:00:00Z", "eventType": "downloadFolderImported",
			"downloadId": strings.Repeat("b", 40),
			"data": map[string]any{
				"fileId": fmt.Sprintf("%d", id), "downloadClient": "qBittorrent",
				"droppedPath": "", "importedPath": "",
			},
		}
	}
	// Build a full page (500 records, ids 999..500) then a short follow-up.
	full := make([]map[string]any, arrhistory.PageSize)
	for i := 0; i < arrhistory.PageSize; i++ {
		full[i] = mk(999 - i)
	}
	short := []map[string]any{mk(499), mk(498)}
	pages := map[int][]map[string]any{1: full, 2: short}

	_, get := stubServer(t, pages)
	recs, err := arrhistory.Fetch(context.Background(), get, 0)
	require.NoError(t, err)
	require.Len(t, recs, arrhistory.PageSize+2)
}
