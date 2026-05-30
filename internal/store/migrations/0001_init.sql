CREATE TABLE IF NOT EXISTS players (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    position     TEXT    NOT NULL DEFAULT 'Bench',
    attended     INTEGER NOT NULL DEFAULT 0,
    lineup_order INTEGER NOT NULL DEFAULT 0,
    beer_racks   REAL    NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS games (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    sort_order INTEGER NOT NULL DEFAULT 0,
    game_date  TEXT    NOT NULL DEFAULT '',
    game_time  TEXT    NOT NULL DEFAULT '',
    opponent   TEXT    NOT NULL DEFAULT '',
    home       INTEGER NOT NULL DEFAULT 1,
    location   TEXT    NOT NULL DEFAULT '',
    played     INTEGER NOT NULL DEFAULT 0,
    us_score   INTEGER NOT NULL DEFAULT 0,
    them_score INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS donations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    donated     INTEGER NOT NULL DEFAULT 0,
    description TEXT    NOT NULL DEFAULT ''
);
