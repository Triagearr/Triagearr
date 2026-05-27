package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// seedTorrentAndRun inserts a torrent with a known name, one run with one
// candidate pointing at that torrent, and one action for that candidate.
// Returns (runID, actionID).
func seedTorrentAndRun(t *testing.T, s *store.Store) (int64, int64) {
	t.Helper()
	ctx := context.Background()

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "aabbccddeeff0011", Name: "My Great Show S01E01", Category: "tv",
		SavePath: "/data/tv", Size: 1 << 30, AddedOn: time.Now().UTC(),
	}))

	runID, err := s.InsertRun(ctx, triagearr.Run{
		TriggeredBy: triagearr.RunTriggerHTTP, TriggeredAt: time.Now().UTC(),
		Mode: "dry-run", StopReason: triagearr.StopNoMoreCandidates, Status: "completed",
	})
	require.NoError(t, err)

	require.NoError(t, s.InsertRunItems(ctx, runID, []triagearr.RunItem{{
		RunID:       runID,
		Rank:        1,
		TorrentHash: "aabbccddeeff0011",
		Score:       42.0,
		SizeBytes:   1 << 30,
	}}))

	actionID, err := s.InsertAction(ctx, triagearr.Action{
		RunID:       runID,
		Rank:        1,
		TorrentHash: "aabbccddeeff0011",
		StartedAt:   time.Now().UTC(),
		Status:      triagearr.ActionSucceeded,
	})
	require.NoError(t, err)

	return runID, actionID
}

func TestGetRun_CandidatesCarryTorrentName(t *testing.T) {
	_, s, h := buildSrv(t, "")
	runID, _ := seedTorrentAndRun(t, s)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/runs/"+strconv.FormatInt(runID, 10), ""))
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Candidates []struct {
			TorrentHash string `json:"torrent_hash"`
			TorrentName string `json:"torrent_name"`
		} `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Candidates, 1)
	require.Equal(t, "aabbccddeeff0011", body.Candidates[0].TorrentHash)
	require.Equal(t, "My Great Show S01E01", body.Candidates[0].TorrentName)
}

func TestPostRun_CandidatesCarryTorrentName(t *testing.T) {
	_, s, h := buildSrvWithDaemonLive(t, "", false)
	ctx := context.Background()
	// Seed a scorable torrent so it appears in the plan.
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "scored001", Name: "Scored Movie (2024)", Category: "movies",
		SavePath: "/data/movies", Size: 10 << 30, AddedOn: time.Now().UTC(),
	}))
	require.NoError(t, s.UpsertScore(ctx, store.ScoreRow{
		Hash: "scored001", Score: 200, AnyTrackerAlive: false,
		FactorsJSON: "[]", ComputedAt: time.Now().UTC(),
	}))
	// Trigger a dry run to generate candidates.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodPost, "/api/v1/runs", `{}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Candidates []struct {
			TorrentHash string `json:"torrent_hash"`
			TorrentName string `json:"torrent_name"`
		} `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	// Only assert that any candidate carrying our hash also carries the name.
	for _, c := range body.Candidates {
		if c.TorrentHash == "scored001" {
			require.Equal(t, "Scored Movie (2024)", c.TorrentName)
		}
	}
}

func TestGetRunActions_CarryTorrentName(t *testing.T) {
	_, s, h := buildSrv(t, "")
	runID, _ := seedTorrentAndRun(t, s)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/runs/"+strconv.FormatInt(runID, 10)+"/actions", ""))
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Actions []struct {
			TorrentHash string `json:"torrent_hash"`
			TorrentName string `json:"torrent_name"`
		} `json:"actions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Actions, 1)
	require.Equal(t, "aabbccddeeff0011", body.Actions[0].TorrentHash)
	require.Equal(t, "My Great Show S01E01", body.Actions[0].TorrentName)
}

func TestListActions_CarryTorrentName(t *testing.T) {
	_, s, h := buildSrv(t, "")
	seedTorrentAndRun(t, s)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/actions", ""))
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Actions []struct {
			TorrentHash string `json:"torrent_hash"`
			TorrentName string `json:"torrent_name"`
		} `json:"actions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Actions, 1)
	require.Equal(t, "My Great Show S01E01", body.Actions[0].TorrentName)
}

func TestGetAction_CarryTorrentName(t *testing.T) {
	_, s, h := buildSrv(t, "")
	_, actionID := seedTorrentAndRun(t, s)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/actions/"+strconv.FormatInt(actionID, 10), ""))
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Action struct {
			TorrentHash string `json:"torrent_hash"`
			TorrentName string `json:"torrent_name"`
		} `json:"action"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "aabbccddeeff0011", body.Action.TorrentHash)
	require.Equal(t, "My Great Show S01E01", body.Action.TorrentName)
}

func TestGetRun_MissingTorrentOmitsTorrentName(t *testing.T) {
	_, s, h := buildSrv(t, "")
	ctx := context.Background()

	// Run with a hash that is NOT in the torrents table.
	runID, err := s.InsertRun(ctx, triagearr.Run{
		TriggeredBy: triagearr.RunTriggerHTTP, TriggeredAt: time.Now().UTC(),
		Mode: "dry-run", StopReason: triagearr.StopNoMoreCandidates, Status: "completed",
	})
	require.NoError(t, err)
	require.NoError(t, s.InsertRunItems(ctx, runID, []triagearr.RunItem{{
		RunID: runID, Rank: 1, TorrentHash: "unknown000000000",
		Score: 1, SizeBytes: 1 << 20,
	}}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/runs/"+strconv.FormatInt(runID, 10), ""))
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Candidates []map[string]any `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Candidates, 1)
	_, hasName := body.Candidates[0]["torrent_name"]
	require.False(t, hasName, "torrent_name should be absent when torrent is not in the DB")
}
