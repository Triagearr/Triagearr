-- M5 Actor: persist every destructive action taken by the actor and a
-- per-file audit trail of the API calls made on its behalf.
--
-- One `actions` row per candidate the actor attempts (1 row per run_items
-- entry it consumes). One `audit_log` row per API call — granularity is
-- per-file on the *arr side so the case "8 OK + 1 failed + 1 not-attempted"
-- in a season pack is reconstructible from the DB alone.

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

CREATE INDEX actions_hash_idx ON actions(torrent_hash);
CREATE INDEX actions_status_idx ON actions(status);

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
