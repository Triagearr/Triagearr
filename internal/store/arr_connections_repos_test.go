package store_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/store"
)

func sampleConn() store.ArrConnection {
	return store.ArrConnection{
		Kind: "sonarr", URL: "http://sonarr:8989", PublicURL: "https://sonarr.example.com", APIKey: "k1",
		Enabled: true, Poll: true, Act: false,
		TagsExclude: []string{"keep"}, CategoriesOnly: []string{"tv"},
		TimeoutMS: 30000,
	}
}

func TestArrConnections_UpsertAndGet(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	saved, err := s.UpsertArrConnection(ctx, sampleConn())
	require.NoError(t, err)
	require.Positive(t, saved.ID)
	require.Equal(t, "sonarr", saved.Kind)
	require.Equal(t, "https://sonarr.example.com", saved.PublicURL)
	require.Equal(t, []string{"keep"}, saved.TagsExclude)
	require.Equal(t, []string{"tv"}, saved.CategoriesOnly)
	require.True(t, saved.Enabled)
	require.False(t, saved.Act)

	// Update via second upsert.
	updated := sampleConn()
	updated.URL = "http://sonarr:9999"
	updated.PublicURL = ""
	updated.Act = true
	updated.TagsExclude = nil
	after, err := s.UpsertArrConnection(ctx, updated)
	require.NoError(t, err)
	require.Equal(t, "http://sonarr:9999", after.URL)
	require.Empty(t, after.PublicURL)
	require.True(t, after.Act)
	require.Empty(t, after.TagsExclude)

	got, err := s.GetArrConnectionByKind(ctx, "sonarr")
	require.NoError(t, err)
	require.Equal(t, "http://sonarr:9999", got.URL)
}

func TestArrConnections_DeleteByKind(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.UpsertArrConnection(ctx, sampleConn())
	require.NoError(t, err)

	require.NoError(t, s.DeleteArrConnectionByKind(ctx, "sonarr"))
	_, err = s.GetArrConnectionByKind(ctx, "sonarr")
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.ErrorIs(t, s.DeleteArrConnectionByKind(ctx, "sonarr"), sql.ErrNoRows)
}

func TestArrConnections_ListOrderedAndCount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	n, err := s.CountArrConnections(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n)

	radarr := sampleConn()
	radarr.Kind = "radarr"
	_, err = s.UpsertArrConnection(ctx, radarr)
	require.NoError(t, err)
	_, err = s.UpsertArrConnection(ctx, sampleConn())
	require.NoError(t, err)

	n, err = s.CountArrConnections(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	list, err := s.ListArrConnections(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	// Ordered by kind: radarr before sonarr.
	require.Equal(t, "radarr", list[0].Kind)
	require.Equal(t, "sonarr", list[1].Kind)
}

func TestArrConnections_Seed(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SeedArrConnections(ctx, nil))
	n, err := s.CountArrConnections(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n, "seeding nil must be a no-op")

	radarr := sampleConn()
	radarr.Kind = "radarr"
	require.NoError(t, s.SeedArrConnections(ctx, []store.ArrConnection{sampleConn(), radarr}))

	list, err := s.ListArrConnections(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
}
