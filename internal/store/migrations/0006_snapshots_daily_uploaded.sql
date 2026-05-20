-- Capture uploaded bytes so Factor 2 (upload_velocity_inv) can honour the
-- 30-day window after snapshots_raw has expired.
--
-- Existing rows keep uploaded_max=0 (the corresponding raw samples are gone;
-- no backfill is possible). The scorer treats a zero/negative delta or span
-- as insufficient data and zeroes the factor — graceful degradation rather
-- than a false positive. The library converges to fully-populated daily rows
-- after one downsampler cycle covering snapshots_raw retention (default 7d).
ALTER TABLE snapshots_daily ADD COLUMN uploaded_max INTEGER NOT NULL DEFAULT 0;
