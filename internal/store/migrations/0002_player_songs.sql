CREATE TABLE IF NOT EXISTS player_songs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    player_id   INTEGER NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    slot        INTEGER NOT NULL CHECK (slot IN (1, 2)),
    track_id    TEXT    NOT NULL,
    track_name  TEXT    NOT NULL,
    artist_name TEXT    NOT NULL,
    UNIQUE (player_id, slot)
);
