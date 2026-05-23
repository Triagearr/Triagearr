-- 0002_nlink: per-file hardlink count capture for cross-seed detection.
-- Enabled by ADR-0023 (TRaSH shared-mount convention makes stat() safe from
-- the Triagearr container) and consumed by the Decider's cross-seed pre-filter
-- and the Actor's T3.5 atomic check (docs/HARDLINK_TOPOLOGY.md).

CREATE TABLE torrent_files (
    torrent_hash TEXT      NOT NULL,
    rel_path     TEXT      NOT NULL,
    size_bytes   INTEGER   NOT NULL,
    nlink        INTEGER,
    sampled_at   TIMESTAMP,
    PRIMARY KEY (torrent_hash, rel_path)
);
CREATE INDEX torrent_files_nlink_idx        ON torrent_files(nlink);
CREATE INDEX torrent_files_hash_sampled_idx ON torrent_files(torrent_hash, sampled_at);
-- Pruning is manual: PruneStaleTorrents must also delete torrent_files for
-- removed hashes (same pattern as snapshots_raw, torrent_trackers).
