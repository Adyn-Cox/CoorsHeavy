-- Splits the roster in two: `players` becomes durable identity (a person), and
-- `season_players` holds everything that varies year to year. Without this, the
-- act of setting up season 2 would overwrite season 1's lineup and beer counts
-- in place, since those columns were single-valued per player.
CREATE TABLE season_players (
    season_id    INTEGER NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
    player_id    INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    position     TEXT    NOT NULL DEFAULT 'Bench',
    lineup_order INTEGER NOT NULL DEFAULT 0,
    attended     INTEGER NOT NULL DEFAULT 0,
    beer_racks   REAL    NOT NULL DEFAULT 0,
    PRIMARY KEY (season_id, player_id)
);

-- Must run before the DROP COLUMNs below.
INSERT INTO season_players (season_id, player_id, position, lineup_order, attended, beer_racks)
SELECT 1, id, position, lineup_order, attended, beer_racks FROM players;

-- slug is the stable, human-readable key the stats CSV uses. It has to be
-- portable between a local database and production, where import-schedule has
-- handed out different autoincrement ids.
ALTER TABLE players ADD COLUMN slug TEXT NOT NULL DEFAULT '';
UPDATE players SET slug = lower(replace(trim(name), ' ', '-'));

-- The roster contains two Patty and two Parker. Disambiguate by appending the
-- id so the unique index below can be created; rename them properly from the
-- lineup page afterwards.
UPDATE players SET slug = slug || '-' || id
  WHERE name IN (SELECT name FROM players GROUP BY name HAVING COUNT(*) > 1);

CREATE UNIQUE INDEX players_slug ON players (slug);

-- Dropped rather than left in place: a stale read from players.beer_racks after
-- season 2 opens would silently show the wrong number.
ALTER TABLE players DROP COLUMN position;
ALTER TABLE players DROP COLUMN attended;
ALTER TABLE players DROP COLUMN lineup_order;
ALTER TABLE players DROP COLUMN beer_racks;
