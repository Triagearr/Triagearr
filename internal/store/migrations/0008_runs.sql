-- M4 triggers: persist every run (planned set of deletions) and its items.
--
-- A run is the output of the Decider: an ordered candidate list with the
-- volume it targets and why it stopped. In M4 every run has mode='dry-run'
-- and status='completed' on insert. M5 will reuse the same shape with
-- mode='live' + status transitions ('pending'/'running'/'failed').

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
