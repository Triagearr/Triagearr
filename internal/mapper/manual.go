package mapper

import (
	"fmt"
	"os"
)

// ManualRule is one entry from `volumes[*].path_remap` in the YAML config.
type ManualRule struct {
	From string
	To   string
}

// ValidateManual stat()s every `to:` of the manual rules and returns a
// canonicalised Rule list ready to be fed to [Resolver.Set]. A missing `to:`
// directory is a hard error — refuse-to-start is the caller's job.
func ValidateManual(rules []ManualRule) ([]Rule, error) {
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.To == "" {
			return nil, fmt.Errorf("manual path_remap missing `to:` (from=%q)", r.From)
		}
		info, err := os.Stat(r.To)
		if err != nil {
			return nil, fmt.Errorf("manual path_remap to=%q: %w", r.To, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("manual path_remap to=%q is not a directory", r.To)
		}
		out = append(out, Rule{From: normalizePrefix(r.From), To: normalizePrefix(r.To), Origin: OriginConfig})
	}
	return out, nil
}

// normalizePrefix ensures the prefix ends with `/` so substring replacement
// doesn't accidentally truncate a path component (e.g., `/files` would also
// match `/filesfoo/...` without the trailing slash).
func normalizePrefix(p string) string {
	if p == "" {
		return ""
	}
	if p[len(p)-1] != '/' {
		return p + "/"
	}
	return p
}
