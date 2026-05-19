-- API-only hardlink map (ADR-0012). One row per *arr-imported file, keyed by
-- the file_id *arr will accept on DELETE. download_id is the qBit info-hash
-- (lowercased) that originally brought the file in, as recorded by *arr's
-- import history.

CREATE TABLE arr_imports (
    arr_name       TEXT    NOT NULL,
    arr_type       TEXT    NOT NULL,
    file_id        INTEGER NOT NULL,
    download_id    TEXT    NOT NULL,
    dropped_path   TEXT    NOT NULL DEFAULT '',
    imported_path  TEXT    NOT NULL DEFAULT '',
    size           INTEGER NOT NULL DEFAULT 0,
    history_id     INTEGER NOT NULL,
    imported_at    TIMESTAMP NOT NULL,
    PRIMARY KEY (arr_name, arr_type, file_id)
);
CREATE INDEX idx_arr_imports_download ON arr_imports(download_id);
CREATE INDEX idx_arr_imports_history  ON arr_imports(arr_name, arr_type, history_id);
