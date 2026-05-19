//go:build linux

package mapper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatInode_HardlinksShareInode(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original.mkv")
	link := filepath.Join(dir, "link.mkv")
	require.NoError(t, os.WriteFile(original, []byte("payload"), 0o644)) //nolint:gosec // test fixture
	require.NoError(t, os.Link(original, link))

	ino1, nl1, err := statInode(original)
	require.NoError(t, err)
	ino2, nl2, err := statInode(link)
	require.NoError(t, err)

	require.Equal(t, ino1, ino2, "hard-linked files must share an inode")
	require.Equal(t, uint64(2), nl1)
	require.Equal(t, uint64(2), nl2)

	// Removing one link decrements nlink for the survivor.
	require.NoError(t, os.Remove(link))
	_, nl3, err := statInode(original)
	require.NoError(t, err)
	require.Equal(t, uint64(1), nl3)
}
