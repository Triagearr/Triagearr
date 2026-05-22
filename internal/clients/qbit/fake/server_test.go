package fake_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/qbit"
	"github.com/Triagearr/Triagearr/internal/clients/qbit/fake"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// seed loads a small but representative torrent set: two privates with files
// + trackers, one public without trackers (exercises the empty-list path).
func seed(s *fake.Server) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC).Unix()
	s.State().AddMany([]fake.Torrent{
		{
			Hash: "aaaa", Name: "Show.S01.1080p", Category: "tv-sonarr",
			SavePath: "/data/torrents/tv", Size: 12_000_000_000,
			AddedOn: now - 86400*30, CompletionOn: now - 86400*29,
			Ratio: 1.8, Uploaded: 21_000_000_000,
			NumComplete: 12, NumIncomplete: 3,
			State: "uploading", LastActivity: now, Private: true,
			Tags: "hd,french",
			Files: []fake.File{
				{Name: "Show.S01E01.mkv", Size: 3_000_000_000, Progress: 1.0},
				{Name: "Show.S01E02.mkv", Size: 3_000_000_000, Progress: 1.0},
			},
			Trackers: []fake.Tracker{
				{URL: "https://tracker.example.org/announce", Status: 2},
			},
		},
		{
			Hash: "bbbb", Name: "Movie.2024.1080p", Category: "movies-radarr",
			SavePath: "/data/torrents/movies", Size: 8_000_000_000,
			AddedOn: now - 86400*5, CompletionOn: now - 86400*4,
			Ratio: 0.3, Uploaded: 2_400_000_000,
			NumComplete: 0, NumIncomplete: 0,
			State: "stalledUP", LastActivity: now - 3600, Private: true,
			Files: []fake.File{
				{Name: "Movie.2024.mkv", Size: 8_000_000_000, Progress: 1.0},
			},
			Trackers: []fake.Tracker{
				{URL: "https://dead.example.org/announce", Status: 4, Msg: "host unreachable"},
			},
		},
		{
			Hash: "cccc", Name: "Public.Release", Category: "",
			SavePath: "/data/torrents", Size: 500_000_000,
			AddedOn: now - 86400, Ratio: 0.0,
			State: "downloading", LastActivity: now, Private: false,
		},
	})
}

func TestFake_RoundTripWithRealClient(t *testing.T) {
	srv := fake.New(fake.Options{Username: "admin", Password: "adminadmin"})
	seed(srv)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c, err := qbit.New(qbit.Options{
		BaseURL:  httpSrv.URL,
		Username: "admin",
		Password: "adminadmin",
	})
	require.NoError(t, err)

	ctx := context.Background()

	tors, err := c.ListTorrents(ctx)
	require.NoError(t, err)
	require.Len(t, tors, 3)

	byHash := map[triagearr.Hash]triagearr.Torrent{}
	for _, t := range tors {
		byHash[t.Hash] = t
	}
	require.Equal(t, "Show.S01.1080p", byHash["aaaa"].Name)
	require.Equal(t, 12, byHash["aaaa"].Seeders)
	require.True(t, byHash["aaaa"].Private)
	require.False(t, byHash["cccc"].Private)

	files, err := c.TorrentFiles(ctx, "aaaa")
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "Show.S01E01.mkv", files[0].Name)

	trackers, err := c.ListTrackers(ctx, "bbbb")
	require.NoError(t, err)
	// The fake injects 3 synthetic DHT/PEX/LSD pseudo-trackers; the real
	// client filters them out — only the real https URL should survive.
	require.Len(t, trackers, 1)
	require.Equal(t, triagearr.TrackerNotWorking, trackers[0].Status)
	require.Equal(t, "dead.example.org", trackers[0].Host)

	require.NoError(t, c.Delete(ctx, "bbbb", triagearr.DeleteOpts{DeleteFiles: true}))
	require.Equal(t, int64(1), srv.State().DeleteCalls())
	require.Equal(t, 2, srv.State().Len())
	_, present := srv.State().Get("bbbb")
	require.False(t, present)

	tors, err = c.ListTorrents(ctx)
	require.NoError(t, err)
	require.Len(t, tors, 2)
}

func TestFake_AuthBypass(t *testing.T) {
	srv := fake.New(fake.Options{})
	seed(srv)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c, err := qbit.New(qbit.Options{BaseURL: httpSrv.URL})
	require.NoError(t, err)
	tors, err := c.ListTorrents(context.Background())
	require.NoError(t, err)
	require.Len(t, tors, 3)
}

func TestFake_BadCredentials(t *testing.T) {
	srv := fake.New(fake.Options{Username: "admin", Password: "adminadmin"})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c, err := qbit.New(qbit.Options{
		BaseURL:  httpSrv.URL,
		Username: "admin",
		Password: "wrong",
	})
	require.NoError(t, err)
	_, err = c.ListTorrents(context.Background())
	require.Error(t, err)
}

func TestFake_UnknownEndpointReturns501(t *testing.T) {
	srv := fake.New(fake.Options{})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	resp, err := httpSrv.Client().Get(httpSrv.URL + "/api/v2/app/version")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, 501, resp.StatusCode)
}
