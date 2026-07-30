-- Flags the post-season games. Playoff rows show a badge on the schedule and
-- let an admin pick the start time and opponent inline (seeding isn't known
-- until the regular season ends).
ALTER TABLE games ADD COLUMN playoff INTEGER NOT NULL DEFAULT 0;
