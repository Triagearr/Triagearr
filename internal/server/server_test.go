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

// seedAuthUser registers a dummy auth user so the server's auth middleware
// switches from open-mode to "cookie OR X-API-Key required". Used by tests
// that exercise rejection paths.
func seedAuthUser(t *testing.T, s *store.Store) {
	t.Helper()
	_, err := s.InsertAuthUser(context.Background(), "test", "$2a$10$abcdefghijklmnopqrstuv")
	require.NoError(t, err)
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
		Path: "/data", Timestamp: time.Now().UTC(),
		TotalBytes: 100 * 1024 * 1024 * 1024, FreePercent: 5,
	}))
}

const testAPIKey = "test-key-deadbeef"

func buildSrv(t *testing.T, apiKey string) (*server.Server, *store.Store, http.Handler) {
	return buildSrvWithDaemonLive(t, apiKey, false)
}

func buildSrvWithDaemonLive(t *testing.T, apiKey string, daemonLive bool) (*server.Server, *store.Store, http.Handler) {
	if apiKey == "" {
		apiKey = testAPIKey
	}
	t.Helper()
	s := testStore(t)
	seed(t, s)
	vol := decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 20,
	}
	srv := server.New(server.Options{
		Bind:       "127.0.0.1:0",
		APIKey:     apiKey,
		Store:      s,
		Decider:    decider.New(s),
		Volume:     func() decider.Volume { return vol },
		DaemonLive: daemonLive,
	})
	return srv, s, srv.Handler()
}

func TestPostRun_AuthMissing(t *testing.T) {
	_, s, h := buildSrv(t, "sekrit")
	seedAuthUser(t, s)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/runs", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPostRun_AuthOK_InsertsRun(t *testing.T) {
	_, s, h := buildSrv(t, "sekrit")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/runs", strings.NewReader(`{}`))
	req.Header.Set("X-API-Key", "sekrit")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "dry-run", body["mode"])

	runs, err := s.ListRuns(context.Background(), store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 1)
}

func authedReq(method, target string, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequestWithContext(context.Background(), method, target, nil)
	} else {
		r = httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	}
	r.Header.Set("X-API-Key", testAPIKey)
	return r
}

func TestGetRun_Found(t *testing.T) {
	_, s, h := buildSrv(t, "")
	id, err := s.InsertRun(context.Background(), triagearr.Run{
		TriggeredBy: triagearr.RunTriggerHTTP, TriggeredAt: time.Now().UTC(),
		Mode: "dry-run", StopReason: triagearr.StopNoMoreCandidates, Status: "completed",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/runs/"+strconv.FormatInt(id, 10), ""))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestGetRun_NotFound(t *testing.T) {
	_, _, h := buildSrv(t, "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/runs/999", ""))
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestListRuns(t *testing.T) {
	_, s, h := buildSrv(t, "")
	_, _ = s.InsertRun(context.Background(), triagearr.Run{
		TriggeredBy: triagearr.RunTriggerCLI, TriggeredAt: time.Now().UTC(),
		Mode: "dry-run", StopReason: triagearr.StopNoMoreCandidates, Status: "completed",
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/runs", ""))
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Runs []any `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Runs, 1)
}

func TestPostRun_RejectsUnknownFields(t *testing.T) {
	_, _, h := buildSrv(t, "")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/runs", bytes.NewReader([]byte(`{"foo":1}`)))
	req.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPostRun_LiveBody_DaemonLive_ResolvesToLive(t *testing.T) {
	_, _, h := buildSrvWithDaemonLive(t, "", true)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodPost, "/api/v1/runs", `{"mode":"live"}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "live", body["mode"])
}

func TestPostRun_NoModeBody_DaemonLive_StaysDryRun(t *testing.T) {
	_, _, h := buildSrvWithDaemonLive(t, "", true)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodPost, "/api/v1/runs", `{}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "dry-run", body["mode"])
}

func TestPostRun_LiveBody_DaemonDryRun_ForcedDryRun(t *testing.T) {
	_, _, h := buildSrvWithDaemonLive(t, "", false)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodPost, "/api/v1/runs", `{"mode":"live"}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "dry-run", body["mode"])
}
