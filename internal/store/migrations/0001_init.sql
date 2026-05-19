-- M1 schema: observation-only.
-- Tables for scoring/decision/action (snapshots_daily, scores, actions, audit_log)
-- arrive in their own milestones — do not pre-create them here.

CREATE TABLE arr_instances (
    name              TEXT NOT NULL,
    type              TEXT NOT NULL,
    url               TEXT NOT NULL,
    healthy           INTEGER NOT NULL DEFAULT 0,
    last_health_check TIMESTAMP,
    last_error        TEXT,
    PRIMARY KEY (name, type)
);

CREATE TABLE torrents (
    hash       TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    category   TEXT NOT NULL DEFAULT '',
    save_path  TEXT NOT NULL DEFAULT '',
    size       INTEGER NOT NULL DEFAULT 0,
    added_on   TIMESTAMP NOT NULL,
    last_seen  TIMESTAMP NOT NULL
);
CREATE INDEX idx_torrents_last_seen ON torrents(last_seen);

CREATE TABLE snapshots_raw (
    torrent_hash  TEXT NOT NULL,
    ts            TIMESTAMP NOT NULL,
    ratio         REAL NOT NULL,
    uploaded      INTEGER NOT NULL,
    seeders       INTEGER NOT NULL,
    leechers      INTEGER NOT NULL,
    state         TEXT NOT NULL,
    last_activity TIMESTAMP NOT NULL,
    PRIMARY KEY (torrent_hash, ts)
) WITHOUT ROWID;

CREATE TABLE media (
    id        INTEGER NOT NULL,
    arr_name  TEXT NOT NULL,
    arr_type  TEXT NOT NULL,
    title     TEXT NOT NULL,
    path      TEXT NOT NULL DEFAULT '',
    size      INTEGER NOT NULL DEFAULT 0,
    tags      TEXT NOT NULL DEFAULT '',
    last_seen TIMESTAMP NOT NULL,
    PRIMARY KEY (id, arr_name, arr_type)
);
CREATE INDEX idx_media_last_seen ON media(last_seen);

CREATE TABLE disk_pressure (
    volume_name  TEXT NOT NULL,
    ts           TIMESTAMP NOT NULL,
    path         TEXT NOT NULL,
    total_bytes  INTEGER NOT NULL,
    used_bytes   INTEGER NOT NULL,
    free_bytes   INTEGER NOT NULL,
    free_percent REAL NOT NULL,
    PRIMARY KEY (volume_name, ts)
) WITHOUT ROWID;
