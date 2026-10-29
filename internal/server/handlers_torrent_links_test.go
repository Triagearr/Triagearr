package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/linker"
	"github.com/Triagearr/Triagearr/internal/server"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// External consumers (a homelab dashboard) act on the *arr media behind a
// torrent; the slug alone forces them to re-list the whole library to find it.
func TestGetTorrent_LinksCarryMediaID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "aabbccddeeff0011", Name: "Some.Movie.2020.1080p", Category: "radarr",
		SavePath: "/data/movies", Size: 1 << 30, AddedOn: now,
	}))
	require.NoError(t, s.UpsertArrImport(ctx, triagearr.ArrTypeRadarr, triagearr.ImportRecord{
		HistoryID: 1, FileID: 10, DownloadID: "aabbccddeeff0011",
		ImportedPath: "/files/media/Some Movie (2020).mkv", ImportedAt: now,
	}))
	require.NoError(t, s.UpsertMediaFile(ctx, triagearr.MediaFile{
		ArrType: triagearr.ArrTypeRadarr,
		FileID:  10, MediaID: 42, Path: "/files/media/Some Movie (2020).mkv", Size: 1 << 30,
	}))

	vol := decider.Volume{Name: "data", Path: "/data", TargetFreePercent: 20}
	srv := server.New(server.Options{
		Bind:   "127.0.0.1:0",
		APIKey: testAPIKey,
		Store:  s,
		Linker: linker.New(s),
	}, &server.Engine{
		Decider: decider.New(s),
		Volume:  func() decider.Volume { return vol },
	})

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/torrents/aabbccddeeff0011", ""))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Links []struct {
			ArrType string `json:"arr_type"`
			FileID  int64  `json:"file_id"`
			MediaID int64  `json:"media_id"`
		} `json:"links"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Links, 1)
	require.Equal(t, "radarr", body.Links[0].ArrType)
	require.Equal(t, int64(10), body.Links[0].FileID)
	require.Equal(t, int64(42), body.Links[0].MediaID)
}
