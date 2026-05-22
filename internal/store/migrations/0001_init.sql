-- Consolidated schema baseline (alpha).
--
-- This single file replaces the former 0001..0012 chain. Triagearr is alpha:
-- the only deployed database is recreated from scratch alongside this squash,
-- so no incremental upgrade path is preserved. New schema work appends a fresh
-- 0002_*.sql; this file is never edited in place once a release ships.
--
-- Per-decision rationale lives in docs/adr/. Notable corrections folded in
-- versus the old chain: media PK reordered to (arr_name, arr_type, id) so
-- lookups by *arr instance use the PK; an index on actions.started_at for the
-- dashboard timeline; a torrents.category index for the dashboard filter; the
-- unused scores(computed_at) index dropped.

-- *arr instance health observations. Distinct from arr_connections, which owns
-- the connection *config* (ADR-0022); this table is just last-known health.
CREATE TABLE arr_instances (
    name              TEXT NOT NULL,
    type              TEXT NOT NULL,
    url               TEXT NOT NULL,
    healthy           INTEGER NOT NULL DEFAULT 0,
    last_health_check TIMESTAMP,
    last_error        TEXT,
    PRIMARY KEY (name, type)
);

-- private defaults to 1: a row is assumed private until the next qBit poll
-- proves otherwise, so the scorer never treats a private torrent as swarm-only.
CREATE TABLE torrents (
    hash          TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    category      TEXT NOT NULL DEFAULT '',
    save_path     TEXT NOT NULL DEFAULT '',
    size          INTEGER NOT NULL DEFAULT 0,
    added_on      TIMESTAMP NOT NULL,
    completion_on TIMESTAMP,
    private       INTEGER NOT NULL DEFAULT 1,
    tags          TEXT NOT NULL DEFAULT '',
    last_seen     TIMESTAMP NOT NULL
);
CREATE INDEX idx_torrents_last_seen ON torrents(last_seen);
CREATE INDEX idx_torrents_category  ON torrents(category);

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

-- PK leads with (arr_name, arr_type) so counts and joins scoped to one *arr
-- instance seek the PK instead of scanning.
CREATE TABLE media (
    id        INTEGER NOT NULL,
    arr_name  TEXT NOT NULL,
    arr_type  TEXT NOT NULL,
    title     TEXT NOT NULL,
    path      TEXT NOT NULL DEFAULT '',
    size      INTEGER NOT NULL DEFAULT 0,
    tags      TEXT NOT NULL DEFAULT '',
    last_seen TIMESTAMP NOT NULL,
    PRIMARY KEY (arr_name, arr_type, id)
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

-- first_seen_dead records when a tracker first reported status=4, so Factor 7
-- measures "sustained dead" rather than "last polled". qBit exposes no
-- status-changed-at field, so the transition is observed in ReplaceTrackers.
CREATE TABLE torrent_trackers (
    torrent_hash    TEXT NOT NULL,
    tracker_url     TEXT NOT NULL,
    tracker_host    TEXT NOT NULL,
    status          INTEGER NOT NULL,
    last_msg        TEXT NOT NULL DEFAULT '',
    last_checked    TIMESTAMP NOT NULL,
    first_seen_dead TIMESTAMP,
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

-- uploaded_max lets Factor 2 honour the 30-day velocity window after
-- snapshots_raw has expired. A zero is treated as "no data" by the scorer.
CREATE TABLE snapshots_daily (
    torrent_hash TEXT NOT NULL,
    day          DATE NOT NULL,
    ratio_avg    REAL NOT NULL,
    ratio_min    REAL NOT NULL,
    ratio_max    REAL NOT NULL,
    seeders_avg  REAL NOT NULL,
    seeders_min  INTEGER NOT NULL,
    seeders_max  INTEGER NOT NULL,
    uploaded_max INTEGER NOT NULL DEFAULT 0,
    samples      INTEGER NOT NULL,
    PRIMARY KEY (torrent_hash, day)
) WITHOUT ROWID;

-- API-only hardlink map (ADR-0012). download_id is the lowercased qBit
-- info-hash that *arr's import history recorded for this file.
CREATE TABLE arr_imports (
    arr_name      TEXT    NOT NULL,
    arr_type      TEXT    NOT NULL,
    file_id       INTEGER NOT NULL,
    download_id   TEXT    NOT NULL,
    dropped_path  TEXT    NOT NULL DEFAULT '',
    imported_path TEXT    NOT NULL DEFAULT '',
    size          INTEGER NOT NULL DEFAULT 0,
    history_id    INTEGER NOT NULL,
    imported_at   TIMESTAMP NOT NULL,
    PRIMARY KEY (arr_name, arr_type, file_id)
);
CREATE INDEX idx_arr_imports_download ON arr_imports(download_id);
CREATE INDEX idx_arr_imports_history  ON arr_imports(arr_name, arr_type, history_id);

-- The partial index serves the Decider's hot path: eligible candidates ranked
-- by score. factors_json is the breakdown read whole by the explain path.
CREATE TABLE scores (
    torrent_hash      TEXT NOT NULL PRIMARY KEY,
    score             REAL NOT NULL,
    private           INTEGER NOT NULL,
    any_tracker_alive INTEGER NOT NULL,
    excluded          INTEGER NOT NULL DEFAULT 0,
    exclusion_reasons TEXT NOT NULL DEFAULT '',
    factors_json      TEXT NOT NULL,
    computed_at       TIMESTAMP NOT NULL
) WITHOUT ROWID;
CREATE INDEX idx_scores_eligible ON scores(score DESC) WHERE excluded = 0;

CREATE TABLE runs (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    triggered_by          TEXT NOT NULL,
    triggered_at          TIMESTAMP NOT NULL,
    mode                  TEXT NOT NULL,
    volume_name           TEXT,
    free_pct_at_fire      REAL,
    target_free_pct       REAL,
    estimated_freed_bytes INTEGER NOT NULL DEFAULT 0,
    stop_reason           TEXT NOT NULL,
    status                TEXT NOT NULL DEFAULT 'completed'
);
CREATE INDEX runs_triggered_at_idx ON runs(triggered_at DESC);

CREATE TABLE run_items (
    run_id           INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    rank             INTEGER NOT NULL,
    torrent_hash     TEXT NOT NULL,
    score            REAL NOT NULL,
    size_bytes       INTEGER NOT NULL,
    would_free_bytes INTEGER NOT NULL,
    PRIMARY KEY (run_id, rank)
);
CREATE INDEX run_items_hash_idx ON run_items(torrent_hash);

-- One row per candidate the actor attempts. started_at is indexed for the
-- dashboard's global action timeline (ORDER BY started_at DESC).
CREATE TABLE actions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id       INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    rank         INTEGER NOT NULL,
    torrent_hash TEXT NOT NULL,
    started_at   TIMESTAMP NOT NULL,
    finished_at  TIMESTAMP,
    status       TEXT NOT NULL,
        -- pending | running | succeeded | aborted_arr_fail | failed_qbit
    freed_bytes  INTEGER NOT NULL DEFAULT 0,
    UNIQUE(run_id, rank)
);
CREATE INDEX actions_hash_idx       ON actions(torrent_hash);
CREATE INDEX actions_status_idx     ON actions(status);
CREATE INDEX actions_started_at_idx ON actions(started_at DESC);

-- One row per API call — per-file granularity on the *arr side so a partial
-- season-pack outcome is reconstructible from the DB alone.
CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    action_id   INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    ts          TIMESTAMP NOT NULL,
    step        TEXT NOT NULL,   -- arr_delete | qbit_delete
    arr_name    TEXT,
    arr_file_id INTEGER,
    outcome     TEXT NOT NULL,   -- ok | failed | skipped | not_attempted
    detail      TEXT
);
CREATE INDEX audit_log_action_idx ON audit_log(action_id, ts);

-- Built-in auth (opt-in via UI). Auth is OFF while auth_users is empty. The DB
-- stores only sha256(token); the token itself lives only in the browser cookie.
CREATE TABLE auth_users (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    username            TEXT NOT NULL UNIQUE,
    password_hash       TEXT NOT NULL,           -- bcrypt
    created_at          TIMESTAMP NOT NULL,
    password_changed_at TIMESTAMP NOT NULL
);

CREATE TABLE auth_sessions (
    token_hash   TEXT PRIMARY KEY,               -- sha256 hex of the cookie token
    user_id      INTEGER NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    created_at   TIMESTAMP NOT NULL,
    expires_at   TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP NOT NULL
);
CREATE INDEX auth_sessions_expires_idx ON auth_sessions(expires_at);
CREATE INDEX auth_sessions_user_idx    ON auth_sessions(user_id);

-- Runtime-editable overrides merged on top of the YAML baseline. One row per
-- dotted koanf key; value is a JSON literal so any scalar/struct persists
-- without a per-field schema change.
CREATE TABLE settings_overrides (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

-- *arr connections owned by the database (ADR-0022). The YAML `arrs:` block
-- only seeds this table on first boot; thereafter it is the source of truth.
-- act defaults to 0 — the destructive opt-in is always explicit, per-instance.
CREATE TABLE arr_connections (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT      NOT NULL,            -- sonarr|radarr|lidarr|readarr|whisparr_v2|whisparr_v3
    name            TEXT      NOT NULL,
    url             TEXT      NOT NULL,
    api_key         TEXT      NOT NULL,
    enabled         INTEGER   NOT NULL DEFAULT 1,
    poll            INTEGER   NOT NULL DEFAULT 1,
    act             INTEGER   NOT NULL DEFAULT 0,
    tags_exclude    TEXT      NOT NULL DEFAULT '[]',   -- JSON array of strings
    categories_only TEXT      NOT NULL DEFAULT '[]',   -- JSON array of strings
    timeout_ms      INTEGER   NOT NULL DEFAULT 30000,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX idx_arr_connections_kind_name ON arr_connections(kind, name);
