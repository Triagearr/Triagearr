// Package triagearr defines the cross-cutting interfaces and value types
// shared between Triagearr components. Concrete implementations live in
// internal/store, internal/clients/*, internal/scorer, internal/actor.
package triagearr

import (
	"context"
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

// DeleteOpts controls the behaviour of a delete call.
// Filled out in M5 — kept here so M1 stubs can compile against the final signature.
type DeleteOpts struct {
	DeleteFiles        bool
	AddImportExclusion bool
}

// ArrInstance is the contract every *arr client implements.
type ArrInstance interface {
	Name() string
	Type() ArrType
	Poll() bool
	Act() bool
	ListMedia(ctx context.Context) ([]MediaItem, error)
	DeleteMedia(ctx context.Context, id MediaID, opts DeleteOpts) error
	HealthCheck(ctx context.Context) error
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
	Ratio        float64
	Uploaded     int64
	Seeders      int
	Leechers     int
	State        TorrentState
	LastActivity time.Time
}

// TorrentFile is one file within a torrent. Used by the mapper (M2) to resolve inodes.
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

// DiskUsage is a point-in-time observation of a watched volume.
type DiskUsage struct {
	VolumeName  string
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
	Delete(ctx context.Context, h Hash, opts DeleteOpts) error
}
