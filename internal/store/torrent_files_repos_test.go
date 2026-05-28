package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func i64ptr(v int64) *int64 { return &v }

func TestUpsertTorrentFile_AndListByHash(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	sampled := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.UpsertTorrentFile(ctx, "h1", "b/two.mkv", 200, i64ptr(2), sampled))
	require.NoError(t, s.UpsertTorrentFile(ctx, "h1", "a/one.mkv", 100, i64ptr(1), sampled))

	rows, err := s.TorrentFilesByHash(ctx, "h1")
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// Ordered by rel_path.
	require.Equal(t, "a/one.mkv", rows[0].RelPath)
	require.Equal(t, "b/two.mkv", rows[1].RelPath)
	require.Equal(t, int64(100), rows[0].SizeBytes)
	require.NotNil(t, rows[0].Nlink)
	require.Equal(t, int64(1), *rows[0].Nlink)
	require.NotNil(t, rows[0].SampledAt)
	require.WithinDuration(t, sampled, *rows[0].SampledAt, time.Second)
}

func TestUpsertTorrentFile_ConflictUpdates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// First seen from a qBit-only snapshot: no stat yet → nlink NULL, no sample time.
	require.NoError(t, s.UpsertTorrentFile(ctx, "h1", "f.mkv", 100, nil, time.Time{}))
	rows, err := s.TorrentFilesByHash(ctx, "h1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0].Nlink, "unsampled file carries NULL nlink")
	require.Nil(t, rows[0].SampledAt, "zero sampledAt is stored as NULL")

	// The files-poller later revisits and stats it: same PK, refreshed values.
	sampled := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.UpsertTorrentFile(ctx, "h1", "f.mkv", 150, i64ptr(3), sampled))

	rows, err = s.TorrentFilesByHash(ctx, "h1")
	require.NoError(t, err)
	require.Len(t, rows, 1, "same (hash, rel_path) upserts in place")
	require.Equal(t, int64(150), rows[0].SizeBytes)
	require.NotNil(t, rows[0].Nlink)
	require.Equal(t, int64(3), *rows[0].Nlink)
	require.NotNil(t, rows[0].SampledAt)
}

func TestTorrentFilesByHash_UnknownHashEmpty(t *testing.T) {
	s := openTestStore(t)
	rows, err := s.TorrentFilesByHash(context.Background(), "nope")
	require.NoError(t, err)
	require.Empty(t, rows)
}

// TestMaxNlinkByHashes pins the cross-seed pre-filter semantics: the Decider
// treats a hash *absent* from the result as "unsampled, keep eligible" and
// lets the Actor's T3.5 stat re-check catch any conflict atomically. So a hash
// whose files all have NULL nlink must NOT appear, while a hash with at least
// one sampled file reports the max across its files.
func TestMaxNlinkByHashes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// h1: three sampled files, max nlink = 3.
	require.NoError(t, s.UpsertTorrentFile(ctx, "h1", "a", 1, i64ptr(1), time.Now()))
	require.NoError(t, s.UpsertTorrentFile(ctx, "h1", "b", 1, i64ptr(3), time.Now()))
	require.NoError(t, s.UpsertTorrentFile(ctx, "h1", "c", 1, i64ptr(2), time.Now()))
	// h2: all files unsampled (NULL nlink) → must be absent from the result.
	require.NoError(t, s.UpsertTorrentFile(ctx, "h2", "a", 1, nil, time.Time{}))
	require.NoError(t, s.UpsertTorrentFile(ctx, "h2", "b", 1, nil, time.Time{}))
	// h3: mix of NULL and sampled → max ignores the NULL.
	require.NoError(t, s.UpsertTorrentFile(ctx, "h3", "a", 1, nil, time.Time{}))
	require.NoError(t, s.UpsertTorrentFile(ctx, "h3", "b", 1, i64ptr(5), time.Now()))

	got, err := s.MaxNlinkByHashes(ctx, []triagearr.Hash{"h1", "h2", "h3", "h4-unknown"})
	require.NoError(t, err)

	require.Equal(t, int64(3), got["h1"])
	require.Equal(t, int64(5), got["h3"])
	_, h2Present := got["h2"]
	require.False(t, h2Present, "all-NULL hash is absent → treated as unsampled, not 'no hardlinks'")
	_, h4Present := got["h4-unknown"]
	require.False(t, h4Present, "hash with no rows is absent")
}

func TestMaxNlinkByHashes_EmptyInput(t *testing.T) {
	s := openTestStore(t)
	got, err := s.MaxNlinkByHashes(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, got)
}
