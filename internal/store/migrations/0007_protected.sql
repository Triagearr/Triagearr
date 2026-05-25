-- User-driven protection flag (Option A). When set, the scorer adds
-- `triagearr_protected` to exclusion_reasons, which makes the Decider skip
-- the torrent. The qBit upsert deliberately does not touch these columns, so
-- a protected flag survives sync ticks.
ALTER TABLE torrents ADD COLUMN protected INTEGER NOT NULL DEFAULT 0;
ALTER TABLE torrents ADD COLUMN protected_at TIMESTAMP;
