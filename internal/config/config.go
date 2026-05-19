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
	Mode    Mode           `koanf:"mode"`
	HTTP    HTTPConfig     `koanf:"http"`
	Storage StorageConfig  `koanf:"storage"`
	Arrs    ArrsConfig     `koanf:"arrs"`
	Qbit    QbitConfig     `koanf:"qbit"`
	Volumes []VolumeConfig `koanf:"volumes"`
	Polling PollingConfig  `koanf:"polling"`
}

// HTTPConfig is unused by M1 but kept here so unknown-key warnings stay quiet.
type HTTPConfig struct {
	Bind        string   `koanf:"bind"`
	APIKey      string   `koanf:"api_key"`
	CORSOrigins []string `koanf:"cors_origins"`
}

// StorageConfig groups storage-related settings.
type StorageConfig struct {
	SQLitePath string          `koanf:"sqlite_path"`
	Retention  RetentionConfig `koanf:"retention"`
	Vacuum     VacuumConfig    `koanf:"vacuum"`
}

// ArrsConfig holds the configured *arr instances per type.
type ArrsConfig struct {
	Sonarr     []ArrInstanceConfig `koanf:"sonarr"`
	Radarr     []ArrInstanceConfig `koanf:"radarr"`
	Lidarr     []ArrInstanceConfig `koanf:"lidarr"`
	Readarr    []ArrInstanceConfig `koanf:"readarr"`
	WhisparrV2 []ArrInstanceConfig `koanf:"whisparr_v2"`
	WhisparrV3 []ArrInstanceConfig `koanf:"whisparr_v3"`
}

// ArrInstanceConfig captures one entry from arrs.<type>[].
type ArrInstanceConfig struct {
	Name           string        `koanf:"name"`
	Enabled        bool          `koanf:"enabled"`
	URL            string        `koanf:"url"`
	APIKey         string        `koanf:"api_key"`
	Poll           bool          `koanf:"poll"`
	Act            bool          `koanf:"act"`
	TagsExclude    []string      `koanf:"tags_exclude"`
	CategoriesOnly []string      `koanf:"categories_only"`
	Timeout        time.Duration `koanf:"timeout"`
}

// QbitConfig configures the qBittorrent client.
type QbitConfig struct {
	Enabled         bool          `koanf:"enabled"`
	URL             string        `koanf:"url"`
	Username        string        `koanf:"username"`
	Password        string        `koanf:"password"`
	CategoryExclude []string      `koanf:"category_exclude"`
	TagsExclude     []string      `koanf:"tags_exclude"`
	DeleteWithFiles bool          `koanf:"delete_with_files"`
	Timeout         time.Duration `koanf:"timeout"`
}

// VolumeConfig describes a watched filesystem mount.
type VolumeConfig struct {
	Name         string             `koanf:"name"`
	Path         string             `koanf:"path"`
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
	QbitInterval        time.Duration `koanf:"qbit_interval"`
	ArrInterval         time.Duration `koanf:"arr_interval"`
	ArrFileMinInterval  time.Duration `koanf:"arr_file_min_interval"`
	TrackerInterval     time.Duration `koanf:"tracker_interval"`
	DiskInterval        time.Duration `koanf:"disk_interval"`
	MaintainerrInterval time.Duration `koanf:"maintainerr_interval"`
	DownsampleCron      string        `koanf:"downsample_cron"`
}

// RetentionConfig bounds the lifetime of historical observations.
type RetentionConfig struct {
	SnapshotsRaw   time.Duration `koanf:"snapshots_raw"`
	SnapshotsDaily time.Duration `koanf:"snapshots_daily"`
}

// VacuumConfig gates the post-retention SQLite VACUUM.
type VacuumConfig struct {
	Enabled      bool  `koanf:"enabled"`
	MinReclaimMB int64 `koanf:"min_reclaim_mb"`
}

// Defaults applied when a field is left zero by the user.
const (
	defaultBind               = ":9494"
	defaultSQLitePath         = "/data/triagearr.db"
	defaultArrTimeout         = 30 * time.Second
	defaultQbitTimeout        = 30 * time.Second
	defaultQbitInterval       = 30 * time.Minute
	defaultArrInterval        = time.Hour
	defaultArrFileMinInterval = 200 * time.Millisecond // ≈ 5 req/s
	defaultTrackerInterval    = 6 * time.Hour
	defaultDiskInterval       = 5 * time.Minute
	defaultDownsampleCron     = "0 3 * * *"
	defaultRetentionRaw       = 7 * 24 * time.Hour
	defaultRetentionDaily     = 365 * 24 * time.Hour
	defaultVacuumMinReclaimMB = int64(50)
)
