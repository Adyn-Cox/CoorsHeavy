// Package store is the data-access layer. Everything goes through the Store
// interface, so swapping SQLite for Postgres later means writing one new
// implementation — handlers and views don't change.
package store

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Positions are the selectable field positions for a player, in display order.
var Positions = []string{"P", "C", "1B", "2B", "3B", "SS", "LF", "CLF", "CRF", "RF", "Bench"}

// ValidPosition reports whether p is one of the allowed positions.
func ValidPosition(p string) bool {
	return slices.Contains(Positions, p)
}

// Season is one year of play. Games, donations and roster spots all hang off a
// season, so opening season 2 never touches season 1's rows.
type Season struct {
	ID        int64
	Name      string // display name, e.g. "2025"
	Year      int
	League    string // e.g. "Thursday Men's E Rec D2"
	Location  string // e.g. "Stazio #2"
	Notes     string // free-text footnotes shown under the schedule, one per line
	IsCurrent bool
}

// NoteLines splits Notes into the individual footnotes shown under the schedule.
func (s Season) NoteLines() []string {
	var out []string
	for _, line := range strings.Split(s.Notes, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// Player is a person on the team. Identity only — it persists across seasons so
// career stats can be summed. Per-season state lives on RosterPlayer.
type Player struct {
	ID   int64
	Name string
	Slug string // stable key used by the stats CSV; unique, e.g. "cooper"
}

// RosterPlayer is a Player's spot in one particular season: where they bat,
// what they field, whether they're here tonight, and how much beer they've
// brought. BeerRacks (0..2) drives the beer progress bar.
type RosterPlayer struct {
	Player
	Position    string
	Attended    bool
	LineupOrder int
	BeerRacks   float64
}

// Slugify turns a player's name into a URL- and CSV-safe key. Callers must
// still handle collisions — the roster has had duplicate first names.
func Slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// UniqueSlugs derives a slug per name, resolving collisions with a numeric
// suffix. The seed roster contains two Patty and two Parker, and players.slug
// is uniquely indexed, so the collision has to be settled before the insert —
// not patched up afterwards.
func UniqueSlugs(names []string) []string {
	used := make(map[string]bool, len(names))
	out := make([]string, len(names))
	for i, name := range names {
		base := Slugify(name)
		if base == "" {
			base = "player"
		}
		slug := base
		for n := 2; used[slug]; n++ {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		used[slug] = true
		out[i] = slug
	}
	return out
}

// GameTimes are the selectable start times for a game, in display order. The
// summer league used the first three slots; the fall league runs four games a
// night at Stazio #4 and adds the last.
var GameTimes = []string{"6:00 PM", "7:00 PM", "8:00 PM", "9:00 PM"}

// ValidGameTime reports whether t is one of the allowed start times.
func ValidGameTime(t string) bool {
	return slices.Contains(GameTimes, t)
}

// TBDOpponent is the placeholder opponent for a playoff game whose matchup
// hasn't been seeded yet.
const TBDOpponent = "TBD"

// Game is a scheduled game from Coors Heavy's point of view. Home = true means
// Coors Heavy is the home team. Scores are recorded once Played is true.
// Playoff games have no fixed time/opponent until seeding is final, so admins
// can edit both from the schedule page.
type Game struct {
	ID        int64
	SeasonID  int64
	SortOrder int
	Date      string // display date, e.g. "Thu 5/14" — carries no year
	PlayedOn  string // ISO date, e.g. "2025-05-14" — the sortable, stable one
	Time      string // one of GameTimes, e.g. "7:00 PM"
	Opponent  string
	Home      bool
	Location  string
	Played    bool
	UsScore   int  // Coors Heavy's runs
	ThemScore int  // opponent's runs
	Playoff   bool // true for the post-season games; time/opponent are admin-editable
}

// Opponents returns the distinct opponents across games in schedule order,
// skipping the TBD placeholder. This is the pick-list an admin chooses from
// when setting a playoff matchup — every team is one we've already played.
// Callers pass one season's games, so seasons never mix.
func Opponents(games []Game) []string {
	var out []string
	for _, g := range games {
		if g.Opponent == "" || g.Opponent == TBDOpponent {
			continue
		}
		if !slices.Contains(out, g.Opponent) {
			out = append(out, g.Opponent)
		}
	}
	return out
}

// Result renders the outcome from Coors Heavy's side, e.g. "W 13-12", "L 3-13",
// "T 7-7", or "" if the game hasn't been played.
func (g Game) Result() string {
	if !g.Played {
		return ""
	}
	tag := "T"
	switch {
	case g.UsScore > g.ThemScore:
		tag = "W"
	case g.UsScore < g.ThemScore:
		tag = "L"
	}
	return fmt.Sprintf("%s %d-%d", tag, g.UsScore, g.ThemScore)
}

// Label is the schedule-page date label, preferring the league's display string
// and falling back to the ISO date.
func (g Game) Label() string {
	if g.Date != "" {
		return g.Date
	}
	return g.PlayedOn
}

// --- Batting ----------------------------------------------------------------

// Batting is a set of counting stats — one game, one season, or a career. Only
// raw counts are stored; every rate below is derived, so a single line and a
// summed total share one code path.
//
// This is classic slow pitch: no stolen bases, no hit-by-pitch, no bunting, so
// none of those appear here.
type Batting struct {
	AB  int
	R   int
	H   int
	B2  int // doubles
	B3  int // triples
	HR  int
	RBI int
	BB  int
	SO  int
	SF  int // sacrifice flies — don't count as an at-bat
	E   int
}

// MinQualifiedAB is the at-bat threshold for appearing in rate-stat leaders.
// Roughly 1.5 AB per game over a 10-game season; a .750 average on four at-bats
// is not a batting title.
const MinQualifiedAB = 15

// StatColumns are the CSV/entry-grid column names, in entry order.
var StatColumns = []string{"ab", "r", "h", "2b", "3b", "hr", "rbi", "bb", "so", "sf", "e"}

// Add accumulates another line into b, for season and career totals.
func (b *Batting) Add(o Batting) {
	b.AB += o.AB
	b.R += o.R
	b.H += o.H
	b.B2 += o.B2
	b.B3 += o.B3
	b.HR += o.HR
	b.RBI += o.RBI
	b.BB += o.BB
	b.SO += o.SO
	b.SF += o.SF
	b.E += o.E
}

// The rates below follow the league's own stat key exactly, which differs from
// standard baseball in two ways worth knowing:
//
//   - PA is At Bats + Walks. Sacrifice flies do NOT get their own bucket, so a
//     sac fly counts as an at-bat like any other out.
//   - OB is Hits + Walks. Reaching on a fielder's choice is not on base, and
//     neither is reaching on an error (we don't record ROE).
//
// A rate with a zero denominator returns NaN rather than 0, so a player with no
// at-bats renders as "—" instead of claiming a .000 average.

// Singles is hits that weren't extra-base hits.
func (b Batting) Singles() int { return b.H - b.B2 - b.B3 - b.HR }

// OB is times on base: hits plus walks. Fielder's choices don't count.
func (b Batting) OB() int { return b.H + b.BB }

// PA is plate appearances, at-bats plus walks, per the league key.
func (b Batting) PA() int { return b.AB + b.BB }

// TB is total bases: 1B + 2(2B) + 3(3B) + 4(HR).
func (b Batting) TB() int { return b.Singles() + 2*b.B2 + 3*b.B3 + 4*b.HR }

// RProd is runs produced: RBI plus runs scored, less home runs so a solo shot
// isn't counted twice.
func (b Batting) RProd() int { return b.RBI + b.R - b.HR }

// AVG is batting average, Hits / At Bats.
func (b Batting) AVG() float64 { return ratio(b.H, b.AB) }

// OBP is on-base percentage, (Hits + Walks) / PA.
func (b Batting) OBP() float64 { return ratio(b.OB(), b.PA()) }

// SLG is slugging percentage, Total Bases / At Bats.
func (b Batting) SLG() float64 { return ratio(b.TB(), b.AB) }

// SlugBB is slugging with walks counted as a base and PA as the denominator.
func (b Batting) SlugBB() float64 { return ratio(b.BB+b.TB(), b.PA()) }

// OPS is on-base plus slugging. Not part of the league key, but universally
// understood, so it stays on the standard leaderboard.
func (b Batting) OPS() float64 { return b.OBP() + b.SLG() }

// HRF is home run frequency: at-bats per home run. Lower is better, and it is
// undefined until a player has actually hit one.
func (b Batting) HRF() float64 { return ratio(b.AB, b.HR) }

// RP7 estimates runs scored per 7 innings if this player took every at-bat:
// 21/(1-OB%) is the plate appearances needed to make 21 outs.
func (b Batting) RP7() float64 {
	obp := b.OBP()
	if math.IsNaN(obp) || obp >= 1 {
		// A player who has never made an out never yields the 21st out, so the
		// estimate diverges. Undefined rather than infinite.
		return math.NaN()
	}
	return ((21 / (1 - obp)) * b.SLG()) / 3
}

// Pct2B, Pct3B and PctHR are extra-base hits as a share of at-bats; PctBB is
// walks as a share of plate appearances.
func (b Batting) Pct2B() float64 { return ratio(b.B2, b.AB) }
func (b Batting) Pct3B() float64 { return ratio(b.B3, b.AB) }
func (b Batting) PctHR() float64 { return ratio(b.HR, b.AB) }
func (b Batting) PctBB() float64 { return ratio(b.BB, b.PA()) }

// RBIPerAB is average RBI per at-bat.
func (b Batting) RBIPerAB() float64 { return ratio(b.RBI, b.AB) }

// OE is offensive efficiency: runs produced per plate appearance.
func (b Batting) OE() float64 { return ratio(b.RProd(), b.PA()) }

// AVGNoHR is batting average with home runs removed from both sides.
func (b Batting) AVGNoHR() float64 { return ratio(b.H-b.HR, b.AB-b.HR) }

// OBNoHR is on-base percentage with home runs removed from both sides.
func (b Batting) OBNoHR() float64 { return ratio(b.OB()-b.HR, b.PA()-b.HR) }

// Empty reports whether the line has no recorded activity at all, so views can
// show "—" instead of a row of zeroes.
func (b Batting) Empty() bool { return b.PA() == 0 && b.R == 0 && b.E == 0 }

// Validate catches transcription mistakes that are impossible on a scorecard.
func (b Batting) Validate() error {
	for _, c := range []struct {
		name string
		v    int
	}{
		{"AB", b.AB}, {"R", b.R}, {"H", b.H}, {"2B", b.B2}, {"3B", b.B3},
		{"HR", b.HR}, {"RBI", b.RBI}, {"BB", b.BB}, {"SO", b.SO}, {"SF", b.SF}, {"E", b.E},
	} {
		if c.v < 0 {
			return fmt.Errorf("%s cannot be negative (got %d)", c.name, c.v)
		}
	}
	if b.H > b.AB {
		return fmt.Errorf("H (%d) cannot exceed AB (%d)", b.H, b.AB)
	}
	if xbh := b.B2 + b.B3 + b.HR; xbh > b.H {
		return fmt.Errorf("2B+3B+HR (%d) cannot exceed H (%d)", xbh, b.H)
	}
	if b.SO > b.AB {
		return fmt.Errorf("SO (%d) cannot exceed AB (%d)", b.SO, b.AB)
	}
	return nil
}

// ratio returns NaN when the denominator is zero, so callers can render an
// undefined rate as "—" rather than a misleading .000.
func ratio(num, den int) float64 {
	if den <= 0 {
		return math.NaN()
	}
	return float64(num) / float64(den)
}

// BattingLine is one player's batting in one game.
type BattingLine struct {
	GameID     int64
	PlayerID   int64
	LineupSpot int
	Batting
}

// PlayerBatting pairs a player with a summed stat line, for leaderboards.
type PlayerBatting struct {
	Player
	Batting
	Games int
}

// GameBatting pairs a game with one player's line in it, for a game log.
type GameBatting struct {
	Game
	Batting
}

// Donation is a booze donation entry shown on the beer page.
type Donation struct {
	ID          int64
	SeasonID    int64
	Name        string
	Donated     bool
	Description string
}

// PlayerSong is one of a player's two walk-up song slots.
type PlayerSong struct {
	PlayerID   int64
	Slot       int // 1 or 2
	TrackID    string
	TrackName  string
	ArtistName string
}

// PlayerWithSongs bundles a roster spot with its songs for the lineup view.
type PlayerWithSongs struct {
	RosterPlayer
	Song1 *PlayerSong // nil if slot 1 is unset
	Song2 *PlayerSong // nil if slot 2 is unset
}

// Itoa is a small convenience for building ids and URLs from int64 keys.
func Itoa(id int64) string { return strconv.FormatInt(id, 10) }

// Store is the persistence interface the rest of the app depends on.
type Store interface {
	// Seasons
	ListSeasons(ctx context.Context) ([]Season, error)
	GetSeason(ctx context.Context, id int64) (Season, error)
	CurrentSeason(ctx context.Context) (Season, error)
	CreateSeason(ctx context.Context, s Season) (Season, error)
	// UpdateSeason rewrites a season's name and details in place, keeping its
	// id and therefore every game, donation and roster spot that points at it.
	UpdateSeason(ctx context.Context, s Season) error
	SetCurrentSeason(ctx context.Context, id int64) error

	// Players — identity, shared across seasons
	ListAllPlayers(ctx context.Context) ([]Player, error)
	GetPlayer(ctx context.Context, id int64) (Player, error)
	GetPlayerBySlug(ctx context.Context, slug string) (Player, error)
	CreatePlayer(ctx context.Context, name string) (Player, error)
	RenamePlayer(ctx context.Context, id int64, name, slug string) error

	// Roster — a season's lineup
	ListRoster(ctx context.Context, seasonID int64) ([]RosterPlayer, error)
	GetRosterPlayer(ctx context.Context, seasonID, playerID int64) (RosterPlayer, error)
	AddToRoster(ctx context.Context, seasonID, playerID int64) error
	RemoveFromRoster(ctx context.Context, seasonID, playerID int64) error
	// CarryRosterForward copies every roster spot from one season to another,
	// keeping positions and batting order but resetting beer and attendance.
	CarryRosterForward(ctx context.Context, fromSeason, toSeason int64) (int, error)
	UpdatePosition(ctx context.Context, seasonID, playerID int64, position string) error
	SetAttended(ctx context.Context, seasonID, playerID int64, attended bool) error
	SetBeerRacks(ctx context.Context, seasonID, playerID int64, racks float64) error
	SaveLineup(ctx context.Context, seasonID int64, orderedIDs []int64, positions map[int64]string) error

	// Schedule
	ListGames(ctx context.Context, seasonID int64) ([]Game, error)
	GetGame(ctx context.Context, id int64) (Game, error)
	CreateGame(ctx context.Context, g Game) (Game, error)
	SetGameScore(ctx context.Context, id int64, us, them int, played bool) error
	// UpdateGameSchedule rewrites a game's date/time/matchup in place, keeping
	// its id — and therefore the batting lines that reference it.
	UpdateGameSchedule(ctx context.Context, g Game) error
	DeleteGame(ctx context.Context, id int64) error
	// UpdateGameMatchup sets a playoff game's start time, opponent and
	// home/away. All three come from the final seeding, so none of them is
	// known when the schedule is loaded.
	UpdateGameMatchup(ctx context.Context, id int64, gameTime, opponent string, home bool) error
	// DeleteGamesInSeason clears one season's schedule. It deliberately has no
	// whole-table counterpart: a wipe-everything delete would take out other
	// seasons and cascade away their batting lines.
	DeleteGamesInSeason(ctx context.Context, seasonID int64) error

	// Stats
	UpsertBattingLine(ctx context.Context, line BattingLine) error
	// ImportBattingLines writes a whole batch atomically. A stats import either
	// lands completely or not at all, so a bad row halfway through can't leave
	// a season half-recorded.
	ImportBattingLines(ctx context.Context, lines []BattingLine, replaceGames []int64) error
	ListBattingForGame(ctx context.Context, gameID int64) ([]BattingLine, error)
	// BoxScore is one game's lines with player names attached, in batting order.
	BoxScore(ctx context.Context, gameID int64) ([]PlayerBatting, error)
	SeasonBatting(ctx context.Context, seasonID int64) ([]PlayerBatting, error)
	CareerBatting(ctx context.Context, playerID int64) (Batting, int, error)
	PlayerGameLog(ctx context.Context, playerID, seasonID int64) ([]GameBatting, error)

	// Donations
	ListDonations(ctx context.Context, seasonID int64) ([]Donation, error)
	GetDonation(ctx context.Context, id int64) (Donation, error)
	CreateDonation(ctx context.Context, seasonID int64, name, description string, donated bool) (Donation, error)
	SetDonated(ctx context.Context, id int64, donated bool) error
	DeleteDonation(ctx context.Context, id int64) error

	// Songs — walk-up songs per player (up to 2 slots each)
	SetPlayerSong(ctx context.Context, song PlayerSong) error
	DeletePlayerSong(ctx context.Context, playerID int64, slot int) error
	// ListAllSongs returns all assigned songs ordered by lineup_order then slot.
	ListAllSongs(ctx context.Context, seasonID int64) ([]PlayerSong, error)

	Close() error
}
