package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// Load reads the YAML config at path, expands ${VAR} and ${VAR:-default}
// references against the process environment, applies defaults, and validates
// the result. Fails fast on any error — never returns a partially-valid config.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is supplied by the operator (CLI flag / env var); reading it is the whole point of this loader.
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

// envVarPattern matches ${VAR}, ${VAR:-default}. Does not support nested
// expansion or escaping — by design, this is a config file, not a shell.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expandEnv replaces ${VAR} (required) and ${VAR:-default} tokens in the
// non-comment portion of each line. Returns an error if any required ${VAR}
// is missing from the environment. YAML `#` comments are preserved verbatim,
// so `${VAR}` written in a comment is not interpreted.
func expandEnv(in []byte) ([]byte, error) {
	var missing []string
	expand := func(s string) string {
		return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
			groups := envVarPattern.FindStringSubmatch(match)
			name := groups[1]
			hasDefault := strings.Contains(match, ":-")
			val, ok := os.LookupEnv(name)
			if ok {
				return val
			}
			if hasDefault {
				return groups[2]
			}
			missing = append(missing, name)
			return match
		})
	}

	lines := strings.Split(string(in), "\n")
	for i, line := range lines {
		code, comment := splitYAMLComment(line)
		lines[i] = expand(code) + comment
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("required env vars not set: %s", strings.Join(missing, ", "))
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// splitYAMLComment returns (code, comment) where comment starts at the first
// `#` that's either at column 0 or preceded by whitespace and lies outside a
// quoted string. Good enough for our config — we don't need a full YAML parser.
func splitYAMLComment(line string) (string, string) {
	var (
		inSingle, inDouble bool
		prev               byte = ' '
	)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble && (i == 0 || prev == ' ' || prev == '\t'):
			return line[:i], line[i:]
		}
		prev = c
	}
	return line, ""
}

func applyDefaults(c *Config) {
	if c.Mode == "" {
		c.Mode = ModeDryRun
	}
	if c.HTTP.Bind == "" {
		c.HTTP.Bind = defaultBind
	}
	if c.Storage.SQLitePath == "" {
		c.Storage.SQLitePath = defaultSQLitePath
	}
	if c.Polling.QbitInterval == 0 {
		c.Polling.QbitInterval = defaultQbitInterval
	}
	if c.Polling.ArrInterval == 0 {
		c.Polling.ArrInterval = defaultArrInterval
	}
	if c.Polling.DiskInterval == 0 {
		c.Polling.DiskInterval = defaultDiskInterval
	}
	if c.Polling.ArrFileMinInterval == 0 {
		c.Polling.ArrFileMinInterval = defaultArrFileMinInterval
	}
	if c.Polling.TrackerInterval == 0 {
		c.Polling.TrackerInterval = defaultTrackerInterval
	}
	if c.Polling.DownsampleCron == "" {
		c.Polling.DownsampleCron = defaultDownsampleCron
	}
	if c.Storage.Retention.SnapshotsRaw == 0 {
		c.Storage.Retention.SnapshotsRaw = defaultRetentionRaw
	}
	if c.Storage.Retention.SnapshotsDaily == 0 {
		c.Storage.Retention.SnapshotsDaily = defaultRetentionDaily
	}
	if c.Storage.Retention.Torrents == 0 {
		c.Storage.Retention.Torrents = defaultRetentionTorrents
	}
	if c.Storage.Vacuum.MinReclaimMB == 0 {
		c.Storage.Vacuum.MinReclaimMB = defaultVacuumMinReclaimMB
	}
	if c.Qbit.Timeout == 0 {
		c.Qbit.Timeout = defaultQbitTimeout
	}
	applyScoringDefaults(&c.Scoring)
	applyArrDefaults(c.Arrs.Sonarr)
	applyArrDefaults(c.Arrs.Radarr)
	applyArrDefaults(c.Arrs.Lidarr)
	applyArrDefaults(c.Arrs.Readarr)
	applyArrDefaults(c.Arrs.WhisparrV2)
	applyArrDefaults(c.Arrs.WhisparrV3)
}

func applyScoringDefaults(s *ScoringConfig) {
	if s.Interval == 0 {
		s.Interval = defaultScoringInterval
	}
	if s.HnRWindowDays == 0 {
		s.HnRWindowDays = defaultHnRWindowDays
	}
	if s.RareContentThreshold == 0 {
		s.RareContentThreshold = defaultRareThreshold
	}
	if s.TrackerDeadGrace == 0 {
		s.TrackerDeadGrace = defaultTrackerDeadGrace
	}
	if s.Weights.RatioObligationMet == 0 {
		s.Weights.RatioObligationMet = defaultWeightRatioObl
	}
	if s.Weights.UploadVelocityInv == 0 {
		s.Weights.UploadVelocityInv = defaultWeightVelocityInv
	}
	if s.Weights.AgeDays == 0 {
		s.Weights.AgeDays = defaultWeightAgeDays
	}
	if s.Weights.SeedersLowGuard == 0 {
		s.Weights.SeedersLowGuard = defaultWeightSeedersLow
	}
	if s.Weights.SwarmHealthBonus == 0 {
		s.Weights.SwarmHealthBonus = defaultWeightSwarmBonus
	}
	if s.Weights.TrackerDeadBonus == 0 {
		s.Weights.TrackerDeadBonus = defaultWeightTrackerDead
	}
}

func applyArrDefaults(insts []ArrInstanceConfig) {
	for i := range insts {
		if insts[i].Timeout == 0 {
			insts[i].Timeout = defaultArrTimeout
		}
	}
}

// Validate runs schema-level checks that aren't expressible at unmarshal time.
func Validate(c *Config) error {
	if c.Mode != ModeDryRun && c.Mode != ModeLive {
		return fmt.Errorf("mode: must be %q or %q, got %q", ModeDryRun, ModeLive, c.Mode)
	}

	for _, group := range []struct {
		label string
		insts []ArrInstanceConfig
	}{
		{"sonarr", c.Arrs.Sonarr},
		{"radarr", c.Arrs.Radarr},
		{"lidarr", c.Arrs.Lidarr},
		{"readarr", c.Arrs.Readarr},
		{"whisparr_v2", c.Arrs.WhisparrV2},
		{"whisparr_v3", c.Arrs.WhisparrV3},
	} {
		seen := map[string]bool{}
		for i, inst := range group.insts {
			if inst.Name == "" {
				return fmt.Errorf("arrs.%s[%d].name: required", group.label, i)
			}
			if seen[inst.Name] {
				return fmt.Errorf("arrs.%s: duplicate name %q", group.label, inst.Name)
			}
			seen[inst.Name] = true
			if !inst.Enabled {
				continue
			}
			if _, err := url.Parse(inst.URL); err != nil || inst.URL == "" {
				return fmt.Errorf("arrs.%s[%s].url: invalid URL %q", group.label, inst.Name, inst.URL)
			}
			if inst.APIKey == "" {
				return fmt.Errorf("arrs.%s[%s].api_key: required when enabled", group.label, inst.Name)
			}
		}
	}

	for i, v := range c.Volumes {
		if v.Name == "" {
			return fmt.Errorf("volumes[%d].name: required", i)
		}
		if v.Path == "" {
			return fmt.Errorf("volumes[%s].path: required", v.Name)
		}
	}

	if c.Qbit.Enabled {
		if _, err := url.Parse(c.Qbit.URL); err != nil || c.Qbit.URL == "" {
			return fmt.Errorf("qbit.url: invalid URL %q", c.Qbit.URL)
		}
	}

	// http.bind binding to a non-loopback address requires an api_key. Defense
	// in depth: the daemon exposes destructive triggers (M4+), so a public bind
	// without auth is a footgun, not a configuration choice.
	if c.HTTP.Bind != "" && c.HTTP.APIKey == "" && !isLoopbackBind(c.HTTP.Bind) {
		return fmt.Errorf("http.bind: %q is not loopback — http.api_key is required", c.HTTP.Bind)
	}

	for i, v := range c.Volumes {
		dp := v.DiskPressure
		if !dp.Enabled {
			continue
		}
		if dp.ThresholdFreePercent < 0 || dp.ThresholdFreePercent > 100 {
			return fmt.Errorf("volumes[%d=%s].disk_pressure.threshold_free_percent: must be in [0,100], got %v", i, v.Name, dp.ThresholdFreePercent)
		}
		if dp.TargetFreePercent < 0 || dp.TargetFreePercent > 100 {
			return fmt.Errorf("volumes[%d=%s].disk_pressure.target_free_percent: must be in [0,100], got %v", i, v.Name, dp.TargetFreePercent)
		}
		if dp.ThresholdFreePercent > 0 && dp.TargetFreePercent <= dp.ThresholdFreePercent {
			return fmt.Errorf("volumes[%d=%s].disk_pressure: target_free_percent (%v) must be greater than threshold_free_percent (%v)", i, v.Name, dp.TargetFreePercent, dp.ThresholdFreePercent)
		}
	}

	return nil
}

// isLoopbackBind reports whether bind targets a loopback address. Accepts
// hostnames "localhost" and any 127.x.x.x or [::1] form. A bare ":port"
// means "all interfaces" — not loopback.
func isLoopbackBind(bind string) bool {
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// AnyArrEnabledForPolling returns true if at least one *arr is enabled+poll.
// Used by the daemon to decide whether to start the arr poller goroutine.
func (c *Config) AnyArrEnabledForPolling() bool {
	for _, group := range [][]ArrInstanceConfig{
		c.Arrs.Sonarr, c.Arrs.Radarr, c.Arrs.Lidarr,
		c.Arrs.Readarr, c.Arrs.WhisparrV2, c.Arrs.WhisparrV3,
	} {
		for _, inst := range group {
			if inst.Enabled && inst.Poll {
				return true
			}
		}
	}
	return false
}
