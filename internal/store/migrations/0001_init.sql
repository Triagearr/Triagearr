-- Consolidated schema baseline (alpha).
--
-- Triagearr is alpha: there is no incremental upgrade path. The only deployed
-- database is recreated from scratch alongside any squash, so this file may
-- be re-edited freely until v1.0 ships. After v1.0, schema work appends a
-- fresh 0002_*.sql and this file becomes immutable.
--
-- Decision rationale lives in docs/adr/. Baseline invariants: one *arr
-- instance per kind (no arr_name), one watched volume (no volume_name).

------------------------------------------------------------------------------
-- Torrents
------------------------------------------------------------------------------

-- private defaults to 1: a row is assumed private until the next qBit poll
-- proves otherwise, so the scorer never treats a private torrent as swarm-only.
-- protected is user-driven; the qBit upsert deliberately leaves it untouched
-- so the flag survives sync ticks.
CREATE TABLE torrents (
    hash          TEXT      PRIMARY KEY,
    name          TEXT      NOT NULL,
    category      TEXT      NOT NULL DEFAULT '',
    save_path     TEXT      NOT NULL DEFAULT '',
    size          INTEGER   NOT NULL DEFAULT 0,
    added_on      TIMESTAMP NOT NULL,
    completion_on TIMESTAMP,
    private       INTEGER   NOT NULL DEFAULT 1,
    tags          TEXT      NOT NULL DEFAULT '',
    last_seen     TIMESTAMP NOT NULL,
    protected     INTEGER   NOT NULL DEFAULT 0,
    protected_at  TIMESTAMP
);
CREATE INDEX idx_torrents_last_seen ON torrents(last_seen);
CREATE INDEX idx_torrents_category  ON torrents(category);

-- Per-file hardlink count (ADR-0023). Consumed by the Decider's cross-seed
-- pre-filter (max-nlink lookup) and the Actor's T3.5 atomic stat re-check.
-- Pruning is manual: PruneStaleTorrents must cascade here, same pattern as
-- snapshots_raw and torrent_trackers.
CREATE TABLE torrent_files (
    torrent_hash TEXT      NOT NULL,
    rel_path     TEXT      NOT NULL,
    size_bytes   INTEGER   NOT NULL,
    nlink        INTEGER,
    sampled_at   TIMESTAMP,
    PRIMARY KEY (torrent_hash, rel_path)
) WITHOUT ROWID;
-- No secondary index on (nlink) or (sampled_at): every torrent_files query
-- filters by torrent_hash, which is the leading PK column.

CREATE TABLE snapshots_raw (
    torrent_hash  TEXT      NOT NULL,
    ts            TIMESTAMP NOT NULL,
    ratio         REAL      NOT NULL,
    uploaded      INTEGER   NOT NULL,
    seeders       INTEGER   NOT NULL,
    leechers      INTEGER   NOT NULL,
    state         TEXT      NOT NULL,
    last_activity TIMESTAMP NOT NULL,
    PRIMARY KEY (torrent_hash, ts)
) WITHOUT ROWID;
-- ts-only index serves the retention / downsample sweeps
-- (DELETE WHERE ts < ?, GROUP BY date(ts) WHERE ts < ?). The PK leads on
-- torrent_hash so ts alone can't ride it.
CREATE INDEX idx_snapshots_raw_ts ON snapshots_raw(ts);

-- uploaded_max lets Factor 2 honour the 30-day velocity window after
-- snapshots_raw has expired. A zero is treated as "no data" by the scorer.
CREATE TABLE snapshots_daily (
    torrent_hash TEXT    NOT NULL,
    day          DATE    NOT NULL,
    ratio_avg    REAL    NOT NULL,
    ratio_min    REAL    NOT NULL,
    ratio_max    REAL    NOT NULL,
    seeders_avg  REAL    NOT NULL,
    seeders_min  INTEGER NOT NULL,
    seeders_max  INTEGER NOT NULL,
    uploaded_max INTEGER NOT NULL DEFAULT 0,
    samples      INTEGER NOT NULL,
    PRIMARY KEY (torrent_hash, day)
) WITHOUT ROWID;
-- day-only index for retention (DELETE WHERE day < ?). Same reason as
-- idx_snapshots_raw_ts: PK leads on torrent_hash.
CREATE INDEX idx_snapshots_daily_day ON snapshots_daily(day);

-- first_seen_dead records when a tracker first reported status=4, so Factor 7
-- measures "sustained dead" rather than "last polled". qBit exposes no
-- status-changed-at field, so the transition is observed in ReplaceTrackers.
CREATE TABLE torrent_trackers (
    torrent_hash    TEXT      NOT NULL,
    tracker_url     TEXT      NOT NULL,
    tracker_host    TEXT      NOT NULL,
    status          INTEGER   NOT NULL,
    last_msg        TEXT      NOT NULL DEFAULT '',
    last_checked    TIMESTAMP NOT NULL,
    first_seen_dead TIMESTAMP,
    PRIMARY KEY (torrent_hash, tracker_url)
) WITHOUT ROWID;
-- No secondary index on tracker_host: it appears only in ORDER BY, never in
-- a WHERE predicate.

------------------------------------------------------------------------------
-- *arr-side state
------------------------------------------------------------------------------

CREATE TABLE media (
    id         INTEGER   NOT NULL,
    arr_type   TEXT      NOT NULL,
    title      TEXT      NOT NULL,
    title_slug TEXT      NOT NULL DEFAULT '',
    path       TEXT      NOT NULL DEFAULT '',
    size       INTEGER   NOT NULL DEFAULT 0,
    tags       TEXT      NOT NULL DEFAULT '',
    last_seen  TIMESTAMP NOT NULL,
    PRIMARY KEY (arr_type, id)
);
-- No index on media.last_seen: media is upserted but never pruned or sorted by
-- it (unlike torrents), and every read enters by the PK (arr_type, id).

CREATE TABLE media_files (
    arr_type  TEXT      NOT NULL,
    file_id   INTEGER   NOT NULL,
    media_id  INTEGER   NOT NULL,
    path      TEXT      NOT NULL,
    size      INTEGER   NOT NULL DEFAULT 0,
    last_seen TIMESTAMP NOT NULL,
    PRIMARY KEY (arr_type, file_id)
);
-- No index on media_id: every join enters media_files by the PK
-- (arr_type, file_id); media_id is only projected to reach the media row.

-- API-only hardlink map (ADR-0012). download_id is the lowercased qBit
-- info-hash that *arr's import history recorded for this file.
CREATE TABLE arr_imports (
    arr_type      TEXT      NOT NULL,
    file_id       INTEGER   NOT NULL,
    download_id   TEXT      NOT NULL,
    dropped_path  TEXT      NOT NULL DEFAULT '',
    imported_path TEXT      NOT NULL DEFAULT '',
    size          INTEGER   NOT NULL DEFAULT 0,
    history_id    INTEGER   NOT NULL,
    imported_at   TIMESTAMP NOT NULL,
    PRIMARY KEY (arr_type, file_id)
);
CREATE INDEX idx_arr_imports_download ON arr_imports(download_id);
CREATE INDEX idx_arr_imports_history  ON arr_imports(arr_type, history_id);

-- Last-known *arr health. Distinct from arr_connections (ADR-0022) which
-- owns the connection config; this table is just the most recent probe.
CREATE TABLE arr_instances (
    kind              TEXT NOT NULL PRIMARY KEY,
    url               TEXT NOT NULL,
    healthy           INTEGER NOT NULL DEFAULT 0,
    last_health_check TIMESTAMP,
    last_error        TEXT
);

-- Last-known torrent client health. Mirror of arr_instances: distinct from
-- torrent_client_connections (ADR-0025) which owns the config; this is just
-- the most recent probe. One row per kind (one client per deployment).
CREATE TABLE torrent_client_instances (
    kind              TEXT NOT NULL PRIMARY KEY,
    url               TEXT NOT NULL,
    healthy           INTEGER NOT NULL DEFAULT 0,
    last_health_check TIMESTAMP,
    last_error        TEXT
);

------------------------------------------------------------------------------
-- Disk pressure
------------------------------------------------------------------------------

-- One watched volume (ADR-0024), so a snapshot is keyed by timestamp alone.
CREATE TABLE disk_pressure (
    ts           TIMESTAMP NOT NULL PRIMARY KEY,
    path         TEXT      NOT NULL,
    total_bytes  INTEGER   NOT NULL,
    used_bytes   INTEGER   NOT NULL,
    free_bytes   INTEGER   NOT NULL,
    free_percent REAL      NOT NULL
) WITHOUT ROWID;

------------------------------------------------------------------------------
-- Scoring
------------------------------------------------------------------------------

-- Per-tracker policy + global defaults (ADR-0026). Replaces the YAML
-- scoring.per_tracker block. The defaults row is forced singleton (id=1) and
-- seeded with conservative values so an unconfigured private tracker does
-- *not* auto-pass Factor 1's ratio/seed obligation.
CREATE TABLE scoring_defaults (
    id             INTEGER   PRIMARY KEY CHECK (id = 1),
    min_ratio      REAL      NOT NULL DEFAULT 1.0,
    min_seed_days  INTEGER   NOT NULL DEFAULT 30,
    rare_threshold INTEGER   NOT NULL DEFAULT 3,
    updated_at     TIMESTAMP NOT NULL
);
INSERT INTO scoring_defaults(id, min_ratio, min_seed_days, rare_threshold, updated_at)
VALUES (1, 1.0, 30, 3, CURRENT_TIMESTAMP);

-- tracker_host is the natural key (matches torrent_trackers.tracker_host).
-- enabled=0 makes the lookup ignore the row and fall through to defaults —
-- handy to "temporarily silence" a configured tracker without losing its
-- values (e.g. while the tracker is dead).
CREATE TABLE tracker_policies (
    tracker_host   TEXT      PRIMARY KEY,
    min_ratio      REAL      NOT NULL,
    min_seed_days  INTEGER   NOT NULL,
    rare_threshold INTEGER,
    enabled        INTEGER   NOT NULL DEFAULT 1,
    updated_at     TIMESTAMP NOT NULL
) WITHOUT ROWID;

-- scores stays small-row (no blob) so WITHOUT ROWID is ideal and the partial
-- index below is near-covering for the Decider's hot path: eligible candidates
-- ranked by score. The per-factor breakdown lives in score_factors, read whole
-- only by the explain path (drawer / CLI), never during ranking.
CREATE TABLE scores (
    torrent_hash      TEXT      NOT NULL PRIMARY KEY,
    score             REAL      NOT NULL,
    private           INTEGER   NOT NULL,
    any_tracker_alive INTEGER   NOT NULL,
    excluded          INTEGER   NOT NULL DEFAULT 0,
    exclusion_reasons TEXT      NOT NULL DEFAULT '',
    computed_at       TIMESTAMP NOT NULL
) WITHOUT ROWID;
CREATE INDEX idx_scores_eligible ON scores(score DESC) WHERE excluded = 0;

-- Breakdown blob split out of scores (see above). Keyed and cascade-deleted by
-- the parent score row, so PruneStaleTorrents' DELETE FROM scores cleans it up.
CREATE TABLE score_factors (
    torrent_hash TEXT NOT NULL PRIMARY KEY
        REFERENCES scores(torrent_hash) ON DELETE CASCADE,
    factors_json TEXT NOT NULL
) WITHOUT ROWID;

------------------------------------------------------------------------------
-- Runs / actions / audit
------------------------------------------------------------------------------

CREATE TABLE runs (
    id                    INTEGER   PRIMARY KEY AUTOINCREMENT,
    triggered_by          TEXT      NOT NULL,
    triggered_at          TIMESTAMP NOT NULL,
    mode                  TEXT      NOT NULL,
    free_pct_at_fire      REAL,
    target_free_pct       REAL,
    estimated_freed_bytes INTEGER   NOT NULL DEFAULT 0,
    stop_reason           TEXT      NOT NULL,
    status                TEXT      NOT NULL DEFAULT 'completed'
);
CREATE INDEX idx_runs_triggered_at ON runs(triggered_at DESC);

CREATE TABLE run_items (
    run_id           INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    rank             INTEGER NOT NULL,
    torrent_hash     TEXT    NOT NULL,
    score            REAL    NOT NULL,
    size_bytes       INTEGER NOT NULL,
    would_free_bytes INTEGER NOT NULL,
    PRIMARY KEY (run_id, rank)
);
-- No index on torrent_hash: run_items is only read by (run_id, rank).

-- One row per candidate the actor attempts. started_at is indexed for the
-- dashboard's global action timeline (ORDER BY started_at DESC).
-- status enum: pending | running | succeeded | aborted_arr_fail
--            | aborted_nlink_check | failed_qbit | skipped_cross_seed
-- Source of truth: internal/triagearr/types.go ActionStatus.
CREATE TABLE actions (
    id           INTEGER   PRIMARY KEY AUTOINCREMENT,
    run_id       INTEGER   NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    rank         INTEGER   NOT NULL,
    torrent_hash TEXT      NOT NULL,
    started_at   TIMESTAMP NOT NULL,
    finished_at  TIMESTAMP,
    status       TEXT      NOT NULL,
    freed_bytes  INTEGER   NOT NULL DEFAULT 0,
    UNIQUE(run_id, rank)
);
-- Only started_at is indexed: the dashboard timeline orders by it. actions is
-- otherwise read by id (PK) or run_id (the UNIQUE(run_id, rank) prefix);
-- torrent_hash and status are never filter/sort predicates.
CREATE INDEX idx_actions_started_at ON actions(started_at DESC);

-- One row per API call — per-file granularity on the *arr side so a partial
-- season-pack outcome is reconstructible from the DB alone.
-- step enum:    arr_delete | qbit_delete | nlink_check
-- outcome enum: ok | failed | skipped | not_attempted
CREATE TABLE audit_log (
    id          INTEGER   PRIMARY KEY AUTOINCREMENT,
    action_id   INTEGER   NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    ts          TIMESTAMP NOT NULL,
    step        TEXT      NOT NULL,
    arr_type    TEXT,
    arr_file_id INTEGER,
    outcome     TEXT      NOT NULL,
    detail      TEXT
);
-- (action_id) alone serves "WHERE action_id=? ORDER BY id": audit_log is a
-- rowid table, so id rides every index entry and the ts column would be dead
-- weight (the query orders by id, not ts).
CREATE INDEX idx_audit_log_action ON audit_log(action_id);

------------------------------------------------------------------------------
-- Auth
------------------------------------------------------------------------------

-- Built-in auth (opt-in via UI). Auth is OFF while auth_users is empty. The DB
-- stores only sha256(token); the token itself lives only in the browser cookie.
CREATE TABLE auth_users (
    id                  INTEGER   PRIMARY KEY AUTOINCREMENT,
    username            TEXT      NOT NULL UNIQUE,
    password_hash       TEXT      NOT NULL,
    created_at          TIMESTAMP NOT NULL,
    password_changed_at TIMESTAMP NOT NULL
);

CREATE TABLE auth_sessions (
    token_hash   TEXT      PRIMARY KEY,
    user_id      INTEGER   NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    created_at   TIMESTAMP NOT NULL,
    expires_at   TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_auth_sessions_expires ON auth_sessions(expires_at);
CREATE INDEX idx_auth_sessions_user    ON auth_sessions(user_id);

------------------------------------------------------------------------------
-- Settings & connections (DB-owned, YAML only seeds on first boot)
------------------------------------------------------------------------------

-- Runtime-editable overrides merged on top of the YAML baseline. One row per
-- dotted koanf key; value is a JSON literal so any scalar/struct persists
-- without a per-field schema change.
CREATE TABLE settings_overrides (
    key        TEXT      PRIMARY KEY,
    value_json TEXT      NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

-- *arr connections (ADR-0022). kind is the sole identity (one instance per
-- *arr type). act defaults to 0 — the destructive opt-in is always explicit,
-- per-instance.
CREATE TABLE arr_connections (
    id              INTEGER   PRIMARY KEY AUTOINCREMENT,
    kind            TEXT      NOT NULL UNIQUE,        -- sonarr|radarr|lidarr|readarr|whisparr_v2|whisparr_v3
    url             TEXT      NOT NULL,
    public_url      TEXT      NOT NULL DEFAULT '',    -- optional browser-facing URL; falls back to url when empty
    api_key         TEXT      NOT NULL,
    enabled         INTEGER   NOT NULL DEFAULT 1,
    poll            INTEGER   NOT NULL DEFAULT 1,
    act             INTEGER   NOT NULL DEFAULT 0,
    tags_exclude    TEXT      NOT NULL DEFAULT '[]',  -- JSON array of strings
    categories_only TEXT      NOT NULL DEFAULT '[]',  -- JSON array of strings
    timeout_ms      INTEGER   NOT NULL DEFAULT 30000,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);

-- Torrent client connections (ADR-0025), mirroring arr_connections. Today only
-- qbittorrent has a backend; transmission/deluge/rtorrent are scaffolded in
-- the UI as "coming soon" but never accepted by the HTTP layer.
CREATE TABLE torrent_client_connections (
    id                INTEGER   PRIMARY KEY AUTOINCREMENT,
    kind              TEXT      NOT NULL UNIQUE,       -- qbittorrent | transmission | deluge | rtorrent
    url               TEXT      NOT NULL,
    public_url        TEXT      NOT NULL DEFAULT '',   -- optional browser-facing URL; falls back to url when empty
    username          TEXT      NOT NULL DEFAULT '',
    password          TEXT      NOT NULL DEFAULT '',
    enabled           INTEGER   NOT NULL DEFAULT 1,
    category_exclude  TEXT      NOT NULL DEFAULT '[]', -- JSON array of strings
    tags_exclude      TEXT      NOT NULL DEFAULT '[]', -- JSON array of strings
    delete_with_files INTEGER   NOT NULL DEFAULT 1,
    timeout_ms        INTEGER   NOT NULL DEFAULT 30000,
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL
);
