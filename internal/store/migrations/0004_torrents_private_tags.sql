-- Capture qBit's `private` and `tags` fields so the M3 scorer can gate factors
-- on the private/public regime (SCORING.md) and consult user tags for context.
--
-- private defaults to 1 (true): the safe assumption is that an existing row is
-- private until the next qBit poll proves otherwise, so the scorer does not
-- accidentally treat private torrents as swarm-only.
ALTER TABLE torrents ADD COLUMN private INTEGER NOT NULL DEFAULT 1;
ALTER TABLE torrents ADD COLUMN tags    TEXT    NOT NULL DEFAULT '';
