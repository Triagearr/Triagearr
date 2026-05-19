package linker_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/linker"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type fakeSource struct {
	lookups []triagearr.Hash
	links   []triagearr.Link
	err     error
}

func (f *fakeSource) LinksByHash(_ context.Context, hash triagearr.Hash) ([]triagearr.Link, error) {
	f.lookups = append(f.lookups, hash)
	if f.err != nil {
		return nil, f.err
	}
	return f.links, nil
}

func TestLinker_NormalisesHashToLowercase(t *testing.T) {
	src := &fakeSource{links: []triagearr.Link{{FileID: 7}}}
	l := linker.New(src)

	got, err := l.Links(context.Background(), "DEADBEEFCAFE0123456789ABCDEFABCDEFABCDEF")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, triagearr.Hash("deadbeefcafe0123456789abcdefabcdefabcdef"), src.lookups[0])
}

func TestLinker_PropagatesError(t *testing.T) {
	want := errors.New("boom")
	l := linker.New(&fakeSource{err: want})
	_, err := l.Links(context.Background(), "deadbeef")
	require.ErrorIs(t, err, want)
}

func TestLinker_EmptyOnOrphan(t *testing.T) {
	l := linker.New(&fakeSource{})
	got, err := l.Links(context.Background(), "deadbeef")
	require.NoError(t, err)
	require.Empty(t, got)
}
