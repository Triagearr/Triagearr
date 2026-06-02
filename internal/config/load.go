package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"

	"github.com/Triagearr/Triagearr/internal/notify"
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

	if k.Exists("http.auth") {
		slog.Warn("http.auth is obsolete and will be ignored — authentication is now opt-in via the dashboard (ADR-0019); remove the field from your config to silence this warning")
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
	if c.HTTP.RateLimits.RunsPerMinute == 0 {
		c.HTTP.RateLimits.RunsPerMinute = defaultRunsPerMinute
	}
	if c.HTTP.RateLimits.AuthPerMinute == 0 {
		c.HTTP.RateLimits.AuthPerMinute = defaultAuthPerMinute
	}
	if c.Storage.SQLitePath == "" {
		c.Storage.SQLitePath = defaultSQLitePath
	}
	if c.Polling.TorrentClientInterval == 0 {
		c.Polling.TorrentClientInterval = defaultTorrentClientInterval
	}
	if c.Polling.ArrInterval == 0 {
		c.Polling.ArrInterval = defaultArrInterval
	}
	if c.Polling.DiskInterval == 0 {
		c.Polling.DiskInterval = defaultDiskInterval
	}
	if c.Polling.HealthInterval == 0 {
		c.Polling.HealthInterval = defaultHealthInterval
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
	c.TorrentClients.EachPtr(func(_ string, inst *TorrentClientInstanceConfig) {
		applyTorrentClientDefaults(inst)
	})
	if c.Volume.Name == "" {
		c.Volume.Name = defaultVolumeName
	}
	applyScoringDefaults(&c.Scoring)
	applyActionDefaults(&c.Action)
	applyNotificationDefaults(&c.Notifications)
	c.Arrs.EachPtr(func(_ string, inst *ArrInstanceConfig) {
		applyArrDefaults(inst)
	})
}

func applyScoringDefaults(s *ScoringConfig) {
	if s.HnRWindowDays == 0 {
		s.HnRWindowDays = defaultHnRWindowDays
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

func applyActionDefaults(a *ActionConfig) {
	if a.MaxDeletionsPerRun == 0 {
		a.MaxDeletionsPerRun = defaultMaxDeletionsPerRun
	}
	if a.InterActionDelay == 0 {
		a.InterActionDelay = defaultInterActionDelay
	}
}

func applyNotificationDefaults(n *NotificationsConfig) {
	switch {
	case n.TargetUnreachable.ReminderInterval == 0:
		n.TargetUnreachable.ReminderInterval = defaultTargetUnreachableReminder
	case n.TargetUnreachable.ReminderInterval < minTargetUnreachableReminder:
		n.TargetUnreachable.ReminderInterval = minTargetUnreachableReminder
	}
}

// validateNotifications checks each enabled provider's required fields and
// every provider's routing block (severity floor + mute kinds). Required-field
// checks are strict so a half-configured provider fails fast at boot/PUT.
func validateNotifications(n *NotificationsConfig) error {
	type providerCheck struct {
		name    string
		enabled bool
		routing ProviderRouting
		// required maps a field label to its value; an enabled provider with an
		// empty required value is rejected.
		required map[string]string
	}
	checks := []providerCheck{
		{"telegram", n.Telegram.Enabled, n.Telegram.ProviderRouting, map[string]string{"bot_token": n.Telegram.BotToken, "chat_id": n.Telegram.ChatID}},
		{"discord", n.Discord.Enabled, n.Discord.ProviderRouting, map[string]string{"webhook_url": n.Discord.WebhookURL}},
		{"ntfy", n.Ntfy.Enabled, n.Ntfy.ProviderRouting, map[string]string{"topic": n.Ntfy.Topic}},
		{"email", n.Email.Enabled, n.Email.ProviderRouting, map[string]string{"host": n.Email.Host, "from": n.Email.From}},
		{"slack", n.Slack.Enabled, n.Slack.ProviderRouting, map[string]string{"webhook_url": n.Slack.WebhookURL}},
		{"webhook", n.Webhook.Enabled, n.Webhook.ProviderRouting, map[string]string{"url": n.Webhook.URL}},
	}
	for _, c := range checks {
		if c.enabled {
			for field, val := range c.required {
				if val == "" {
					return fmt.Errorf("notifications.%s.%s: required when enabled", c.name, field)
				}
			}
			if c.name == "email" && len(n.Email.To) == 0 {
				return fmt.Errorf("notifications.email.to: at least one recipient required when enabled")
			}
		}
		if err := validateRouting(c.name, c.routing); err != nil {
			return err
		}
	}
	return nil
}

// validateRouting rejects an unknown severity floor or mute kind so a typo is
// caught at config time rather than silently dropping events.
func validateRouting(provider string, r ProviderRouting) error {
	if _, err := notify.ParseSeverity(r.MinSeverity); err != nil {
		return fmt.Errorf("notifications.%s.min_severity: %w", provider, err)
	}
	for _, k := range r.Mute {
		if !notify.EventKind(k).Known() {
			return fmt.Errorf("notifications.%s.mute: unknown event kind %q", provider, k)
		}
	}
	return nil
}

func applyArrDefaults(inst *ArrInstanceConfig) {
	if inst.Timeout == 0 {
		inst.Timeout = defaultArrTimeout
	}
}

func applyTorrentClientDefaults(inst *TorrentClientInstanceConfig) {
	if inst.Timeout == 0 {
		inst.Timeout = defaultTorrentClientTimeout
	}
}

// Validate runs schema-level checks that aren't expressible at unmarshal time.
func Validate(c *Config) error {
	if c.Mode != ModeDryRun && c.Mode != ModeLive {
		return fmt.Errorf("mode: must be %q or %q, got %q", ModeDryRun, ModeLive, c.Mode)
	}

	var arrErr error
	c.Arrs.EachPtr(func(label string, inst *ArrInstanceConfig) {
		if arrErr != nil || !inst.Enabled {
			return
		}
		if _, err := url.Parse(inst.URL); err != nil || inst.URL == "" {
			arrErr = fmt.Errorf("arrs.%s.url: invalid URL %q", label, inst.URL)
			return
		}
		if inst.APIKey == "" {
			arrErr = fmt.Errorf("arrs.%s.api_key: required when enabled", label)
		}
	})
	if arrErr != nil {
		return arrErr
	}

	if c.Volume.Path == "" {
		return fmt.Errorf("volume.path: required")
	}

	// Kinds without a backend refuse to start when enabled — the daemon would
	// otherwise silently ignore them.
	var tcErr error
	c.TorrentClients.EachPtr(func(label string, inst *TorrentClientInstanceConfig) {
		if tcErr != nil || !inst.Enabled {
			return
		}
		if !c.TorrentClients.HasBackend(label) {
			tcErr = fmt.Errorf("torrent_clients.%s: kind is scaffolded but has no backend yet", label)
			return
		}
		if _, err := url.Parse(inst.URL); err != nil || inst.URL == "" {
			tcErr = fmt.Errorf("torrent_clients.%s.url: invalid URL %q", label, inst.URL)
		}
	})
	if tcErr != nil {
		return tcErr
	}

	if err := validateNotifications(&c.Notifications); err != nil {
		return err
	}

	if dp := c.Volume.DiskPressure; dp.Enabled {
		if dp.ThresholdFreePercent < 0 || dp.ThresholdFreePercent > 100 {
			return fmt.Errorf("volume.disk_pressure.threshold_free_percent: must be in [0,100], got %v", dp.ThresholdFreePercent)
		}
		if dp.TargetFreePercent < 0 || dp.TargetFreePercent > 100 {
			return fmt.Errorf("volume.disk_pressure.target_free_percent: must be in [0,100], got %v", dp.TargetFreePercent)
		}
		if dp.ThresholdFreePercent > 0 && dp.TargetFreePercent <= dp.ThresholdFreePercent {
			return fmt.Errorf("volume.disk_pressure: target_free_percent (%v) must be greater than threshold_free_percent (%v)", dp.TargetFreePercent, dp.ThresholdFreePercent)
		}
	}

	return nil
}

// AnyArrEnabledForPolling returns true if at least one *arr is enabled+poll.
// Used by the daemon to decide whether to start the arr poller goroutine.
func (c *Config) AnyArrEnabledForPolling() bool {
	var any bool
	c.Arrs.EachPtr(func(_ string, inst *ArrInstanceConfig) {
		if inst.Enabled && inst.Poll {
			any = true
		}
	})
	return any
}
