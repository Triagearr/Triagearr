-- M6.x runtime-editable settings overrides.
--
-- The YAML config (config.yml) remains the immutable baseline committed by
-- the operator. A small whitelist of fields (scoring weights/thresholds,
-- disk_pressure thresholds, polling intervals) can be overridden at runtime
-- from the Settings UI; those overrides land here and are merged on top of
-- the YAML by config.LoadWithOverrides at boot and after every PUT.
--
-- One row per dotted-path key (e.g. "scoring.weights.ratio_obligation_met",
-- "polling.qbit_interval"). The value is stored as a JSON literal so any
-- scalar/struct can be persisted without per-field schema changes.

CREATE TABLE settings_overrides (
    key         TEXT PRIMARY KEY,        -- dotted path matching koanf keys
    value_json  TEXT NOT NULL,           -- JSON-encoded value
    updated_at  TIMESTAMP NOT NULL
);
