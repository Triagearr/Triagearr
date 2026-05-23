// Package triagearr defines the cross-cutting interfaces and value types
// shared between Triagearr components. Concrete implementations live in
// internal/store, internal/clients/*, internal/scorer, internal/actor.
package triagearr

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ArrType identifies a flavour of *arr application.
type ArrType string

// Supported *arr application types.
const (
	ArrTypeSonarr     ArrType = "sonarr"
	ArrTypeRadarr     ArrType = "radarr"
	ArrTypeLidarr     ArrType = "lidarr"
	ArrTypeReadarr    ArrType = "readarr"
	ArrTypeWhisparrV2 ArrType = "whisparr_v2"
	ArrTypeWhisparrV3 ArrType = "whisparr_v3"
)

// MediaID is a stable identifier for a media item within a single *arr instance.
type MediaID int64

// MediaItem is the *arr's view of a piece of media (series, movie, album, ...).
type MediaItem struct {
	ID       MediaID
	ArrName  string
	ArrType  ArrType
	Title    string
	Path     string
	Size     int64
	Tags     []string
	LastSeen time.Time
}

// MediaFile is one on-disk file owned by an *arr media item. Sonarr maps these
// to `episodeFile.id`, Radarr to `movieFile.id`. The actor (M5) uses the
// file_id to issue granular DELETEs without touching siblings of the same
// series/movie.
type MediaFile struct {
	ArrName string
	ArrType ArrType
	FileID  int64
	MediaID MediaID
	Path    string
	Size    int64
}

// DeleteOpts controls the behaviour of a delete call.
type DeleteOpts struct {
	DeleteFiles        bool
	AddImportExclusion bool
}

// ErrTransient marks an upstream failure (5xx, timeout, connection reset)
// that the Actor may retry. Clients wrap their concrete error with this
// sentinel via errors.Join so callers can detect it with errors.Is.
var ErrTransient = errors.New("transient upstream failure")

// ArrInstance is the contract every *arr client implements.
//
// Deletion is per-file (episodeFile/movieFile) and lives on the optional
// FileDeleter interface — *arr clients that can act type-assert into it,
// stubs do not. M5's Actor consumes FileDeleter, not ArrInstance, for the
// destructive step.
type ArrInstance interface {
	Name() string
	Type() ArrType
	Poll() bool
	Act() bool
	ListMedia(ctx context.Context) ([]MediaItem, error)
	HealthCheck(ctx context.Context) error
}

// FileDeleter is the optional capability for *arr clients that can delete a
// single library file (one episodeFile.id / movieFile.id). The Actor (M5)
// fans the per-torrent decision out into N per-file DELETE calls.
type FileDeleter interface {
	DeleteMediaFile(ctx context.Context, fileID int64, opts DeleteOpts) error
}

// Hash is a qBittorrent torrent hash (info hash v1, lowercase hex).
type Hash string

// TorrentState mirrors qBit's state strings ("uploading", "stalledUP", ...).
type TorrentState string

// Torrent is the current state of a torrent as reported by qBit.
type Torrent struct {
	Hash         Hash
	Name         string
	Category     string
	SavePath     string
	Size         int64
	AddedOn      time.Time
	CompletionOn time.Time
	Ratio        float64
	Uploaded     int64
	Seeders      int
	Leechers     int
	State        TorrentState
	LastActivity time.Time
	// Private mirrors qBit's `private` flag. The scorer (M3) gates several
	// factors on this regime (ratio-obligation vs swarm-only); see SCORING.md.
	Private bool
	// Tags is qBit's comma-separated tag string, preserved verbatim.
	Tags string
}

// TrackerStatus mirrors qBit's tracker.status enum (0..4).
type TrackerStatus int

// TrackerStatus values from qBit's `/api/v2/torrents/trackers`.
const (
	TrackerDisabled     TrackerStatus = 0
	TrackerNotContacted TrackerStatus = 1
	TrackerWorking      TrackerStatus = 2
	TrackerUpdating     TrackerStatus = 3
	TrackerNotWorking   TrackerStatus = 4
)

// String returns the qBit-documented label for the enum value.
func (s TrackerStatus) String() string {
	switch s {
	case TrackerDisabled:
		return "disabled"
	case TrackerNotContacted:
		return "not_contacted"
	case TrackerWorking:
		return "working"
	case TrackerUpdating:
		return "updating"
	case TrackerNotWorking:
		return "not_working"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// TrackerInfo is one tracker attached to a torrent. The host is parsed from
// the URL to match `scoring.per_tracker` policy keys; the scorer (M3) reads
// the parsed host, not the raw URL.
type TrackerInfo struct {
	URL    string
	Host   string
	Status TrackerStatus
	Msg    string
}

// TorrentFile is one file within a torrent.
type TorrentFile struct {
	Name     string
	Size     int64
	Progress float64
}

// Snapshot is a point-in-time observation of a torrent. Persisted into snapshots_raw.
type Snapshot struct {
	Hash         Hash
	Timestamp    time.Time
	Ratio        float64
	Uploaded     int64
	Seeders      int
	Leechers     int
	State        TorrentState
	LastActivity time.Time
}

// DiskUsage is a point-in-time observation of the watched volume.
type DiskUsage struct {
	Path        string
	Timestamp   time.Time
	TotalBytes  uint64
	UsedBytes   uint64
	FreeBytes   uint64
	FreePercent float64
}

// QbitClient abstracts the qBittorrent download client. V1 supports one instance.
type QbitClient interface {
	ListTorrents(ctx context.Context) ([]Torrent, error)
	TorrentFiles(ctx context.Context, h Hash) ([]TorrentFile, error)
	ListTrackers(ctx context.Context, h Hash) ([]TrackerInfo, error)
	Delete(ctx context.Context, h Hash, opts DeleteOpts) error
}

// FileLister is the optional capability for *arr clients that expose per-file
// metadata (Sonarr episode files, Radarr movie files). Stub clients omit it.
// The arr poller (M2) type-asserts on this interface to fan out file calls.
type FileLister interface {
	ListMediaFiles(ctx context.Context, mediaID MediaID) ([]MediaFile, error)
}

// ImportRecord is one *arr import-history entry (ADR-0012). It pairs a qBit
// torrent (DownloadID, the V1 info-hash lowercased) with the *arr file_id that
// was created when *arr hard-linked the torrent into its library.
type ImportRecord struct {
	HistoryID    int64  // *arr-side history.id, used as the delta cursor
	FileID       int64  // *arr file_id (episodeFile.id / movieFile.id) — DELETE target for M5
	DownloadID   Hash   // qBit info-hash, lowercased
	DroppedPath  string // source path as reported by *arr at import time
	ImportedPath string // destination path inside the *arr library
	Size         int64
	ImportedAt   time.Time
}

// ImportLister is the optional capability for *arr clients that expose import
// history. The arr poller type-asserts on this interface to keep arr_imports
// in sync.
type ImportLister interface {
	ListImports(ctx context.Context, sinceHistoryID int64) ([]ImportRecord, error)
}

// RunTrigger identifies what caused a Decider run to fire.
type RunTrigger string

// Supported RunTrigger values.
const (
	RunTriggerDiskPressure RunTrigger = "disk_pressure"
	RunTriggerHTTP         RunTrigger = "http"
	RunTriggerCLI          RunTrigger = "cli"
)

// RunStopReason explains why the Decider stopped accumulating candidates.
type RunStopReason string

// Supported RunStopReason values.
const (
	StopTargetReached    RunStopReason = "target_reached"
	StopSizeCap          RunStopReason = "size_cap"
	StopNoMoreCandidates RunStopReason = "no_more_candidates"
)

// Run is the persisted record of one Decider invocation.
type Run struct {
	ID                  int64
	TriggeredBy         RunTrigger
	TriggeredAt         time.Time
	Mode                string
	FreePctAtFire       float64
	TargetFreePct       float64
	EstimatedFreedBytes int64
	StopReason          RunStopReason
	Status              string
}

// RunItem is one candidate in a Run's ordered plan.
type RunItem struct {
	RunID          int64
	Rank           int
	TorrentHash    Hash
	Score          float64
	SizeBytes      int64
	WouldFreeBytes int64
}

// Link is one resolved (*arr file ↔ qBit torrent) edge, joining what *arr
// recorded at import time with the current media_files snapshot. Returned by
// the linker (ADR-0012) and consumed by the M5 actor as the per-file DELETE
// target list.
type Link struct {
	ArrName      string
	ArrType      ArrType
	FileID       int64
	DownloadID   Hash
	DroppedPath  string // *arr-side source path at import (diagnostic only)
	ImportedPath string // *arr-side library path at import (diagnostic only)
	LivePath     string // current path from media_files (M5 actor source of truth)
	Size         int64
}

// ActionStatus is the lifecycle state of one Actor action (one candidate
// torrent processed end-to-end through the *arr → qBit pipeline).
type ActionStatus string

// Supported ActionStatus values.
const (
	ActionPending        ActionStatus = "pending"
	ActionRunning        ActionStatus = "running"
	ActionSucceeded      ActionStatus = "succeeded"
	ActionAbortedArrFail ActionStatus = "aborted_arr_fail"
	ActionFailedQbit     ActionStatus = "failed_qbit"
	// ActionSkippedCrossSeed: T3.5 saw nlink>1 on at least one file after the
	// *arr fan-out, so deleting the qBit torrent would harm a cross-seed peer
	// without freeing disk. *arr deletes are NOT rolled back (see
	// HARDLINK_TOPOLOGY.md case 4): nlink stays ≥1 thanks to the surviving peer.
	ActionSkippedCrossSeed ActionStatus = "skipped_cross_seed"
)

// Action is the persisted record of one destructive operation attempted by
// the Actor on a single candidate (one row per run_items entry consumed).
type Action struct {
	ID          int64
	RunID       int64
	Rank        int
	TorrentHash Hash
	StartedAt   time.Time
	FinishedAt  time.Time // zero when not yet finished
	Status      ActionStatus
	FreedBytes  int64
}

// AuditStep names one API call class the Actor performs.
type AuditStep string

// Supported AuditStep values.
const (
	AuditStepArrDelete  AuditStep = "arr_delete"
	AuditStepNlinkCheck AuditStep = "nlink_check"
	AuditStepQbitDelete AuditStep = "qbit_delete"
)

// AuditOutcome reports the result of one audited API call.
type AuditOutcome string

// Supported AuditOutcome values.
const (
	AuditOutcomeOK           AuditOutcome = "ok"
	AuditOutcomeFailed       AuditOutcome = "failed"
	AuditOutcomeSkipped      AuditOutcome = "skipped"
	AuditOutcomeNotAttempted AuditOutcome = "not_attempted"
)

// AuditEntry is one row of the per-file audit trail. For an *arr fan-out we
// expect N rows (one per arr_file_id); the qBit step contributes a single row.
type AuditEntry struct {
	ID        int64
	ActionID  int64
	Timestamp time.Time
	Step      AuditStep
	ArrName   string // empty for non-arr steps
	ArrFileID int64  // 0 for non-arr steps
	Outcome   AuditOutcome
	Detail    string // truncated, redacted free-form context
}
