-- Torrent client connections owned by the database (ADR-0025), mirroring
-- arr_connections (ADR-0022). The YAML `torrent_clients:` block only seeds
-- this table on first boot; thereafter it is the source of truth. kind is
-- the sole identity (one instance per torrent client type). Today only
-- qbittorrent has a backend; transmission/deluge/rtorrent are scaffolded
-- in the UI as "coming soon" but never accepted by the HTTP layer.
CREATE TABLE torrent_client_connections (
    id                INTEGER   PRIMARY KEY AUTOINCREMENT,
    kind              TEXT      NOT NULL UNIQUE,        -- qbittorrent | transmission | deluge | rtorrent
    url               TEXT      NOT NULL,
    username          TEXT      NOT NULL DEFAULT '',
    password          TEXT      NOT NULL DEFAULT '',
    enabled           INTEGER   NOT NULL DEFAULT 1,
    category_exclude  TEXT      NOT NULL DEFAULT '[]',  -- JSON array of strings
    tags_exclude      TEXT      NOT NULL DEFAULT '[]',  -- JSON array of strings
    delete_with_files INTEGER   NOT NULL DEFAULT 1,
    timeout_ms        INTEGER   NOT NULL DEFAULT 30000,
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL
);
