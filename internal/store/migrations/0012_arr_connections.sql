-- ADR-0022: *arr connections owned by the database.
--
-- The YAML `arrs:` block becomes a one-time seed: on first boot, when this
-- table is empty, the daemon inserts the YAML instances here. Thereafter the
-- table is the source of truth and `cfg.Arrs` is rebuilt from it before the
-- client registry is constructed.
--
-- One row per *arr instance. `act` defaults to 0 — the non-negotiable safety
-- rule: a destructive opt-in is always explicit and per-instance.

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
