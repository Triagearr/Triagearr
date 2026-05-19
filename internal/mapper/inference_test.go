package mapper_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/mapper"
)

// writeFile creates path with given content and returns its size.
func writeFile(t *testing.T, path string, content string) int64 {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644)) //nolint:gosec // test fixture
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Size()
}

func TestInfer_Identity(t *testing.T) {
	root := t.TempDir()
	files := []string{"a.mkv", "tv/foo/E01.mkv", "tv/foo/E02.mkv", "movies/bar.mkv", "movies/baz.mkv"}
	samples := make([]mapper.Sample, 0, len(files))
	for _, rel := range files {
		full := filepath.Join(root, rel)
		size := writeFile(t, full, rel)
		// Source path is identical to the local path (no remap needed).
		samples = append(samples, mapper.Sample{SourcePath: full, Size: size})
	}
	idx, err := mapper.BuildIndex("test", root, 1000)
	require.NoError(t, err)

	res := mapper.Infer(idx, samples)
	require.True(t, res.Accepted, "identity inference should succeed; candidates=%+v", res.Candidates)
	require.Equal(t, mapper.OriginIdentity, res.Rule.Origin)
	require.Equal(t, "", res.Rule.From)
	require.Equal(t, "", res.Rule.To)
}

func TestInfer_PrefixMismatch_QNAP(t *testing.T) {
	root := t.TempDir()
	// Simulate Triagearr-side layout: /<root>/files/...
	files := []string{
		"files/torrents/tv/Foo.S01E01.mkv",
		"files/torrents/tv/Foo.S01E02.mkv",
		"files/torrents/movies/Bar.mkv",
		"files/media/tv/Foo/Foo.S01E01.mkv",
		"files/media/tv/Foo/Foo.S01E02.mkv",
		"files/media/movies/Bar/Bar.mkv",
	}
	samples := make([]mapper.Sample, 0, len(files))
	for _, rel := range files {
		full := filepath.Join(root, rel)
		size := writeFile(t, full, rel)
		// Source paths use /files/... (the qBit/*arr view).
		src := "/files/" + rel[len("files/"):]
		samples = append(samples, mapper.Sample{SourcePath: src, Size: size})
		_ = full
	}
	idx, err := mapper.BuildIndex("test", root, 1000)
	require.NoError(t, err)

	res := mapper.Infer(idx, samples)
	require.True(t, res.Accepted, "QNAP-style inference should succeed; candidates=%+v", res.Candidates)
	require.Equal(t, mapper.OriginInferred, res.Rule.Origin)

	// Verify the inferred rule produces correct translations regardless of how
	// the prefix was decomposed. The QNAP-style mismatch can be expressed as
	// either /files/ → root+/files/ or / → root+/, both correct.
	r := mapper.NewResolver()
	r.Set([]mapper.VolumeRules{{VolumeName: "v", Rules: []mapper.Rule{res.Rule}}})
	for _, s := range samples {
		local, _, ok := r.Translate(s.SourcePath)
		require.True(t, ok, "rule should translate %s", s.SourcePath)
		// Resolved path must exist on disk under the test root.
		_, err := os.Stat(local)
		require.NoError(t, err, "translated path %s must stat (from src %s)", local, s.SourcePath)
	}
}

func TestInfer_Ambiguous_Refused(t *testing.T) {
	root := t.TempDir()
	// Each sample maps to a unique fake source path; together they don't
	// converge on a single prefix rule (mixed prefixes).
	writeFile(t, filepath.Join(root, "a.mkv"), "a")
	writeFile(t, filepath.Join(root, "b.mkv"), "b")
	idx, err := mapper.BuildIndex("test", root, 1000)
	require.NoError(t, err)

	// 2 samples — below minSamplesMatched.
	samples := []mapper.Sample{
		{SourcePath: "/weird/a.mkv", Size: 1},
		{SourcePath: "/weirder/b.mkv", Size: 1},
	}
	res := mapper.Infer(idx, samples)
	require.False(t, res.Accepted, "below sample threshold must refuse")
}

func TestResolver_Translate_LongestPrefixWins(t *testing.T) {
	r := mapper.NewResolver()
	r.Set([]mapper.VolumeRules{{
		VolumeName: "v",
		Rules: []mapper.Rule{
			{From: "/files/", To: "/share/files/", Origin: mapper.OriginConfig},
			{From: "/files/tv/", To: "/share/tv/", Origin: mapper.OriginConfig},
		},
	}})

	local, rule, ok := r.Translate("/files/tv/Foo.mkv")
	require.True(t, ok)
	require.Equal(t, "/share/tv/Foo.mkv", local)
	require.Equal(t, "/files/tv/", rule.From)

	local, _, ok = r.Translate("/files/movies/Bar.mkv")
	require.True(t, ok)
	require.Equal(t, "/share/files/movies/Bar.mkv", local)
}

func TestResolver_Translate_NoRule(t *testing.T) {
	r := mapper.NewResolver()
	r.Set([]mapper.VolumeRules{{
		VolumeName: "v",
		Rules:      []mapper.Rule{{From: "/files/", To: "/share/files/", Origin: mapper.OriginConfig}},
	}})
	_, _, ok := r.Translate("/elsewhere/foo")
	require.False(t, ok)
}

func TestResolver_Translate_Identity(t *testing.T) {
	r := mapper.NewResolver()
	r.Set([]mapper.VolumeRules{{
		VolumeName: "v",
		Rules:      []mapper.Rule{{From: "", To: "", Origin: mapper.OriginIdentity}},
	}})
	local, rule, ok := r.Translate("/anywhere/foo")
	require.True(t, ok)
	require.Equal(t, "/anywhere/foo", local)
	require.Equal(t, mapper.OriginIdentity, rule.Origin)
}
