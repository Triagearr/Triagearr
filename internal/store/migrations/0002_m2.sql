-- Tracker capture (ADR-0009), per-file *arr capture, daily downsampled snapshots.

ALTER TABLE torrents ADD COLUMN completion_on TIMESTAMP;

CREATE TABLE torrent_trackers (
    torrent_hash TEXT NOT NULL,
    tracker_url  TEXT NOT NULL,
    tracker_host TEXT NOT NULL,
    status       INTEGER NOT NULL,
    last_msg     TEXT NOT NULL DEFAULT '',
    last_checked TIMESTAMP NOT NULL,
    PRIMARY KEY (torrent_hash, tracker_url)
) WITHOUT ROWID;
CREATE INDEX idx_torrent_trackers_host ON torrent_trackers(tracker_host);

CREATE TABLE media_files (
    arr_name  TEXT NOT NULL,
    arr_type  TEXT NOT NULL,
    file_id   INTEGER NOT NULL,
    media_id  INTEGER NOT NULL,
    path      TEXT NOT NULL,
    size      INTEGER NOT NULL DEFAULT 0,
    last_seen TIMESTAMP NOT NULL,
    PRIMARY KEY (arr_name, arr_type, file_id)
);
CREATE INDEX idx_media_files_media ON media_files(arr_name, arr_type, media_id);

CREATE TABLE snapshots_daily (
    torrent_hash TEXT NOT NULL,
    day          DATE NOT NULL,
    ratio_avg    REAL NOT NULL,
    ratio_min    REAL NOT NULL,
    ratio_max    REAL NOT NULL,
    seeders_avg  REAL NOT NULL,
    seeders_min  INTEGER NOT NULL,
    seeders_max  INTEGER NOT NULL,
    samples      INTEGER NOT NULL,
    PRIMARY KEY (torrent_hash, day)
) WITHOUT ROWID;
