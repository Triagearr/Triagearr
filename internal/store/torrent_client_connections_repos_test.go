package store_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/store"
)

func sampleTorrentClientConn() store.TorrentClientConnection {
	return store.TorrentClientConnection{
		Kind: "qbittorrent", URL: "http://qbit:8080", PublicURL: "https://qbit.example.com",
		Username: "admin", Password: "secret", Enabled: true,
		CategoryExclude: []string{"keep"}, TagsExclude: []string{"hnr"},
		DeleteWithFiles: true, TimeoutMS: 30000,
	}
}

func TestTorrentClientConnections_UpsertAndGet(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	saved, err := s.UpsertTorrentClientConnection(ctx, sampleTorrentClientConn())
	require.NoError(t, err)
	require.Positive(t, saved.ID)
	require.Equal(t, "qbittorrent", saved.Kind)
	require.Equal(t, "https://qbit.example.com", saved.PublicURL)
	require.Equal(t, []string{"keep"}, saved.CategoryExclude)
	require.Equal(t, []string{"hnr"}, saved.TagsExclude)
	require.True(t, saved.Enabled)
	require.True(t, saved.DeleteWithFiles)

	// Update via second upsert preserves created_at, applies new fields.
	updated := sampleTorrentClientConn()
	updated.URL = "http://qbit:9999"
	updated.PublicURL = ""
	updated.DeleteWithFiles = false
	updated.CategoryExclude = nil
	after, err := s.UpsertTorrentClientConnection(ctx, updated)
	require.NoError(t, err)
	require.Equal(t, "http://qbit:9999", after.URL)
	require.Empty(t, after.PublicURL)
	require.False(t, after.DeleteWithFiles)
	require.Empty(t, after.CategoryExclude)
	require.True(t, after.CreatedAt.Equal(saved.CreatedAt), "created_at must be preserved on update")

	got, err := s.GetTorrentClientConnectionByKind(ctx, "qbittorrent")
	require.NoError(t, err)
	require.Equal(t, "http://qbit:9999", got.URL)
}

func TestTorrentClientConnections_GetMissing(t *testing.T) {
	s := openTestStore(t)
	_, err := s.GetTorrentClientConnectionByKind(context.Background(), "qbittorrent")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestTorrentClientConnections_DeleteByKind(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.UpsertTorrentClientConnection(ctx, sampleTorrentClientConn())
	require.NoError(t, err)

	require.NoError(t, s.DeleteTorrentClientConnectionByKind(ctx, "qbittorrent"))
	_, err = s.GetTorrentClientConnectionByKind(ctx, "qbittorrent")
	require.ErrorIs(t, err, sql.ErrNoRows)

	// Deleting an unknown kind reports no rows so the HTTP layer can 404.
	require.ErrorIs(t, s.DeleteTorrentClientConnectionByKind(ctx, "qbittorrent"), sql.ErrNoRows)
}

func TestTorrentClientConnections_ListOrderedAndCount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	n, err := s.CountTorrentClientConnections(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n)

	deluge := sampleTorrentClientConn()
	deluge.Kind = "deluge"
	_, err = s.UpsertTorrentClientConnection(ctx, deluge)
	require.NoError(t, err)
	_, err = s.UpsertTorrentClientConnection(ctx, sampleTorrentClientConn())
	require.NoError(t, err)

	n, err = s.CountTorrentClientConnections(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	list, err := s.ListTorrentClientConnections(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	// Ordered by kind: deluge before qbittorrent.
	require.Equal(t, "deluge", list[0].Kind)
	require.Equal(t, "qbittorrent", list[1].Kind)
}

func TestTorrentClientConnections_Seed(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SeedTorrentClientConnections(ctx, nil))
	n, err := s.CountTorrentClientConnections(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n, "seeding nil must be a no-op")

	deluge := sampleTorrentClientConn()
	deluge.Kind = "deluge"
	require.NoError(t, s.SeedTorrentClientConnections(ctx,
		[]store.TorrentClientConnection{sampleTorrentClientConn(), deluge}))

	list, err := s.ListTorrentClientConnections(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
}
