// Package store is the data-access layer. Everything goes through the Store
// interface, so swapping SQLite for Postgres later means writing one new
// implementation — handlers and views don't change.
package store

import (
	"context"
	"fmt"
	"slices"
)

// Positions are the selectable field positions for a player, in display order.
var Positions = []string{"P", "C", "1B", "2B", "3B", "SS", "LF", "CLF", "CRF", "RF", "Bench"}

// ValidPosition reports whether p is one of the allowed positions.
func ValidPosition(p string) bool {
	return slices.Contains(Positions, p)
}

// Player is a roster member. BeerRacks (0..2) drives the beer progress bar.
type Player struct {
	ID          int64
	Name        string
	Position    string
	Attended    bool
	LineupOrder int
	BeerRacks   float64
}

// GameTimes are the selectable start times for a game, in display order. The
// league only ever uses these three slots.
var GameTimes = []string{"6:00 PM", "7:00 PM", "8:00 PM"}

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
	SortOrder int
	Date      string // display date, e.g. "Thu 5/14"
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

// Donation is a booze donation entry shown on the beer page.
type Donation struct {
	ID          int64
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

// PlayerWithSongs bundles a Player with their assigned songs for the lineup view.
type PlayerWithSongs struct {
	Player
	Song1 *PlayerSong // nil if slot 1 is unset
	Song2 *PlayerSong // nil if slot 2 is unset
}

// Store is the persistence interface the rest of the app depends on.
type Store interface {
	// Players / lineup
	ListPlayers(ctx context.Context) ([]Player, error)
	GetPlayer(ctx context.Context, id int64) (Player, error)
	UpdatePosition(ctx context.Context, id int64, position string) error
	SetAttended(ctx context.Context, id int64, attended bool) error
	SetBeerRacks(ctx context.Context, id int64, racks float64) error
	ReorderPlayers(ctx context.Context, orderedIDs []int64) error
	SaveLineup(ctx context.Context, orderedIDs []int64, positions map[int64]string) error

	// Schedule
	ListGames(ctx context.Context) ([]Game, error)
	GetGame(ctx context.Context, id int64) (Game, error)
	CreateGame(ctx context.Context, g Game) (Game, error)
	SetGameScore(ctx context.Context, id int64, us, them int, played bool) error
	// UpdateGameMatchup sets a game's start time and opponent (playoff seeding).
	UpdateGameMatchup(ctx context.Context, id int64, gameTime, opponent string) error
	DeleteAllGames(ctx context.Context) error

	// Donations
	ListDonations(ctx context.Context) ([]Donation, error)
	GetDonation(ctx context.Context, id int64) (Donation, error)
	CreateDonation(ctx context.Context, name, description string, donated bool) (Donation, error)
	SetDonated(ctx context.Context, id int64, donated bool) error
	DeleteDonation(ctx context.Context, id int64) error

	// Songs — walk-up songs per player (up to 2 slots each)
	SetPlayerSong(ctx context.Context, song PlayerSong) error
	DeletePlayerSong(ctx context.Context, playerID int64, slot int) error
	// ListAllSongs returns all assigned songs ordered by lineup_order then slot.
	ListAllSongs(ctx context.Context) ([]PlayerSong, error)

	Close() error
}
