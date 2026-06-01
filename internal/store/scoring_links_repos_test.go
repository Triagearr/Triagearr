package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// seedLinkedMedia wires one qBit hash to one *arr media item through the
// arr_imports → media_files → media chain the scoring joins walk.
func seedLinkedMedia(t *testing.T, s interface {
	UpsertTorrent(context.Context, triagearr.Torrent) error
	UpsertMedia(context.Context, triagearr.MediaItem) error
	UpsertMediaFile(context.Context, triagearr.MediaFile) error
	UpsertArrImport(context.Context, triagearr.ArrType, triagearr.ImportRecord) error
}, hash triagearr.Hash, fileID int64, mediaID triagearr.MediaID, tags []string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{Hash: hash, Name: string(hash), AddedOn: now}))
	require.NoError(t, s.UpsertMedia(ctx, triagearr.MediaItem{
		ID: mediaID, ArrType: triagearr.ArrTypeSonarr, Title: "Show", Path: "/m", Tags: tags,
	}))
	require.NoError(t, s.UpsertMediaFile(ctx, triagearr.MediaFile{
		ArrType: triagearr.ArrTypeSonarr, FileID: fileID, MediaID: mediaID, Path: "/m/e.mkv", Size: 10,
	}))
	require.NoError(t, s.UpsertArrImport(ctx, triagearr.ArrTypeSonarr, triagearr.ImportRecord{
		HistoryID: fileID, FileID: fileID, DownloadID: hash,
		ImportedPath: "/m/e.mkv", ImportedAt: now,
	}))
}

func TestLinkedMedia_ForHashAndAll(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	seedLinkedMedia(t, s, "hashone", 11, 1, []string{"keep"})
	seedLinkedMedia(t, s, "hashtwo", 22, 2, nil)

	linked, err := s.LinkedMediaForHash(ctx, "hashone")
	require.NoError(t, err)
	require.Len(t, linked, 1)
	require.Equal(t, int64(1), linked[0].MediaID)
	require.Equal(t, string(triagearr.ArrTypeSonarr), linked[0].ArrType)

	// An unlinked hash returns nothing.
	none, err := s.LinkedMediaForHash(ctx, "ghost")
	require.NoError(t, err)
	require.Empty(t, none)

	all, err := s.LinkedMediaAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.Len(t, all["hashone"], 1)
	require.Len(t, all["hashtwo"], 1)

	hashes, err := s.HashesWithArrImports(ctx)
	require.NoError(t, err)
	require.Contains(t, hashes, triagearr.Hash("hashone"))
	require.Contains(t, hashes, triagearr.Hash("hashtwo"))

	n, err := s.CountArrImports(ctx, triagearr.ArrTypeSonarr)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestErrHashAmbiguous_Error(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, h := range []triagearr.Hash{
		"deadbeef00000000000000000000000000000001",
		"deadbeef00000000000000000000000000000002",
	} {
		require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{Hash: h, Name: string(h[:8]), AddedOn: now}))
	}

	_, err := s.ResolveTorrentHash(ctx, "deadbeef")
	require.Error(t, err)
	require.Contains(t, err.Error(), "deadbeef", "the ambiguous-prefix message echoes the prefix")
}

func TestListTrackersAll(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{Hash: "h1", Name: "H1", AddedOn: now}))
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{Hash: "h2", Name: "H2", AddedOn: now}))

	require.NoError(t, s.ReplaceTrackers(ctx, "h1", []triagearr.TrackerInfo{
		{URL: "http://t1/announce", Host: "t1", Status: triagearr.TrackerWorking, Msg: ""},
		{URL: "http://t2/announce", Host: "t2", Status: triagearr.TrackerNotWorking, Msg: "down"},
	}))
	require.NoError(t, s.ReplaceTrackers(ctx, "h2", []triagearr.TrackerInfo{
		{URL: "http://t3/announce", Host: "t3", Status: triagearr.TrackerWorking},
	}))

	byHash, err := s.ListTrackersAll(ctx)
	require.NoError(t, err)
	require.Len(t, byHash["h1"], 2)
	require.Len(t, byHash["h2"], 1)
}

func TestRuns_MarkStatusAndListBasic(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	runID, err := s.InsertRun(ctx, triagearr.Run{
		TriggeredBy: triagearr.RunTriggerCLI, TriggeredAt: now, Mode: "live", Status: "running",
	})
	require.NoError(t, err)

	require.NoError(t, s.MarkRunStatus(ctx, runID, "completed"))
	run, _, err := s.GetRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status)

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "h", Name: "H", SavePath: "/data", Size: 42, AddedOn: now,
	}))
	basic, err := s.ListTorrentsBasic(ctx)
	require.NoError(t, err)
	require.Len(t, basic, 1)
	require.Equal(t, "/data", basic[0].SavePath)
	require.Equal(t, int64(42), basic[0].Size)
}

func TestInsertSnapshots_Batch(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{Hash: "h", Name: "H", AddedOn: now}))

	// Empty batch is a no-op.
	require.NoError(t, s.InsertSnapshots(ctx, nil))

	require.NoError(t, s.InsertSnapshots(ctx, []triagearr.Snapshot{
		{Hash: "h", Timestamp: now.Add(-time.Hour), Ratio: 1, Seeders: 2, State: "uploading", LastActivity: now},
		{Hash: "h", Timestamp: now, Ratio: 2, Seeders: 3, State: "uploading", LastActivity: now},
	}))

	pts, err := s.ListSnapshotsRaw(ctx, "h", now.Add(-2*time.Hour), 0)
	require.NoError(t, err)
	require.Len(t, pts, 2)
}
