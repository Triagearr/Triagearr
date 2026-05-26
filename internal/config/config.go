// Package config loads and validates Triagearr's YAML configuration.
//
// The schema mirrors config.example.yml at the repository root. Only the
// sections used by M1 (observation-only) are typed; sections owned by later
// milestones are tolerated but not interpreted.
package config

import "time"

// Mode is the master safety switch.
type Mode string

// Mode values accepted in the `mode` field.
const (
	ModeDryRun Mode = "dry-run"
	ModeLive   Mode = "live"
)

// Config is the typed top-level configuration. Fields not relevant to M1 are
// intentionally omitted — they will be added when their owning milestone needs them.
type Config struct {
	Mode    Mode          `koanf:"mode"`
	HTTP    HTTPConfig    `koanf:"http"`
	Storage StorageConfig `koanf:"storage"`
	Arrs    ArrsConfig    `koanf:"arrs"`
	// TorrentClients holds the configured torrent client instance per kind.
	// Exactly one instance per kind is supported — the kind is the identity
	// (ADR-0025). Only qbittorrent has a backend today; the other kinds are
	// scaffolded for future backends.
	TorrentClients TorrentClientsConfig `koanf:"torrent_clients"`
	// Volume is the single watched filesystem mount. Triagearr watches exactly
	// one volume — the TRaSH shared data root (ADR-0024).
	Volume  VolumeConfig  `koanf:"volume"`
	Polling PollingConfig `koanf:"polling"`
	Scoring ScoringConfig `koanf:"scoring"`
	Action  ActionConfig  `koanf:"action"`
	// Notifications configures post-action operator notifications. Only
	// disk-pressure runs that reach the Actor are notified (ADR-0021).
	Notifications NotificationsConfig `koanf:"notifications"`
}

// NotificationsConfig groups the configured notification providers. Each
// provider is independently toggled; an empty/disabled section is a no-op.
type NotificationsConfig struct {
	Telegram TelegramConfig `koanf:"telegram"`
}

// TelegramConfig configures the Telegram Bot API notifier. BotToken and
// ChatID are required when Enabled.
type TelegramConfig struct {
	Enabled  bool   `koanf:"enabled"`
	BotToken string `koanf:"bot_token"`
	ChatID   string `koanf:"chat_id"`
}

// ActionConfig tunes the M5 Actor's destructive pipeline.
type ActionConfig struct {
	// MaxDeletionsPerRun caps how many candidates a single run executes;
	// zero means unlimited. Default 10.
	MaxDeletionsPerRun int `koanf:"max_deletions_per_run"`
	// InterActionDelay sleeps between consecutive whole-torrent qBit deletes,
	// giving the filesystem and *arr a moment to settle before the next call.
	// Default 2s.
	InterActionDelay time.Duration `koanf:"inter_action_delay"`
	// AddImportExclusion forwards to *arr — when true, deleted releases are
	// added to the import exclusion list so *arr won't re-grab them.
	// Default false (operator can opt in per their workflow).
	AddImportExclusion bool `koanf:"add_import_exclusion"`
}

// ScoringConfig drives the M3 scorer. Weights come from SCORING.md;
// HnR window weight is hard-coded (-10000) per the safety contract.
//
// Per-tracker policy and the rare-content default were moved out of YAML
// into the database (ADR-0026): see tracker_policies + scoring_defaults.
type ScoringConfig struct {
	HnRWindowDays    int            `koanf:"hnr_window_days"`
	TrackerDeadGrace time.Duration  `koanf:"tracker_dead_grace"`
	Weights          ScoringWeights `koanf:"weights"`
}

// ScoringWeights holds the tunable per-factor weights.
type ScoringWeights struct {
	RatioObligationMet float64 `koanf:"ratio_obligation_met"`
	UploadVelocityInv  float64 `koanf:"upload_velocity_inv"`
	AgeDays            float64 `koanf:"age_days"`
	SeedersLowGuard    float64 `koanf:"seeders_low_guard"`
	SwarmHealthBonus   float64 `koanf:"swarm_health_bonus"`
	TrackerDeadBonus   float64 `koanf:"tracker_dead_bonus"`
}

// HTTPConfig configures the HTTP API. The API key is NOT a config field:
// it lives in `${data_dir}/api_key` (Sonarr-style), auto-generated on first
// boot and persisted with 0600 perms. Setting `bind: ""` disables the API
// entirely.
//
// Authentication is opt-in via the dashboard (ADR-0019): when no user is
// registered the API is open and the operator relies on whatever upstream
// protection they configure (TinyAuth/Authelia/private network). When the
// operator enables auth via Settings, the middleware starts requiring a
// session cookie OR an X-API-Key on every /api/v1/* request.
type HTTPConfig struct {
	Bind        string           `koanf:"bind"`
	CORSOrigins []string         `koanf:"cors_origins"`
	RateLimits  RateLimitsConfig `koanf:"rate_limits"`
}

// RateLimitsConfig caps the request rate on sensitive endpoints, per-IP,
// per minute. Defaults are deliberately permissive — homelab UX matters
// more than DoS resistance behind a reverse proxy. Conventions:
//   - unset (0)  → permissive default (60 for runs, 30 for auth)
//   - positive N → cap at N requests/minute/IP with burst N
//   - negative   → disable the limiter entirely
type RateLimitsConfig struct {
	RunsPerMinute int `koanf:"runs_per_minute"`
	AuthPerMinute int `koanf:"auth_per_minute"`
}

// StorageConfig groups storage-related settings.
type StorageConfig struct {
	SQLitePath string          `koanf:"sqlite_path"`
	Retention  RetentionConfig `koanf:"retention"`
	Vacuum     VacuumConfig    `koanf:"vacuum"`
}

// ArrsConfig holds the configured *arr instance per type.
// Exactly one instance per type is supported — the type is the identity.
type ArrsConfig struct {
	Sonarr     ArrInstanceConfig `koanf:"sonarr"`
	Radarr     ArrInstanceConfig `koanf:"radarr"`
	Lidarr     ArrInstanceConfig `koanf:"lidarr"`
	Readarr    ArrInstanceConfig `koanf:"readarr"`
	WhisparrV2 ArrInstanceConfig `koanf:"whisparr_v2"`
	WhisparrV3 ArrInstanceConfig `koanf:"whisparr_v3"`
}

// EachPtr iterates over every *arr slot, yielding (kind label, pointer).
// Use this for any code that needs to read or mutate every slot uniformly —
// it is the single source of truth for the supported *arr kinds.
func (c *ArrsConfig) EachPtr(fn func(label string, inst *ArrInstanceConfig)) {
	fn("sonarr", &c.Sonarr)
	fn("radarr", &c.Radarr)
	fn("lidarr", &c.Lidarr)
	fn("readarr", &c.Readarr)
	fn("whisparr_v2", &c.WhisparrV2)
	fn("whisparr_v3", &c.WhisparrV3)
}

// SetByKind assigns inst to the slot named by kind. Returns false when kind
// is unknown — callers typically log and skip.
func (c *ArrsConfig) SetByKind(kind string, inst ArrInstanceConfig) bool {
	switch kind {
	case "sonarr":
		c.Sonarr = inst
	case "radarr":
		c.Radarr = inst
	case "lidarr":
		c.Lidarr = inst
	case "readarr":
		c.Readarr = inst
	case "whisparr_v2":
		c.WhisparrV2 = inst
	case "whisparr_v3":
		c.WhisparrV3 = inst
	default:
		return false
	}
	return true
}

// ArrInstanceConfig captures one arrs.<type> block. The type key in the YAML
// is the identity — there is no separate name field.
type ArrInstanceConfig struct {
	Enabled        bool          `koanf:"enabled"`
	URL            string        `koanf:"url"`
	APIKey         string        `koanf:"api_key"`
	Poll           bool          `koanf:"poll"`
	Act            bool          `koanf:"act"`
	TagsExclude    []string      `koanf:"tags_exclude"`
	CategoriesOnly []string      `koanf:"categories_only"`
	Timeout        time.Duration `koanf:"timeout"`
}

// TorrentClientsConfig holds the configured torrent client instance per kind.
// Exactly one instance per kind is supported — the kind is the identity.
// Only qbittorrent has a backend implementation today.
type TorrentClientsConfig struct {
	Qbittorrent  TorrentClientInstanceConfig `koanf:"qbittorrent"`
	Transmission TorrentClientInstanceConfig `koanf:"transmission"` // scaffold, no backend yet
	Deluge       TorrentClientInstanceConfig `koanf:"deluge"`       // scaffold, no backend yet
	Rtorrent     TorrentClientInstanceConfig `koanf:"rtorrent"`     // scaffold, no backend yet
}

// EachPtr iterates over every torrent-client slot, yielding (kind label,
// pointer). Single source of truth for the supported torrent-client kinds.
func (c *TorrentClientsConfig) EachPtr(fn func(label string, inst *TorrentClientInstanceConfig)) {
	fn("qbittorrent", &c.Qbittorrent)
	fn("transmission", &c.Transmission)
	fn("deluge", &c.Deluge)
	fn("rtorrent", &c.Rtorrent)
}

// HasBackend reports whether a kind has a working backend today. Only
// qbittorrent does; transmission/deluge/rtorrent are scaffolded.
func (TorrentClientsConfig) HasBackend(kind string) bool {
	return kind == "qbittorrent"
}

// SetByKind assigns inst to the slot named by kind. Returns false when kind
// is unknown.
func (c *TorrentClientsConfig) SetByKind(kind string, inst TorrentClientInstanceConfig) bool {
	switch kind {
	case "qbittorrent":
		c.Qbittorrent = inst
	case "transmission":
		c.Transmission = inst
	case "deluge":
		c.Deluge = inst
	case "rtorrent":
		c.Rtorrent = inst
	default:
		return false
	}
	return true
}

// TorrentClientInstanceConfig captures one torrent_clients.<kind> block. The
// kind key in the YAML is the identity — there is no separate name field.
type TorrentClientInstanceConfig struct {
	Enabled         bool          `koanf:"enabled"`
	URL             string        `koanf:"url"`
	Username        string        `koanf:"username"`
	Password        string        `koanf:"password"`
	CategoryExclude []string      `koanf:"category_exclude"`
	TagsExclude     []string      `koanf:"tags_exclude"`
	DeleteWithFiles bool          `koanf:"delete_with_files"`
	Timeout         time.Duration `koanf:"timeout"`
}

// VolumeConfig describes the single watched filesystem mount — the TRaSH
// shared data root (ADR-0023, ADR-0024).
//
// Name is an optional display label (defaults to "media"). Path is the
// container mount path, statfs'd for disk usage and the prefix every qBit
// save_path sits under.
//
// When Source is non-empty, the disk poller fetches usage from that URL
// instead of calling statfs(Path). This is a dev/test hook — production
// configs leave it empty so statfs is used.
type VolumeConfig struct {
	Name         string             `koanf:"name"`
	Path         string             `koanf:"path"`
	Source       string             `koanf:"source"`
	DiskPressure DiskPressureConfig `koanf:"disk_pressure"`
}

// DiskPressureConfig is partially populated in M1 (only `enabled` is used by the
// disk poller). The thresholds become live in M4.
type DiskPressureConfig struct {
	Enabled              bool    `koanf:"enabled"`
	ThresholdFreePercent float64 `koanf:"threshold_free_percent"`
	TargetFreePercent    float64 `koanf:"target_free_percent"`
	MaxRunSizeGB         int     `koanf:"max_run_size_gb"`
}

// PollingConfig groups the poll intervals for the various pollers.
type PollingConfig struct {
	TorrentClientInterval time.Duration `koanf:"torrent_client_interval"`
	ArrInterval           time.Duration `koanf:"arr_interval"`
	ArrFileMinInterval    time.Duration `koanf:"arr_file_min_interval"`
	TrackerInterval       time.Duration `koanf:"tracker_interval"`
	DiskInterval          time.Duration `koanf:"disk_interval"`
	MaintainerrInterval   time.Duration `koanf:"maintainerr_interval"`
	DownsampleCron        string        `koanf:"downsample_cron"`
}

// RetentionConfig bounds the lifetime of historical observations.
type RetentionConfig struct {
	SnapshotsRaw   time.Duration `koanf:"snapshots_raw"`
	SnapshotsDaily time.Duration `koanf:"snapshots_daily"`
	// Torrents is the grace before a torrent absent from qBit gets pruned
	// (cascade on snapshots + trackers). 0 disables. Default 7d.
	Torrents time.Duration `koanf:"torrents"`
}

// VacuumConfig gates the post-retention SQLite VACUUM.
type VacuumConfig struct {
	Enabled      bool  `koanf:"enabled"`
	MinReclaimMB int64 `koanf:"min_reclaim_mb"`
}

// Defaults applied when a field is left zero by the user.
const (
	defaultBind                  = "127.0.0.1:9494"
	defaultRunsPerMinute         = 60
	defaultAuthPerMinute         = 30
	defaultSQLitePath            = "/config/triagearr.db"
	defaultArrTimeout            = 30 * time.Second
	defaultTorrentClientTimeout  = 30 * time.Second
	defaultTorrentClientInterval = 30 * time.Minute
	defaultArrInterval           = time.Hour
	defaultArrFileMinInterval    = 200 * time.Millisecond // ≈ 5 req/s
	defaultTrackerInterval       = 6 * time.Hour
	defaultDiskInterval          = 5 * time.Minute
	defaultDownsampleCron        = "0 3 * * *"
	defaultRetentionRaw          = 7 * 24 * time.Hour
	defaultRetentionDaily        = 365 * 24 * time.Hour
	defaultRetentionTorrents     = 7 * 24 * time.Hour
	defaultVacuumMinReclaimMB    = int64(50)
	defaultHnRWindowDays         = 14
	defaultTrackerDeadGrace      = 7 * 24 * time.Hour
	defaultWeightRatioObl        = 50.0
	defaultWeightVelocityInv     = 30.0
	defaultWeightAgeDays         = 0.1
	defaultWeightSeedersLow      = -1000.0
	defaultWeightSwarmBonus      = 5.0
	defaultWeightTrackerDead     = 40.0

	defaultMaxDeletionsPerRun = 10
	defaultInterActionDelay   = 2 * time.Second

	defaultVolumeName = "media"
)
