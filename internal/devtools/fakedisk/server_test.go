package fakedisk_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/devtools/fakedisk"
)

func TestFakeDisk_GetSetFillFree(t *testing.T) {
	srv := fakedisk.New(fakedisk.Options{})
	srv.State().Set("dev", 1000, 500)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	get := func() fakedisk.DiskInfo {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, httpSrv.URL+"/disk/dev", nil)
		require.NoError(t, err)
		resp, err := httpSrv.Client().Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var info fakedisk.DiskInfo
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&info))
		return info
	}
	post := func(path string, body any) fakedisk.DiskInfo {
		b, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, httpSrv.URL+path, bytes.NewReader(b))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpSrv.Client().Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var info fakedisk.DiskInfo
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&info))
		return info
	}

	info := get()
	require.Equal(t, uint64(500), info.FreeBytes)
	require.InDelta(t, 50.0, info.FreePercent, 0.001)

	info = post("/disk/dev/fill", map[string]uint64{"bytes": 200})
	require.Equal(t, uint64(300), info.FreeBytes)

	info = post("/disk/dev/free", map[string]uint64{"bytes": 100})
	require.Equal(t, uint64(400), info.FreeBytes)

	info = post("/disk/dev", map[string]uint64{"total_bytes": 2000, "free_bytes": 1500})
	require.Equal(t, uint64(2000), info.TotalBytes)
	require.Equal(t, uint64(1500), info.FreeBytes)
}

func TestFakeDisk_UnknownVolume404(t *testing.T) {
	srv := fakedisk.New(fakedisk.Options{})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, httpSrv.URL+"/disk/missing", nil)
	require.NoError(t, err)
	resp, err := httpSrv.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestFakeDisk_FreeClampedToTotal(t *testing.T) {
	srv := fakedisk.New(fakedisk.Options{})
	srv.State().Set("dev", 1000, 200)
	info, ok := srv.State().Free("dev", 10000)
	require.True(t, ok)
	require.Equal(t, uint64(1000), info.FreeBytes)
	require.InDelta(t, 100.0, info.FreePercent, 0.001)
}
