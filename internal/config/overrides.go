package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// Override is one runtime-editable settings entry. The key is a dotted koanf
// path (e.g. "scoring.weights.ratio_obligation_met", "polling.torrent_client_interval")
// and ValueJSON is its replacement value encoded as JSON.
//
// Overrides are applied on top of the YAML baseline by LoadWithOverrides,
// after env-var expansion but before defaults and validation. The whitelist
// of editable keys is enforced at the HTTP layer, not here.
type Override struct {
	Key       string
	ValueJSON string
}

// LoadWithOverrides reads the YAML config at path (same flow as Load) and
// applies the given overrides on top before defaults/validation. Use this
// instead of Load when the daemon should honour runtime edits from
// settings_overrides.
//
// Validation runs on the merged result — an invalid override is rejected
// the same way a hand-edited YAML mistake would be.
func LoadWithOverrides(path string, overrides []Override) (*Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is supplied by the operator; reading it is the whole point of this loader.
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}
	expanded, err := expandEnv(raw)
	if err != nil {
		return nil, fmt.Errorf("expanding env vars in %q: %w", path, err)
	}

	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider(expanded), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("parsing yaml %q: %w", path, err)
	}

	if k.Exists("http.auth") {
		slog.Warn("http.auth is obsolete and will be ignored — authentication is now opt-in via the dashboard (ADR-0019); remove the field from your config to silence this warning")
	}

	if err := applyOverrides(k, overrides); err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := k.UnmarshalWithConf("", cfg, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}
	applyDefaults(cfg)
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyOverrides decodes each override JSON value and sets it at its key.
// Sorted by key first so the order is deterministic even when the caller
// passes a map-iterated slice (overrides on nested paths are independent so
// order doesn't usually matter, but determinism helps debugging).
func applyOverrides(k *koanf.Koanf, overrides []Override) error {
	sorted := make([]Override, len(overrides))
	copy(sorted, overrides)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	for _, o := range sorted {
		var v any
		if err := json.Unmarshal([]byte(o.ValueJSON), &v); err != nil {
			return fmt.Errorf("override %q: invalid JSON: %w", o.Key, err)
		}
		if err := k.Set(o.Key, v); err != nil {
			return fmt.Errorf("override %q: %w", o.Key, err)
		}
		slog.Debug("config override applied", "key", o.Key)
	}
	return nil
}

// IsEditableKey reports whether the given dotted path may be overridden via
// the Settings API. The whitelist matches the operator-facing choices made
// in plan: scoring, disk_pressure thresholds, polling intervals, and the
// dry-run/live mode (ADR-0029 — toggling it from the dashboard is the whole
// point; the change is audit-trailed in settings_overrides rather than git,
// and stays safe via the dry-run default, per-instance act gate, and HnR veto).
// Anything not on this list must round-trip through a YAML edit (audit-trailed
// via git) — notably per-instance act flags, URLs/credentials, and the
// bind/sqlite_path infrastructure knobs.
func IsEditableKey(key string) bool {
	for prefix := range editablePrefixes {
		if key == prefix || strings.HasPrefix(key, prefix+".") {
			return true
		}
	}
	return false
}

// editablePrefixes is the whitelist of allowed override key prefixes. A key
// is editable if it equals one of these or starts with one followed by ".".
//
// Kept as a map for O(1) lookup and so adding a new section is a one-liner.
var editablePrefixes = map[string]struct{}{
	"mode":                 {}, // dry-run/live toggle, UI-managed (ADR-0029); Validate() enforces the enum
	"scoring":              {},
	"polling":              {},
	"volume.disk_pressure": {}, // thresholds only; volume.path/name/source are boot-critical (preflight reads them) and must stay YAML-only
	"notifications":        {}, // includes provider credentials — operator opted into UI-managed secrets
}

// EditableKeys returns the whitelist as a sorted slice — useful for the
// HTTP layer to expose "what's editable" and for tests.
func EditableKeys() []string {
	out := make([]string, 0, len(editablePrefixes))
	for k := range editablePrefixes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
