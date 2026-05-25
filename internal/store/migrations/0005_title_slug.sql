-- Add titleSlug to the media table so the torrent detail endpoint can build
-- deep links to the *arr web UI without an extra API call.
ALTER TABLE media ADD COLUMN title_slug TEXT NOT NULL DEFAULT '';
