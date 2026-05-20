-- Capture the moment a tracker first reported status=4 (not_working), so
-- Factor 7 (tracker_dead_bonus) can measure "sustained dead" instead of
-- "last polled" (which is rewritten every tracker tick and never crosses
-- the grace window).
--
-- Backfill: existing dead trackers get first_seen_dead = last_checked. The
-- 7d grace then re-starts from the last known poll; no false positive on
-- migration, and historical graveyards mature naturally after one grace
-- window of subsequent polls.
ALTER TABLE torrent_trackers ADD COLUMN first_seen_dead TIMESTAMP;
UPDATE torrent_trackers SET first_seen_dead = last_checked WHERE status = 4;
