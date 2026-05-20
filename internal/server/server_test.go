package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/server"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "srv.db")
	s, err := store.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())
	return s
}

func seed(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "h1", Name: "n1", Category: "tv", SavePath: "/data/dl",
		Size: 3 * 1024 * 1024 * 1024, AddedOn: time.Now().UTC(),
	}))
	require.NoError(t, s.UpsertScore(ctx, store.ScoreRow{
		Hash: "h1", Score: 99, AnyTrackerAlive: true, FactorsJSON: "[]", ComputedAt: time.Now().UTC(),
	}))
	require.NoError(t, s.InsertDiskUsage(ctx, triagearr.DiskUsage{
		VolumeName: "data", Path: "/data", Timestamp: time.Now().UTC(),
		TotalBytes: 100 * 1024 * 1024 * 1024, FreePercent: 5,
	}))
}

func buildSrv(t *testing.T, apiKey string) (*server.Server, *store.Store, http.Handler) {
	t.Helper()
	s := testStore(t)
	seed(t, s)
	vols := []decider.Volume{{
		Name: "data", Path: "/data", TargetFreePercent: 20, MaxRunSizeGB: 100,
	}}
	srv := server.New(server.Options{
		Bind:    "127.0.0.1:0",
		APIKey:  apiKey,
		Store:   s,
		Decider: decider.New(s),
		Volume: func(name string) (decider.Volume, bool) {
			for _, v := range vols {
				if v.Name == name {
					return v, true
				}
			}
			return decider.Volume{}, false
		},
		Volumes: func() []decider.Volume { return vols },
	})
	return srv, s, srv.Handler()
}

func TestPostRun_AuthMissing(t *testing.T) {
	_, _, h := buildSrv(t, "sekrit")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(`{"volume":"data"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPostRun_AuthOK_InsertsRun(t *testing.T) {
	_, s, h := buildSrv(t, "sekrit")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(`{"volume":"data"}`))
	req.Header.Set("X-API-Key", "sekrit")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "dry-run", body["mode"])
	require.Equal(t, "data", body["volume"])

	runs, err := s.ListRuns(context.Background(), store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 1)
}

func TestPostRun_UnknownVolume(t *testing.T) {
	_, _, h := buildSrv(t, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(`{"volume":"nope"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetRun_Found(t *testing.T) {
	_, s, h := buildSrv(t, "")
	id, err := s.InsertRun(context.Background(), triagearr.Run{
		TriggeredBy: triagearr.RunTriggerHTTP, TriggeredAt: time.Now().UTC(),
		Mode: "dry-run", StopReason: triagearr.StopNoMoreCandidates, Status: "completed",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+strconv.FormatInt(id, 10), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestGetRun_NotFound(t *testing.T) {
	_, _, h := buildSrv(t, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/999", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestListRuns(t *testing.T) {
	_, s, h := buildSrv(t, "")
	_, _ = s.InsertRun(context.Background(), triagearr.Run{
		TriggeredBy: triagearr.RunTriggerCLI, TriggeredAt: time.Now().UTC(),
		Mode: "dry-run", StopReason: triagearr.StopNoMoreCandidates, Status: "completed",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Runs []any `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Runs, 1)
}

func TestPostRun_RejectsUnknownFields(t *testing.T) {
	_, _, h := buildSrv(t, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader([]byte(`{"foo":1}`)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

