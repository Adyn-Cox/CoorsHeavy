package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"

	// Pure-Go SQLite driver — no CGO, so the binary stays statically linked
	// and trivial to containerize.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SQLite is the SQLite-backed implementation of Store.
type SQLite struct {
	db *sql.DB
}

var _ Store = (*SQLite)(nil)

// OpenSQLite opens (creating if needed) the database at path, runs migrations,
// and seeds the roster on first run.
func OpenSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &SQLite{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	if err := s.seed(); err != nil {
		return nil, err
	}
	return s, nil
}

// migrate applies every .sql file in migrations/ in filename order, recording
// each in schema_migrations so it runs exactly once. (Tracking matters for
// non-idempotent statements — SQLite has no ALTER TABLE ... IF NOT EXISTS.)
// Swap in goose/golang-migrate when you need up/down.
func (s *SQLite) migrate() error {
	if _, err := s.db.Exec(
		"CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (datetime('now')))",
	); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := s.appliedMigrations()
	if err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		if applied[name] {
			continue
		}
		b, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(string(b)); err != nil {
			return fmt.Errorf("migrate %s: %w", name, err)
		}
		if _, err := s.db.Exec("INSERT INTO schema_migrations (name) VALUES (?)", name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *SQLite) appliedMigrations() (map[string]bool, error) {
	rows, err := s.db.Query("SELECT name FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = true
	}
	return applied, rows.Err()
}

// seed populates the roster and schedule the first time their tables are empty.
func (s *SQLite) seed() error {
	if err := s.seedPlayers(); err != nil {
		return err
	}
	return s.seedGames()
}

func (s *SQLite) seedPlayers() error {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM players").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, name := range seedRoster {
		if _, err := tx.Exec(
			"INSERT INTO players (name, position, attended, lineup_order, beer_racks) VALUES (?, 'Bench', 0, ?, 0)",
			name, i+1,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) seedGames() error {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM games").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, g := range seedGames {
		if _, err := tx.Exec(insertGameSQL,
			g.SortOrder, g.Date, g.Time, g.Opponent, boolToInt(g.Home), g.Location, boolToInt(g.Played), g.UsScore, g.ThemScore, boolToInt(g.Playoff),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) Close() error { return s.db.Close() }

// --- Players ----------------------------------------------------------------

func (s *SQLite) ListPlayers(ctx context.Context) ([]Player, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, position, attended, lineup_order, beer_racks FROM players ORDER BY lineup_order, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []Player
	for rows.Next() {
		p, err := scanPlayer(rows)
		if err != nil {
			return nil, err
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

func (s *SQLite) GetPlayer(ctx context.Context, id int64) (Player, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, name, position, attended, lineup_order, beer_racks FROM players WHERE id = ?", id)
	return scanPlayer(row)
}

func (s *SQLite) UpdatePosition(ctx context.Context, id int64, position string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE players SET position = ? WHERE id = ?", position, id)
	return err
}

func (s *SQLite) SetAttended(ctx context.Context, id int64, attended bool) error {
	_, err := s.db.ExecContext(ctx, "UPDATE players SET attended = ? WHERE id = ?", boolToInt(attended), id)
	return err
}

func (s *SQLite) SetBeerRacks(ctx context.Context, id int64, racks float64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE players SET beer_racks = ? WHERE id = ?", racks, id)
	return err
}

func (s *SQLite) ReorderPlayers(ctx context.Context, orderedIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, "UPDATE players SET lineup_order = ? WHERE id = ?", i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) SaveLineup(ctx context.Context, orderedIDs []int64, positions map[int64]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, "UPDATE players SET lineup_order = ? WHERE id = ?", i+1, id); err != nil {
			return err
		}
	}
	for id, pos := range positions {
		if ValidPosition(pos) {
			if _, err := tx.ExecContext(ctx, "UPDATE players SET position = ? WHERE id = ?", pos, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// --- Schedule ---------------------------------------------------------------

const gameColumns = "id, sort_order, game_date, game_time, opponent, home, location, played, us_score, them_score, playoff"

const insertGameSQL = `INSERT INTO games
	(sort_order, game_date, game_time, opponent, home, location, played, us_score, them_score, playoff)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func (s *SQLite) ListGames(ctx context.Context) ([]Game, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+gameColumns+" FROM games ORDER BY sort_order, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, g)
	}
	return games, rows.Err()
}

func (s *SQLite) GetGame(ctx context.Context, id int64) (Game, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+gameColumns+" FROM games WHERE id = ?", id)
	return scanGame(row)
}

func (s *SQLite) CreateGame(ctx context.Context, g Game) (Game, error) {
	res, err := s.db.ExecContext(ctx, insertGameSQL,
		g.SortOrder, g.Date, g.Time, g.Opponent, boolToInt(g.Home), g.Location, boolToInt(g.Played), g.UsScore, g.ThemScore, boolToInt(g.Playoff))
	if err != nil {
		return Game{}, err
	}
	g.ID, err = res.LastInsertId()
	return g, err
}

func (s *SQLite) SetGameScore(ctx context.Context, id int64, us, them int, played bool) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE games SET played = ?, us_score = ?, them_score = ? WHERE id = ?",
		boolToInt(played), us, them, id)
	return err
}

func (s *SQLite) UpdateGameMatchup(ctx context.Context, id int64, gameTime, opponent string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE games SET game_time = ?, opponent = ? WHERE id = ?",
		gameTime, opponent, id)
	return err
}

func (s *SQLite) DeleteAllGames(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM games")
	return err
}

// --- Donations --------------------------------------------------------------

func (s *SQLite) ListDonations(ctx context.Context) ([]Donation, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, donated, description FROM donations ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var donations []Donation
	for rows.Next() {
		d, err := scanDonation(rows)
		if err != nil {
			return nil, err
		}
		donations = append(donations, d)
	}
	return donations, rows.Err()
}

func (s *SQLite) GetDonation(ctx context.Context, id int64) (Donation, error) {
	row := s.db.QueryRowContext(ctx, "SELECT id, name, donated, description FROM donations WHERE id = ?", id)
	return scanDonation(row)
}

func (s *SQLite) CreateDonation(ctx context.Context, name, description string, donated bool) (Donation, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO donations (name, donated, description) VALUES (?, ?, ?)",
		name, boolToInt(donated), description)
	if err != nil {
		return Donation{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Donation{}, err
	}
	return Donation{ID: id, Name: name, Donated: donated, Description: description}, nil
}

func (s *SQLite) SetDonated(ctx context.Context, id int64, donated bool) error {
	_, err := s.db.ExecContext(ctx, "UPDATE donations SET donated = ? WHERE id = ?", boolToInt(donated), id)
	return err
}

func (s *SQLite) DeleteDonation(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM donations WHERE id = ?", id)
	return err
}

// --- Songs ------------------------------------------------------------------

func (s *SQLite) SetPlayerSong(ctx context.Context, song PlayerSong) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO player_songs (player_id, slot, track_id, track_name, artist_name)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(player_id, slot) DO UPDATE SET
			track_id    = excluded.track_id,
			track_name  = excluded.track_name,
			artist_name = excluded.artist_name`,
		song.PlayerID, song.Slot, song.TrackID, song.TrackName, song.ArtistName)
	return err
}

func (s *SQLite) DeletePlayerSong(ctx context.Context, playerID int64, slot int) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM player_songs WHERE player_id = ? AND slot = ?", playerID, slot)
	return err
}

func (s *SQLite) ListAllSongs(ctx context.Context) ([]PlayerSong, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ps.player_id, ps.slot, ps.track_id, ps.track_name, ps.artist_name
		FROM player_songs ps
		JOIN players p ON p.id = ps.player_id
		ORDER BY p.lineup_order, ps.slot`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var songs []PlayerSong
	for rows.Next() {
		s, err := scanPlayerSong(rows)
		if err != nil {
			return nil, err
		}
		songs = append(songs, s)
	}
	return songs, rows.Err()
}

// --- helpers ----------------------------------------------------------------

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanPlayerSong(sc scanner) (PlayerSong, error) {
	var s PlayerSong
	err := sc.Scan(&s.PlayerID, &s.Slot, &s.TrackID, &s.TrackName, &s.ArtistName)
	return s, err
}

func scanPlayer(sc scanner) (Player, error) {
	var p Player
	var attended int
	err := sc.Scan(&p.ID, &p.Name, &p.Position, &attended, &p.LineupOrder, &p.BeerRacks)
	p.Attended = attended != 0
	return p, err
}

func scanGame(sc scanner) (Game, error) {
	var g Game
	var home, played, playoff int
	err := sc.Scan(&g.ID, &g.SortOrder, &g.Date, &g.Time, &g.Opponent, &home, &g.Location, &played, &g.UsScore, &g.ThemScore, &playoff)
	g.Home = home != 0
	g.Played = played != 0
	g.Playoff = playoff != 0
	return g, err
}

func scanDonation(sc scanner) (Donation, error) {
	var d Donation
	var donated int
	err := sc.Scan(&d.ID, &d.Name, &donated, &d.Description)
	d.Donated = donated != 0
	return d, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
