package server

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/Triagearr/Triagearr/internal/store"
)

type summaryResponse struct {
	Volume         volumeView      `json:"volume"`
	Arrs           []arrView       `json:"arrs"`
	TorrentClients []clientView    `json:"torrent_clients"`
	Counts         summaryCounts   `json:"counts"`
	LastRuns       []runResponse   `json:"last_runs"`
	TopScore       []scoreListItem `json:"top_score"`
}

type summaryCounts struct {
	Torrents int `json:"torrents"`
	Scored   int `json:"scored"`
	Actions  int `json:"actions"`
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Fan out the eight independent reads across the reader pool. Each goroutine
	// owns its slot in the response struct, so no mutex is needed.
	var (
		wg       sync.WaitGroup
		volume   volumeView
		arrs     []arrView
		clients  []clientView
		counts   summaryCounts
		lastRuns []runResponse
		top      []scoreListItem
	)
	run := func(label string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				slog.Warn("summary: "+label, "err", err)
			}
		}()
	}
	run("volume", func() error {
		vv, err := s.buildVolumeView(ctx)
		volume = vv
		return err
	})
	run("arrs", func() error {
		a, err := s.buildArrViews(ctx)
		arrs = a
		return err
	})
	run("torrent clients", func() error {
		c, err := s.buildClientViews(ctx)
		clients = c
		return err
	})
	run("count torrents", func() error {
		n, err := s.opts.Store.CountTorrents(ctx)
		counts.Torrents = n
		return err
	})
	run("count scored", func() error {
		n, err := s.opts.Store.CountScored(ctx)
		counts.Scored = n
		return err
	})
	run("count actions", func() error {
		n, err := s.opts.Store.CountActions(ctx)
		counts.Actions = n
		return err
	})
	run("list runs", func() error {
		runs, err := s.opts.Store.ListRuns(ctx, store.ListRunsOpts{Limit: 10})
		if err != nil {
			return err
		}
		lastRuns = make([]runResponse, len(runs))
		for i, rn := range runs {
			lastRuns[i] = buildResponse(rn, nil)
		}
		return nil
	})
	run("list scores", func() error {
		rows, err := s.opts.Store.ListScores(ctx, store.ListScoresOpts{Limit: 10, WithFactors: true})
		if err != nil {
			return err
		}
		top = make([]scoreListItem, len(rows))
		for i, row := range rows {
			top[i] = scoreItemFromRow(row)
		}
		return nil
	})
	wg.Wait()

	writeJSON(w, http.StatusOK, summaryResponse{
		Volume: volume, Arrs: arrs, TorrentClients: clients, Counts: counts,
		LastRuns: lastRuns, TopScore: top,
	})
}
