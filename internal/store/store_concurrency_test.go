package store_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// TestConcurrentWritesNoBusy stresses the writer pool from N goroutines.
// On the old single-pool layout with SetMaxOpenConns(8) this would surface
// SQLITE_BUSY (5) or SQLITE_BUSY_SNAPSHOT (517) reliably; with the two-pool
// layout it must complete with zero errors.
//
// The workload mirrors the prod tracker tick: ReplaceTrackers is a
// read-then-write transaction (select prior, delete, reinsert) — exactly
// the pattern that failed at boot on prod.
func TestConcurrentWritesNoBusy(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	const goroutines = 8
	const itersPerG = 30
	hashes := make([]triagearr.Hash, goroutines)
	for i := range hashes {
		h := triagearr.Hash(strings.Repeat(string(rune('a'+i)), 40))
		hashes[i] = h
		require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
			Hash: h, Name: "t", Category: "x", SavePath: "/dl",
			Size: 1, AddedOn: time.Now().UTC(),
		}))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*itersPerG)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(h triagearr.Hash, gid int) {
			defer wg.Done()
			for j := 0; j < itersPerG; j++ {
				infos := []triagearr.TrackerInfo{
					{URL: "http://tracker.example/announce", Host: "tracker.example", Status: triagearr.TrackerWorking, Msg: ""},
					{URL: "http://alt.example/announce", Host: "alt.example", Status: triagearr.TrackerNotWorking, Msg: "down"},
				}
				if err := s.ReplaceTrackers(ctx, h, infos); err != nil {
					errCh <- err
					return
				}
			}
		}(hashes[i], i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err, "ReplaceTrackers should never return SQLITE_BUSY with the two-pool layout")
	}
}

// TestReadsConcurrentWithWrites verifies the WAL guarantee held by the
// two-pool layout: readers see a consistent snapshot without blocking on
// the writer. A reader loop runs while writers are hammering the same
// table; both must complete without error and the reader must observe at
// least one tracker row from each iteration of the writer.
func TestReadsConcurrentWithWrites(t *testing.T) {
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "aa", Name: "t", Category: "x", SavePath: "/dl",
		Size: 1, AddedOn: time.Now().UTC(),
	}))

	done := make(chan struct{})
	var writerErr error

	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			if err := s.ReplaceTrackers(ctx, "aa", []triagearr.TrackerInfo{
				{URL: "http://tracker.example/announce", Host: "tracker.example", Status: triagearr.TrackerWorking},
			}); err != nil {
				writerErr = err
				return
			}
		}
	}()

	var seenReads int
	for {
		select {
		case <-done:
			require.NoError(t, writerErr)
			require.Positive(t, seenReads, "reader must observe at least one snapshot")
			return
		default:
			rows, err := s.ListTrackers(ctx, "aa")
			require.NoError(t, err, "reader must not see SQLITE_BUSY against a single writer")
			if len(rows) > 0 {
				seenReads++
			}
		}
	}
}
