-- Snapshot the torrent name onto the history rows at write time. The runs and
-- actions views used to resolve names by a live join on torrents, but a reaped
-- torrent is evicted from torrents immediately (ForgetTorrent) — and aged-out
-- ones by PruneStaleTorrents — so the join would miss and the post-mortem of
-- the run that just deleted something would show bare hashes. A deletion record
-- must carry the title as it was, independent of whether the torrent still
-- exists, same as actions/run_items already outlive the torrent.
ALTER TABLE run_items ADD COLUMN torrent_name TEXT NOT NULL DEFAULT '';
ALTER TABLE actions   ADD COLUMN torrent_name TEXT NOT NULL DEFAULT '';
