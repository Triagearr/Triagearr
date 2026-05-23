package preflight_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/preflight"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type fakeQbit struct {
	tors []triagearr.Torrent
	err  error
}

func (f *fakeQbit) ListTorrents(_ context.Context) ([]triagearr.Torrent, error) {
	return f.tors, f.err
}

// TestValidate_ConformingMount uses real directories: the volume root + a
// couple of qBit save_paths that all resolve. Validate must return nil.
func TestValidate_ConformingMount(t *testing.T) {
	root := t.TempDir()
	tv := filepath.Join(root, "torrents", "tv")
	movies := filepath.Join(root, "torrents", "movies")
	require.NoError(t, os.MkdirAll(tv, 0o755))
	require.NoError(t, os.MkdirAll(movies, 0o755))
	qb := &fakeQbit{tors: []triagearr.Torrent{
		{Name: "Foo", SavePath: tv},
		{Name: "Bar", SavePath: movies},
	}}
	require.NoError(t, preflight.Validate(context.Background(), qb, root, os.Stat))
}

// TestValidate_MissingVolume catches the most common misconfig: the operator
// pointed Triagearr at /data but did not mount /data into the container.
func TestValidate_MissingVolume(t *testing.T) {
	err := preflight.Validate(context.Background(), nil, "/definitely/not/mounted/anywhere", os.Stat)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not exist in Triagearr's mount namespace")
	require.Contains(t, err.Error(), "ADR-0023")
}

// TestValidate_SavePathMissing simulates the cross-namespace mismatch ADR-0023
// closes: qBit reports /files/torrents/tv but Triagearr sees /share/files/...
// Validate must name the offending path.
func TestValidate_SavePathMissing(t *testing.T) {
	root := t.TempDir()
	qb := &fakeQbit{tors: []triagearr.Torrent{
		{Name: "Foo", SavePath: "/files/torrents/tv"}, // qBit-namespace path, not in tmpdir
	}}
	err := preflight.Validate(context.Background(), qb, root, os.Stat)
	require.Error(t, err)
	require.Contains(t, err.Error(), "/files/torrents/tv")
	require.Contains(t, err.Error(), `torrent "Foo"`)
}

// TestValidate_QbitUnreachable_TolerantBoot: ListTorrents failures are NOT
// preflight failures (the qBit poller surfaces those separately).
func TestValidate_QbitUnreachable_TolerantBoot(t *testing.T) {
	root := t.TempDir()
	qb := &fakeQbit{err: errors.New("connection refused")}
	require.NoError(t, preflight.Validate(context.Background(), qb, root, os.Stat))
}

// TestValidate_NilQbit_OnlyVolumeProbe is the "qbit disabled in config" branch.
func TestValidate_NilQbit_OnlyVolumeProbe(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, preflight.Validate(context.Background(), nil, root, os.Stat))
}
