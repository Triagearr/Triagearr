-- M3 scoring engine: persist the latest DeleteScore per torrent.
--
-- Shape rationale: factors_json carries the breakdown as a JSON array (see
-- internal/scorer Factor type). The Decider (M4) only ever needs `score`,
-- `excluded` and the gates; the breakdown is read whole by the CLI/UI explain
-- path. Audit history of every decision lives in audit_log (M5), not here.

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
CREATE INDEX idx_scores_computed_at ON scores(computed_at);
