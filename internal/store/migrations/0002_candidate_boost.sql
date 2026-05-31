-- candidate_boost is the user-driven inverse of protected: it adds a large
-- positive contribution to the DeleteScore so the operator can force a torrent
-- to the top of the reap queue (scorer Factor 9, ADR-0030). Like protected it
-- is excluded from the qBit upsert so the flag survives sync ticks, and the two
-- are mutually exclusive — the store clears one when setting the other.
ALTER TABLE torrents ADD COLUMN candidate_boost    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE torrents ADD COLUMN candidate_boost_at TIMESTAMP;
