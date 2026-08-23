-- Seasons. Everything before this migration was implicitly the 2026 season, so
-- season 1 is created here and every existing game and donation is backfilled
-- onto it. Nothing is deleted: season 1 keeps every row it had.
CREATE TABLE seasons (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    year       INTEGER NOT NULL,
    league     TEXT    NOT NULL DEFAULT '',
    location   TEXT    NOT NULL DEFAULT '',
    notes      TEXT    NOT NULL DEFAULT '',
    is_current INTEGER NOT NULL DEFAULT 0
);

INSERT INTO seasons (id, name, year, league, location, is_current, notes)
VALUES (
    1, '2026', 2026, 'Thursday Men''s E Rec D2', 'Stazio #2', 1,
    'Week 8 — Independence Day tournament, no league game (bye).' || char(10) ||
    'Thu 7/30 — makeup game vs Big Sticks, postponed from 6/25.' || char(10) ||
    'Playoffs — Round 1: Thu 8/6 · Championship Round: Thu 8/13 · both at Stazio #2. Matchups are set by final seed.'
);

-- No REFERENCES clause on these: SQLite requires a NULL default when adding a
-- column with a foreign key, and we need NOT NULL DEFAULT 1 to backfill.
ALTER TABLE games ADD COLUMN season_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE donations ADD COLUMN season_id INTEGER NOT NULL DEFAULT 1;

-- played_on is the real ISO date. game_date stays as the display string the
-- league hands out ("Thu 5/14"), which carries no year and can't be sorted.
ALTER TABLE games ADD COLUMN played_on TEXT NOT NULL DEFAULT '';

CREATE INDEX games_season ON games (season_id, sort_order);
CREATE INDEX donations_season ON donations (season_id);

-- Backfill 2026 dates keyed on sort_order: it survives an import-schedule run,
-- which reassigns every id.
UPDATE games SET played_on = '2026-05-14' WHERE season_id = 1 AND sort_order = 1;
UPDATE games SET played_on = '2026-05-21' WHERE season_id = 1 AND sort_order = 2;
UPDATE games SET played_on = '2026-05-28' WHERE season_id = 1 AND sort_order = 3;
UPDATE games SET played_on = '2026-06-04' WHERE season_id = 1 AND sort_order = 4;
UPDATE games SET played_on = '2026-06-11' WHERE season_id = 1 AND sort_order = 5;
UPDATE games SET played_on = '2026-06-18' WHERE season_id = 1 AND sort_order = 6;
UPDATE games SET played_on = '2026-07-09' WHERE season_id = 1 AND sort_order = 7;
UPDATE games SET played_on = '2026-07-16' WHERE season_id = 1 AND sort_order = 8;
UPDATE games SET played_on = '2026-07-23' WHERE season_id = 1 AND sort_order = 9;
UPDATE games SET played_on = '2026-07-30' WHERE season_id = 1 AND sort_order = 10;
UPDATE games SET played_on = '2026-08-06' WHERE season_id = 1 AND sort_order = 11;
UPDATE games SET played_on = '2026-08-13' WHERE season_id = 1 AND sort_order = 12;
