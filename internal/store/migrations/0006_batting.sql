-- Per-player, per-game batting lines. Classic slow pitch: no stolen bases, no
-- hit-by-pitch, no sacrifice bunts, so none of those columns exist.
--
-- Only counting stats are stored. AVG/OBP/SLG/OPS are derived in Go (see
-- store.Batting) so a season total and a single game use the same code path.
CREATE TABLE batting_lines (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id     INTEGER NOT NULL REFERENCES games(id)   ON DELETE CASCADE,
    player_id   INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    lineup_spot INTEGER NOT NULL DEFAULT 0,
    ab          INTEGER NOT NULL DEFAULT 0,
    r           INTEGER NOT NULL DEFAULT 0,
    h           INTEGER NOT NULL DEFAULT 0,
    b2          INTEGER NOT NULL DEFAULT 0,
    b3          INTEGER NOT NULL DEFAULT 0,
    hr          INTEGER NOT NULL DEFAULT 0,
    rbi         INTEGER NOT NULL DEFAULT 0,
    bb          INTEGER NOT NULL DEFAULT 0,
    so          INTEGER NOT NULL DEFAULT 0,
    sf          INTEGER NOT NULL DEFAULT 0,
    e           INTEGER NOT NULL DEFAULT 0,
    UNIQUE (game_id, player_id)
);

CREATE INDEX batting_lines_player ON batting_lines (player_id);
