package qbit_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/torrent/qbit"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestListTrackers_FiltersSyntheticAndParsesHost(t *testing.T) {
	srv := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/torrents/trackers" {
			http.NotFound(w, r)
			return
		}
		require.Equal(t, "abc", r.URL.Query().Get("hash"))
		w.Header().Set("Content-Type", "application/json")
		// qBit prepends synthetic ** [DHT] ** / [PeX] / [LSD] rows that carry no
		// real URL — looksLikeURL must drop them. Real announce URLs keep only
		// their lowercased host (port stripped) via parseTrackerHost.
		_, _ = w.Write([]byte(`[
			{"url":"** [DHT] **","status":2,"msg":""},
			{"url":"** [PeX] **","status":2,"msg":""},
			{"url":"http://Tracker.Example.COM:8080/announce","status":2,"msg":"OK"},
			{"url":"udp://tracker2.example.org:6969/announce","status":4,"msg":"down"}
		]`))
	}))

	c, err := qbit.New(qbit.Options{BaseURL: srv.URL})
	require.NoError(t, err)

	trackers, err := c.ListTrackers(context.Background(), triagearr.Hash("abc"))
	require.NoError(t, err)
	require.Len(t, trackers, 2, "synthetic DHT/PeX rows must be filtered out")

	require.Equal(t, "tracker.example.com", trackers[0].Host)
	require.Equal(t, triagearr.TrackerWorking, trackers[0].Status)
	require.Equal(t, "OK", trackers[0].Msg)

	require.Equal(t, "tracker2.example.org", trackers[1].Host)
	require.Equal(t, triagearr.TrackerNotWorking, trackers[1].Status)
}
