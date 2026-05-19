// Package mapper resolves the same on-disk inode from two different
// starting points: the path qBit reports for a torrent file, and the path
// *arr reports for a media file. Both source systems often live in a
// different mount namespace than Triagearr; the [Resolver] translates the
// reported strings into paths Triagearr can stat() before any deletion is
// considered.
//
// See ADR-0010 for the inference design and the boot procedure.
package mapper

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// RuleOrigin marks where a path_remap rule came from.
type RuleOrigin string

// RuleOrigin values.
const (
	OriginConfig   RuleOrigin = "config"
	OriginInferred RuleOrigin = "inferred"
	OriginIdentity RuleOrigin = "identity"
)

// Rule is one prefix substitution: every path that starts with From has its
// prefix replaced by To. An empty From means identity (no translation).
type Rule struct {
	From          string
	To            string
	Origin        RuleOrigin
	SampleMatches int // populated for inferred rules: how many samples voted for this rule
	SampleTotal   int // populated for inferred rules: total samples evaluated
}

// VolumeRules is the active set of rules for one configured volume.
type VolumeRules struct {
	VolumeName string
	Rules      []Rule
}

// Resolver applies the active path_remap rules. It is safe for concurrent use.
type Resolver struct {
	mu      sync.RWMutex
	volumes []VolumeRules
}

// NewResolver returns an empty resolver. Until Set is called, Translate fails
// closed (no translation, ok=false). Inference / manual override populates it
// at boot via [Resolver.Set].
func NewResolver() *Resolver { return &Resolver{} }

// Set replaces the active rule set. Called once by the boot inference and
// again on SIGHUP-driven config reload (M4).
func (r *Resolver) Set(volumes []VolumeRules) {
	// Sort rules within each volume by descending From length so the most
	// specific prefix wins on multi-rule volumes (per-category split).
	for i := range volumes {
		sort.SliceStable(volumes[i].Rules, func(a, b int) bool {
			return len(volumes[i].Rules[a].From) > len(volumes[i].Rules[b].From)
		})
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.volumes = volumes
}

// VolumeRules returns a copy of the active rules for debugging / inspect.
func (r *Resolver) VolumeRules() []VolumeRules {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]VolumeRules, len(r.volumes))
	for i, v := range r.volumes {
		rules := make([]Rule, len(v.Rules))
		copy(rules, v.Rules)
		out[i] = VolumeRules{VolumeName: v.VolumeName, Rules: rules}
	}
	return out
}

// Translate applies the longest matching rule across every volume.
func (r *Resolver) Translate(src string) (string, Rule, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var best Rule
	bestLen := -1
	for _, v := range r.volumes {
		for _, rule := range v.Rules {
			if rule.From == "" {
				if bestLen < 0 {
					best = rule
					bestLen = 0
				}
				continue
			}
			if strings.HasPrefix(src, rule.From) && len(rule.From) > bestLen {
				best = rule
				bestLen = len(rule.From)
			}
		}
	}
	if bestLen < 0 {
		return "", Rule{}, false
	}
	if best.From == "" {
		return src, best, true
	}
	return best.To + strings.TrimPrefix(src, best.From), best, true
}

// FileRef is one file from a qBit torrent, after translation and stat.
type FileRef struct {
	QbitPath  string
	LocalPath string
	Inode     uint64
	Nlink     uint64
	StatErr   string
	Rule      Rule
}

// StatFile translates and stat()s a qBit-reported file path. Used by both the
// per-file CLI (`inspect mapping`) and (in M5) the actor's T3.5 re-stat.
func (r *Resolver) StatFile(qbitPath string) FileRef {
	local, rule, ok := r.Translate(qbitPath)
	if !ok {
		return FileRef{QbitPath: qbitPath, StatErr: "no path_remap rule covers this path"}
	}
	ino, nlink, err := statInode(local)
	ref := FileRef{QbitPath: qbitPath, LocalPath: local, Rule: rule, Inode: ino, Nlink: nlink}
	if err != nil {
		ref.StatErr = err.Error()
	}
	return ref
}

// Describe returns a human-readable summary of a rule for log/CLI output.
func Describe(rule Rule) string {
	switch rule.Origin {
	case OriginIdentity:
		return "identity"
	case OriginConfig:
		return fmt.Sprintf("config %s → %s", rule.From, rule.To)
	case OriginInferred:
		return fmt.Sprintf("inferred %s → %s (%d/%d samples)", rule.From, rule.To, rule.SampleMatches, rule.SampleTotal)
	default:
		return fmt.Sprintf("%s → %s", rule.From, rule.To)
	}
}
