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
		Kind: "sonarr", Name: "main", URL: "http://sonarr:8989", APIKey: "k1",
		Enabled: true, Poll: true, Act: false,
		TagsExclude: []string{"keep"}, CategoriesOnly: []string{"tv"},
		TimeoutMS: 30000,
	}
}

func TestArrConnections_CRUDRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id, err := s.CreateArrConnection(ctx, sampleConn())
	require.NoError(t, err)
	require.Positive(t, id)

	got, err := s.GetArrConnection(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "sonarr", got.Kind)
	require.Equal(t, "main", got.Name)
	require.Equal(t, []string{"keep"}, got.TagsExclude)
	require.Equal(t, []string{"tv"}, got.CategoriesOnly)
	require.True(t, got.Enabled)
	require.False(t, got.Act)

	got.URL = "http://sonarr:9999"
	got.Act = true
	got.TagsExclude = nil
	require.NoError(t, s.UpdateArrConnection(ctx, got))

	after, err := s.GetArrConnection(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "http://sonarr:9999", after.URL)
	require.True(t, after.Act)
	require.Empty(t, after.TagsExclude)

	require.NoError(t, s.DeleteArrConnection(ctx, id))
	_, err = s.GetArrConnection(ctx, id)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestArrConnections_ListOrderedAndCount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	n, err := s.CountArrConnections(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n)

	radarr := sampleConn()
	radarr.Kind, radarr.Name = "radarr", "movies"
	_, err = s.CreateArrConnection(ctx, radarr)
	require.NoError(t, err)
	_, err = s.CreateArrConnection(ctx, sampleConn())
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

func TestArrConnections_DuplicateKindNameRejected(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.CreateArrConnection(ctx, sampleConn())
	require.NoError(t, err)

	_, err = s.CreateArrConnection(ctx, sampleConn())
	require.Error(t, err, "unique (kind,name) must reject a duplicate")

	// Same name, different kind is fine.
	other := sampleConn()
	other.Kind = "radarr"
	_, err = s.CreateArrConnection(ctx, other)
	require.NoError(t, err)
}

func TestArrConnections_UpdateDeleteUnknownID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	missing := sampleConn()
	missing.ID = 999
	require.ErrorIs(t, s.UpdateArrConnection(ctx, missing), sql.ErrNoRows)
	require.ErrorIs(t, s.DeleteArrConnection(ctx, 999), sql.ErrNoRows)
}

func TestArrConnections_Seed(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SeedArrConnections(ctx, nil))
	n, err := s.CountArrConnections(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n, "seeding nil must be a no-op")

	radarr := sampleConn()
	radarr.Kind, radarr.Name = "radarr", "movies"
	require.NoError(t, s.SeedArrConnections(ctx, []store.ArrConnection{sampleConn(), radarr}))

	list, err := s.ListArrConnections(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)

	// A row colliding with an existing one aborts the whole transaction.
	err = s.SeedArrConnections(ctx, []store.ArrConnection{sampleConn()})
	require.Error(t, err)
}
