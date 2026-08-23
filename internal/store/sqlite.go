package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	// Pure-Go SQLite driver — no CGO, so the binary stays statically linked
	// and trivial to containerize.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned when a lookup by id or slug matches nothing.
var ErrNotFound = errors.New("not found")

// SQLite is the SQLite-backed implementation of Store.
type SQLite struct {
	db *sql.DB
}

var _ Store = (*SQLite)(nil)

// OpenSQLite opens (creating if needed) the database at path, runs migrations,
// and seeds the roster on first run.
//
// The DSN pragmas matter: foreign_keys is off by default in SQLite, which left
// every ON DELETE CASCADE in the schema decorative. Batting lines hang off
// games and players, so enforcement has to be on.
func OpenSQLite(path string) (*SQLite, error) {
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
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
//
// Each file runs inside a transaction together with its schema_migrations row,
// so a migration that fails partway leaves no half-applied, unrecorded DDL
// behind. Keep the 4-digit filename prefix: ordering is lexical, so "10_" would
// sort before "0004_".
//
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
		if err := s.applyMigration(name, string(b)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) applyMigration(name, body string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("migrate %s: %w", name, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(body); err != nil {
		return fmt.Errorf("migrate %s: %w", name, err)
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (name) VALUES (?)", name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return tx.Commit()
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

// seed populates the roster the first time the players table is empty and puts
// everyone on the current season's roster.
//
// The schedule is deliberately not seeded here. It used to be, gated on the
// games table being empty, which meant clearing the schedule and restarting
// silently resurrected the 2025 season. Schedules now load from CSV via
// cmd/import-schedule, which also means a new season needs no code change.
func (s *SQLite) seed() error {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM players").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var seasonID int64
	if err := s.db.QueryRow("SELECT id FROM seasons ORDER BY is_current DESC, year DESC LIMIT 1").Scan(&seasonID); err != nil {
		return fmt.Errorf("seed: no season to attach the roster to: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Slugs are uniqued up front: players.slug is uniquely indexed and the seed
	// roster has duplicate first names, so the second Patty would be rejected
	// before any after-the-fact fixup could run.
	slugs := UniqueSlugs(seedRoster)
	for i, name := range seedRoster {
		res, err := tx.Exec("INSERT INTO players (name, slug) VALUES (?, ?)", name, slugs[i])
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			"INSERT INTO season_players (season_id, player_id, position, lineup_order, attended, beer_racks) VALUES (?, ?, 'Bench', ?, 0, 0)",
			seasonID, id, i+1,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) Close() error { return s.db.Close() }

// --- Seasons ----------------------------------------------------------------

const seasonColumns = "id, name, year, league, location, notes, is_current"

func (s *SQLite) ListSeasons(ctx context.Context) ([]Season, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+seasonColumns+" FROM seasons ORDER BY year DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var seasons []Season
	for rows.Next() {
		sn, err := scanSeason(rows)
		if err != nil {
			return nil, err
		}
		seasons = append(seasons, sn)
	}
	return seasons, rows.Err()
}

func (s *SQLite) GetSeason(ctx context.Context, id int64) (Season, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+seasonColumns+" FROM seasons WHERE id = ?", id)
	sn, err := scanSeason(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Season{}, fmt.Errorf("season %d: %w", id, ErrNotFound)
	}
	return sn, err
}

// CurrentSeason returns the season marked current, falling back to the most
// recent one so the site still renders if the flag was never set.
func (s *SQLite) CurrentSeason(ctx context.Context) (Season, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+seasonColumns+" FROM seasons ORDER BY is_current DESC, year DESC, id DESC LIMIT 1")
	sn, err := scanSeason(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Season{}, fmt.Errorf("no seasons exist: %w", ErrNotFound)
	}
	return sn, err
}

func (s *SQLite) CreateSeason(ctx context.Context, sn Season) (Season, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO seasons (name, year, league, location, notes, is_current) VALUES (?, ?, ?, ?, ?, 0)",
		sn.Name, sn.Year, sn.League, sn.Location, sn.Notes)
	if err != nil {
		return Season{}, err
	}
	sn.ID, err = res.LastInsertId()
	sn.IsCurrent = false
	return sn, err
}

// UpdateSeason rewrites a season's details in place. The id is untouched, so
// every game, donation and roster spot that points at it stays put — renaming
// "2026" to "Summer 2026" is a label change, not a migration.
func (s *SQLite) UpdateSeason(ctx context.Context, sn Season) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE seasons SET name = ?, year = ?, league = ?, location = ?, notes = ?
		WHERE id = ?`,
		sn.Name, sn.Year, sn.League, sn.Location, sn.Notes, sn.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetCurrentSeason moves the current flag, in one transaction so there is never
// a moment with two current seasons or none.
func (s *SQLite) SetCurrentSeason(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE seasons SET is_current = 0"); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, "UPDATE seasons SET is_current = 1 WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("season %d: %w", id, ErrNotFound)
	}
	return tx.Commit()
}

// --- Players ----------------------------------------------------------------

func (s *SQLite) ListAllPlayers(ctx context.Context) ([]Player, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, slug FROM players ORDER BY name, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug); err != nil {
			return nil, err
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

func (s *SQLite) GetPlayer(ctx context.Context, id int64) (Player, error) {
	var p Player
	err := s.db.QueryRowContext(ctx, "SELECT id, name, slug FROM players WHERE id = ?", id).
		Scan(&p.ID, &p.Name, &p.Slug)
	if errors.Is(err, sql.ErrNoRows) {
		return Player{}, fmt.Errorf("player %d: %w", id, ErrNotFound)
	}
	return p, err
}

func (s *SQLite) GetPlayerBySlug(ctx context.Context, slug string) (Player, error) {
	var p Player
	err := s.db.QueryRowContext(ctx, "SELECT id, name, slug FROM players WHERE slug = ?", slug).
		Scan(&p.ID, &p.Name, &p.Slug)
	if errors.Is(err, sql.ErrNoRows) {
		return Player{}, fmt.Errorf("player %q: %w", slug, ErrNotFound)
	}
	return p, err
}

// CreatePlayer adds a person to the club. The slug is uniqued against the
// existing roster, so adding a third Patty works instead of tripping the unique
// index — which is exactly the situation this roster keeps producing.
func (s *SQLite) CreatePlayer(ctx context.Context, name string) (Player, error) {
	base := Slugify(name)
	if base == "" {
		return Player{}, errors.New("player name must contain at least one letter or digit")
	}

	taken, err := s.takenSlugs(ctx, base)
	if err != nil {
		return Player{}, err
	}
	slug := base
	for n := 2; taken[slug]; n++ {
		slug = fmt.Sprintf("%s-%d", base, n)
	}

	res, err := s.db.ExecContext(ctx, "INSERT INTO players (name, slug) VALUES (?, ?)", name, slug)
	if err != nil {
		return Player{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Player{}, err
	}
	return Player{ID: id, Name: name, Slug: slug}, nil
}

// takenSlugs returns the slugs already in use that could collide with base.
func (s *SQLite) takenSlugs(ctx context.Context, base string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT slug FROM players WHERE slug = ? OR slug LIKE ? || '-%'", base, base)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	taken := map[string]bool{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		taken[slug] = true
	}
	return taken, rows.Err()
}

// RenamePlayer changes a player's display name and slug. Pass an empty slug to
// derive one from the name.
func (s *SQLite) RenamePlayer(ctx context.Context, id int64, name, slug string) error {
	if slug == "" {
		slug = Slugify(name)
	}
	if slug == "" {
		return errors.New("player name must contain at least one letter or digit")
	}
	_, err := s.db.ExecContext(ctx, "UPDATE players SET name = ?, slug = ? WHERE id = ?", name, slug, id)
	return err
}

// --- Roster -----------------------------------------------------------------

const rosterSelect = `
	SELECT p.id, p.name, p.slug, sp.position, sp.attended, sp.lineup_order, sp.beer_racks
	FROM season_players sp
	JOIN players p ON p.id = sp.player_id`

func (s *SQLite) ListRoster(ctx context.Context, seasonID int64) ([]RosterPlayer, error) {
	rows, err := s.db.QueryContext(ctx,
		rosterSelect+" WHERE sp.season_id = ? ORDER BY sp.lineup_order, p.id", seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roster []RosterPlayer
	for rows.Next() {
		rp, err := scanRosterPlayer(rows)
		if err != nil {
			return nil, err
		}
		roster = append(roster, rp)
	}
	return roster, rows.Err()
}

func (s *SQLite) GetRosterPlayer(ctx context.Context, seasonID, playerID int64) (RosterPlayer, error) {
	row := s.db.QueryRowContext(ctx,
		rosterSelect+" WHERE sp.season_id = ? AND sp.player_id = ?", seasonID, playerID)
	rp, err := scanRosterPlayer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RosterPlayer{}, fmt.Errorf("player %d in season %d: %w", playerID, seasonID, ErrNotFound)
	}
	return rp, err
}

func (s *SQLite) AddToRoster(ctx context.Context, seasonID, playerID int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO season_players (season_id, player_id, lineup_order)
		VALUES (?, ?, COALESCE((SELECT MAX(lineup_order) FROM season_players WHERE season_id = ?), 0) + 1)
		ON CONFLICT(season_id, player_id) DO NOTHING`,
		seasonID, playerID, seasonID)
	return err
}

func (s *SQLite) RemoveFromRoster(ctx context.Context, seasonID, playerID int64) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM season_players WHERE season_id = ? AND player_id = ?", seasonID, playerID)
	return err
}

// CarryRosterForward copies a roster into a new season. Positions and batting
// order carry over; attendance and beer reset, since those are per-season.
func (s *SQLite) CarryRosterForward(ctx context.Context, fromSeason, toSeason int64) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO season_players (season_id, player_id, position, lineup_order, attended, beer_racks)
		SELECT ?, player_id, position, lineup_order, 0, 0
		FROM season_players WHERE season_id = ?
		ON CONFLICT(season_id, player_id) DO NOTHING`,
		toSeason, fromSeason)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *SQLite) UpdatePosition(ctx context.Context, seasonID, playerID int64, position string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE season_players SET position = ? WHERE season_id = ? AND player_id = ?",
		position, seasonID, playerID)
	return err
}

func (s *SQLite) SetAttended(ctx context.Context, seasonID, playerID int64, attended bool) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE season_players SET attended = ? WHERE season_id = ? AND player_id = ?",
		boolToInt(attended), seasonID, playerID)
	return err
}

func (s *SQLite) SetBeerRacks(ctx context.Context, seasonID, playerID int64, racks float64) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE season_players SET beer_racks = ? WHERE season_id = ? AND player_id = ?",
		racks, seasonID, playerID)
	return err
}

func (s *SQLite) SaveLineup(ctx context.Context, seasonID int64, orderedIDs []int64, positions map[int64]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx,
			"UPDATE season_players SET lineup_order = ? WHERE season_id = ? AND player_id = ?",
			i+1, seasonID, id); err != nil {
			return err
		}
	}
	for id, pos := range positions {
		if ValidPosition(pos) {
			if _, err := tx.ExecContext(ctx,
				"UPDATE season_players SET position = ? WHERE season_id = ? AND player_id = ?",
				pos, seasonID, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// --- Schedule ---------------------------------------------------------------

const gameColumns = "id, season_id, sort_order, game_date, played_on, game_time, opponent, home, location, played, us_score, them_score, playoff"

const insertGameSQL = `INSERT INTO games
	(season_id, sort_order, game_date, played_on, game_time, opponent, home, location, played, us_score, them_score, playoff)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func (s *SQLite) ListGames(ctx context.Context, seasonID int64) ([]Game, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+gameColumns+" FROM games WHERE season_id = ? ORDER BY sort_order, id", seasonID)
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
	g, err := scanGame(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Game{}, fmt.Errorf("game %d: %w", id, ErrNotFound)
	}
	return g, err
}

func (s *SQLite) CreateGame(ctx context.Context, g Game) (Game, error) {
	res, err := s.db.ExecContext(ctx, insertGameSQL,
		g.SeasonID, g.SortOrder, g.Date, g.PlayedOn, g.Time, g.Opponent, boolToInt(g.Home),
		g.Location, boolToInt(g.Played), g.UsScore, g.ThemScore, boolToInt(g.Playoff))
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

// UpdateGameSchedule rewrites the scheduling fields of an existing game. It
// exists so cmd/import-schedule can reload a season without deleting rows:
// batting_lines references games with ON DELETE CASCADE, so a delete-and-
// reinsert would silently take every recorded stat with it.
func (s *SQLite) UpdateGameSchedule(ctx context.Context, g Game) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE games SET
			sort_order = ?, game_date = ?, played_on = ?, game_time = ?,
			opponent = ?, home = ?, location = ?,
			played = ?, us_score = ?, them_score = ?, playoff = ?
		WHERE id = ?`,
		g.SortOrder, g.Date, g.PlayedOn, g.Time, g.Opponent, boolToInt(g.Home), g.Location,
		boolToInt(g.Played), g.UsScore, g.ThemScore, boolToInt(g.Playoff), g.ID)
	return err
}

func (s *SQLite) DeleteGame(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM games WHERE id = ?", id)
	return err
}

func (s *SQLite) UpdateGameMatchup(ctx context.Context, id int64, gameTime, opponent string, home bool) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE games SET game_time = ?, opponent = ?, home = ? WHERE id = ?",
		gameTime, opponent, boolToInt(home), id)
	return err
}

func (s *SQLite) DeleteGamesInSeason(ctx context.Context, seasonID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM games WHERE season_id = ?", seasonID)
	return err
}

// --- Stats ------------------------------------------------------------------

const battingSums = `SUM(bl.ab), SUM(bl.r), SUM(bl.h), SUM(bl.b2), SUM(bl.b3), SUM(bl.hr),
	SUM(bl.rbi), SUM(bl.bb), SUM(bl.so), SUM(bl.sf), SUM(bl.e)`

func (s *SQLite) UpsertBattingLine(ctx context.Context, l BattingLine) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO batting_lines
			(game_id, player_id, lineup_spot, ab, r, h, b2, b3, hr, rbi, bb, so, sf, e)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(game_id, player_id) DO UPDATE SET
			lineup_spot = excluded.lineup_spot,
			ab = excluded.ab, r  = excluded.r,  h  = excluded.h,
			b2 = excluded.b2, b3 = excluded.b3, hr = excluded.hr,
			rbi = excluded.rbi, bb = excluded.bb, so = excluded.so,
			sf = excluded.sf, e  = excluded.e`,
		l.GameID, l.PlayerID, l.LineupSpot,
		l.AB, l.R, l.H, l.B2, l.B3, l.HR, l.RBI, l.BB, l.SO, l.SF, l.E)
	return err
}

// ImportBattingLines upserts a batch in one transaction. Game ids listed in
// replaceGames have their existing lines cleared first, so re-importing a
// corrected CSV drops players who were removed from it rather than leaving
// their old line behind.
func (s *SQLite) ImportBattingLines(ctx context.Context, lines []BattingLine, replaceGames []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, gameID := range replaceGames {
		if _, err := tx.ExecContext(ctx, "DELETE FROM batting_lines WHERE game_id = ?", gameID); err != nil {
			return err
		}
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO batting_lines
			(game_id, player_id, lineup_spot, ab, r, h, b2, b3, hr, rbi, bb, so, sf, e)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(game_id, player_id) DO UPDATE SET
			lineup_spot = excluded.lineup_spot,
			ab = excluded.ab, r  = excluded.r,  h  = excluded.h,
			b2 = excluded.b2, b3 = excluded.b3, hr = excluded.hr,
			rbi = excluded.rbi, bb = excluded.bb, so = excluded.so,
			sf = excluded.sf, e  = excluded.e`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, l := range lines {
		if _, err := stmt.ExecContext(ctx, l.GameID, l.PlayerID, l.LineupSpot,
			l.AB, l.R, l.H, l.B2, l.B3, l.HR, l.RBI, l.BB, l.SO, l.SF, l.E); err != nil {
			return fmt.Errorf("game %d player %d: %w", l.GameID, l.PlayerID, err)
		}
	}
	return tx.Commit()
}

func (s *SQLite) ListBattingForGame(ctx context.Context, gameID int64) ([]BattingLine, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT bl.game_id, bl.player_id, bl.lineup_spot,
		       bl.ab, bl.r, bl.h, bl.b2, bl.b3, bl.hr, bl.rbi, bl.bb, bl.so, bl.sf, bl.e
		FROM batting_lines bl
		WHERE bl.game_id = ?
		ORDER BY bl.lineup_spot, bl.id`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []BattingLine
	for rows.Next() {
		var l BattingLine
		if err := rows.Scan(&l.GameID, &l.PlayerID, &l.LineupSpot,
			&l.AB, &l.R, &l.H, &l.B2, &l.B3, &l.HR, &l.RBI, &l.BB, &l.SO, &l.SF, &l.E); err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

// BoxScore returns every player's line for one game, in batting order. It
// returns the same PlayerBatting shape the leaderboard uses so a box score and
// a season table can share one set of column definitions — a box score is just
// a season table scoped to a single night.
func (s *SQLite) BoxScore(ctx context.Context, gameID int64) ([]PlayerBatting, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.slug,
		       bl.ab, bl.r, bl.h, bl.b2, bl.b3, bl.hr, bl.rbi, bl.bb, bl.so, bl.sf, bl.e
		FROM batting_lines bl
		JOIN players p ON p.id = bl.player_id
		WHERE bl.game_id = ?
		ORDER BY bl.lineup_spot, p.name`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlayerBatting
	for rows.Next() {
		pb := PlayerBatting{Games: 1}
		if err := rows.Scan(&pb.ID, &pb.Name, &pb.Slug,
			&pb.AB, &pb.R, &pb.H, &pb.B2, &pb.B3, &pb.HR, &pb.RBI, &pb.BB, &pb.SO, &pb.SF, &pb.E); err != nil {
			return nil, err
		}
		out = append(out, pb)
	}
	return out, rows.Err()
}

// SeasonBatting totals every player's lines for one season, best OPS first.
func (s *SQLite) SeasonBatting(ctx context.Context, seasonID int64) ([]PlayerBatting, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.slug, COUNT(bl.id), `+battingSums+`
		FROM batting_lines bl
		JOIN games   g ON g.id = bl.game_id
		JOIN players p ON p.id = bl.player_id
		WHERE g.season_id = ?
		GROUP BY p.id, p.name, p.slug
		ORDER BY p.name`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlayerBatting
	for rows.Next() {
		var pb PlayerBatting
		if err := rows.Scan(&pb.ID, &pb.Name, &pb.Slug, &pb.Games,
			&pb.AB, &pb.R, &pb.H, &pb.B2, &pb.B3, &pb.HR, &pb.RBI, &pb.BB, &pb.SO, &pb.SF, &pb.E); err != nil {
			return nil, err
		}
		out = append(out, pb)
	}
	return out, rows.Err()
}

// CareerBatting totals one player's lines across every season, with the number
// of games played.
func (s *SQLite) CareerBatting(ctx context.Context, playerID int64) (Batting, int, error) {
	var b Batting
	var games int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(bl.id), `+battingSums+`
		FROM batting_lines bl
		WHERE bl.player_id = ?`, playerID).
		Scan(&games, &b.AB, &b.R, &b.H, &b.B2, &b.B3, &b.HR, &b.RBI, &b.BB, &b.SO, &b.SF, &b.E)
	if errors.Is(err, sql.ErrNoRows) {
		return Batting{}, 0, nil
	}
	return b, games, err
}

// PlayerGameLog returns one player's line for each game they batted in. Pass
// seasonID 0 for every season.
func (s *SQLite) PlayerGameLog(ctx context.Context, playerID, seasonID int64) ([]GameBatting, error) {
	q := `
		SELECT ` + prefixed(gameColumns, "g.") + `,
		       bl.ab, bl.r, bl.h, bl.b2, bl.b3, bl.hr, bl.rbi, bl.bb, bl.so, bl.sf, bl.e
		FROM batting_lines bl
		JOIN games g ON g.id = bl.game_id
		WHERE bl.player_id = ?`
	args := []any{playerID}
	if seasonID > 0 {
		q += " AND g.season_id = ?"
		args = append(args, seasonID)
	}
	q += " ORDER BY g.season_id, g.sort_order, g.id"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GameBatting
	for rows.Next() {
		var gb GameBatting
		var home, played, playoff int
		if err := rows.Scan(&gb.ID, &gb.SeasonID, &gb.SortOrder, &gb.Date, &gb.PlayedOn, &gb.Time,
			&gb.Opponent, &home, &gb.Location, &played, &gb.UsScore, &gb.ThemScore, &playoff,
			&gb.AB, &gb.R, &gb.H, &gb.B2, &gb.B3, &gb.HR, &gb.RBI, &gb.BB, &gb.SO, &gb.SF, &gb.E); err != nil {
			return nil, err
		}
		gb.Home, gb.Played, gb.Playoff = home != 0, played != 0, playoff != 0
		out = append(out, gb)
	}
	return out, rows.Err()
}

// --- Donations --------------------------------------------------------------

func (s *SQLite) ListDonations(ctx context.Context, seasonID int64) ([]Donation, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, season_id, name, donated, description FROM donations WHERE season_id = ? ORDER BY id", seasonID)
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
	row := s.db.QueryRowContext(ctx, "SELECT id, season_id, name, donated, description FROM donations WHERE id = ?", id)
	d, err := scanDonation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Donation{}, fmt.Errorf("donation %d: %w", id, ErrNotFound)
	}
	return d, err
}

func (s *SQLite) CreateDonation(ctx context.Context, seasonID int64, name, description string, donated bool) (Donation, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO donations (season_id, name, donated, description) VALUES (?, ?, ?, ?)",
		seasonID, name, boolToInt(donated), description)
	if err != nil {
		return Donation{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Donation{}, err
	}
	return Donation{ID: id, SeasonID: seasonID, Name: name, Donated: donated, Description: description}, nil
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

// ListAllSongs returns songs for the given season's roster, in batting order.
func (s *SQLite) ListAllSongs(ctx context.Context, seasonID int64) ([]PlayerSong, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ps.player_id, ps.slot, ps.track_id, ps.track_name, ps.artist_name
		FROM player_songs ps
		JOIN season_players sp ON sp.player_id = ps.player_id AND sp.season_id = ?
		ORDER BY sp.lineup_order, ps.slot`, seasonID)
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

// prefixed qualifies a comma-separated column list with a table alias, so the
// shared gameColumns constant can be reused inside a join.
func prefixed(columns, prefix string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func scanSeason(sc scanner) (Season, error) {
	var s Season
	var current int
	err := sc.Scan(&s.ID, &s.Name, &s.Year, &s.League, &s.Location, &s.Notes, &current)
	s.IsCurrent = current != 0
	return s, err
}

func scanPlayerSong(sc scanner) (PlayerSong, error) {
	var s PlayerSong
	err := sc.Scan(&s.PlayerID, &s.Slot, &s.TrackID, &s.TrackName, &s.ArtistName)
	return s, err
}

func scanRosterPlayer(sc scanner) (RosterPlayer, error) {
	var p RosterPlayer
	var attended int
	err := sc.Scan(&p.ID, &p.Name, &p.Slug, &p.Position, &attended, &p.LineupOrder, &p.BeerRacks)
	p.Attended = attended != 0
	return p, err
}

func scanGame(sc scanner) (Game, error) {
	var g Game
	var home, played, playoff int
	err := sc.Scan(&g.ID, &g.SeasonID, &g.SortOrder, &g.Date, &g.PlayedOn, &g.Time, &g.Opponent,
		&home, &g.Location, &played, &g.UsScore, &g.ThemScore, &playoff)
	g.Home = home != 0
	g.Played = played != 0
	g.Playoff = playoff != 0
	return g, err
}

func scanDonation(sc scanner) (Donation, error) {
	var d Donation
	var donated int
	err := sc.Scan(&d.ID, &d.SeasonID, &d.Name, &donated, &d.Description)
	d.Donated = donated != 0
	return d, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
