package mapper

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// IndexEntry pairs a basename and size — the disambiguation key used by the
// inference. Two files with the same basename but different sizes are not
// considered the same file.
type IndexEntry struct {
	Path string
	Size int64
}

// VolumeIndex is the basename+size → local-paths lookup table for one volume.
type VolumeIndex struct {
	VolumeName string
	Root       string
	ByName     map[string][]IndexEntry
	Entries    int
}

// BuildIndex walks root once and indexes every regular file by basename.
// maxEntries bounds the in-memory cost (default 200k per volume in the daemon).
// The walk is read-only and ignores symlinks (`os.Lstat`-style; the stdlib
// fs.WalkDir already follows the entry types it discovers).
func BuildIndex(volumeName, root string, maxEntries int) (*VolumeIndex, error) {
	idx := &VolumeIndex{
		VolumeName: volumeName,
		Root:       root,
		ByName:     map[string][]IndexEntry{},
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Permission-denied on a subtree is non-fatal — log-and-skip behaviour
			// belongs to the caller, who has slog. The walker itself just continues.
			if errors.Is(err, fs.ErrPermission) {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if maxEntries > 0 && idx.Entries >= maxEntries {
			return fs.SkipAll
		}
		info, statErr := d.Info()
		if statErr != nil {
			// Race with concurrent removal — skip the entry, the next boot will retry.
			return nil //nolint:nilerr // intentional: missing-after-walk is benign here
		}
		base := filepath.Base(path)
		idx.ByName[base] = append(idx.ByName[base], IndexEntry{Path: path, Size: info.Size()})
		idx.Entries++
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return nil, fmt.Errorf("indexing %s: %w", root, err)
	}
	return idx, nil
}

// Sample is one source-system path to evaluate against the local index.
type Sample struct {
	SourcePath string
	Size       int64 // -1 if unknown
}

// minSamplesMatched and acceptanceThreshold encode ADR-0010's acceptance gate.
const (
	minSamplesMatched   = 5
	acceptanceThreshold = 0.80
)

// CandidateRule is an exported view of one (from, to) prefix vote, surfaced
// when inference fails so the caller can log the full distribution.
type CandidateRule struct {
	From  string
	To    string
	Votes int
}

// InferenceResult captures the outcome of one volume's inference.
type InferenceResult struct {
	VolumeName     string
	Rule           Rule
	Accepted       bool
	SamplesTotal   int
	SamplesMatched int
	Candidates     []CandidateRule // populated when refused; logged for diagnosis
}

// Infer derives the prefix-substitution rule for one volume by matching the
// supplied samples against the local index. Returns Accepted=false (with the
// candidate distribution) when no single rule reaches the acceptance gate;
// the caller is expected to refuse-to-start in that case.
//
// Identity match (source paths stat as-is) is published as a no-op rule with
// Origin=OriginIdentity so the user sees explicit confirmation in the logs.
func Infer(idx *VolumeIndex, samples []Sample) InferenceResult {
	res := InferenceResult{VolumeName: idx.VolumeName, SamplesTotal: len(samples)}
	votes := map[[2]string]int{}
	for _, s := range samples {
		base := filepath.Base(s.SourcePath)
		hits := idx.ByName[base]
		var match *IndexEntry
		for i, h := range hits {
			if s.Size >= 0 && h.Size != s.Size {
				continue
			}
			if match != nil {
				// ambiguous on (basename, size) within the index — skip the sample
				match = nil
				break
			}
			match = &hits[i]
		}
		if match == nil {
			continue
		}
		from, to, ok := derivePrefix(s.SourcePath, match.Path)
		if !ok {
			continue
		}
		votes[[2]string{from, to}]++
		res.SamplesMatched++
	}

	if res.SamplesMatched == 0 {
		return res
	}

	cands := make([]CandidateRule, 0, len(votes))
	for k, v := range votes {
		cands = append(cands, CandidateRule{From: k[0], To: k[1], Votes: v})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Votes != cands[j].Votes {
			return cands[i].Votes > cands[j].Votes
		}
		return len(cands[i].From) > len(cands[j].From)
	})
	res.Candidates = cands

	best := cands[0]
	if res.SamplesMatched < minSamplesMatched {
		return res
	}
	if float64(best.Votes)/float64(res.SamplesMatched) < acceptanceThreshold {
		return res
	}
	origin := OriginInferred
	if best.From == "" && best.To == "" {
		origin = OriginIdentity
	}
	res.Rule = Rule{
		From:          best.From,
		To:            best.To,
		Origin:        origin,
		SampleMatches: best.Votes,
		SampleTotal:   res.SamplesMatched,
	}
	res.Accepted = true
	return res
}

// derivePrefix returns the (from, to) prefix substitution that maps source to
// local, by finding the longest common trailing path segments and treating
// the remainder as the prefix substitution. Returns ok=false when the paths
// share no common suffix at all (impossible match).
func derivePrefix(source, local string) (from, to string, ok bool) {
	srcParts := splitSegments(source)
	locParts := splitSegments(local)
	i := len(srcParts) - 1
	j := len(locParts) - 1
	for i >= 0 && j >= 0 && srcParts[i] == locParts[j] {
		i--
		j--
	}
	// At this point srcParts[0..i] is the source prefix to strip (after re-joining),
	// and locParts[0..j] is the local prefix to prepend.
	srcPrefix := joinSegments(srcParts[:i+1], strings.HasPrefix(source, "/"))
	locPrefix := joinSegments(locParts[:j+1], strings.HasPrefix(local, "/"))
	// Ensure both prefixes are slash-terminated so substring replacement is clean.
	if srcPrefix != "" && !strings.HasSuffix(srcPrefix, "/") {
		srcPrefix += "/"
	}
	if locPrefix != "" && !strings.HasSuffix(locPrefix, "/") {
		locPrefix += "/"
	}
	// If both prefixes are equal, this is an identity match.
	if srcPrefix == locPrefix {
		return "", "", true
	}
	// We require source to actually have srcPrefix at the head, otherwise this
	// rule wouldn't help translate the original path.
	if srcPrefix != "" && !strings.HasPrefix(source, srcPrefix) {
		return "", "", false
	}
	return srcPrefix, locPrefix, true
}

func splitSegments(p string) []string {
	clean := filepath.Clean(p)
	if clean == "/" {
		return nil
	}
	clean = strings.TrimPrefix(clean, "/")
	return strings.Split(clean, "/")
}

func joinSegments(parts []string, leadingSlash bool) string {
	if len(parts) == 0 {
		if leadingSlash {
			return "/"
		}
		return ""
	}
	joined := strings.Join(parts, "/")
	if leadingSlash {
		return "/" + joined
	}
	return joined
}
