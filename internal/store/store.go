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

// Game is a scheduled game from Coors Heavy's point of view. Home = true means
// Coors Heavy is the home team. Scores are recorded once Played is true.
type Game struct {
	ID        int64
	SortOrder int
	Date      string // display date, e.g. "Thu 5/14"
	Time      string // e.g. "7:00 PM"
	Opponent  string
	Home      bool
	Location  string
	Played    bool
	UsScore   int // Coors Heavy's runs
	ThemScore int // opponent's runs
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
	DeleteAllGames(ctx context.Context) error

	// Donations
	ListDonations(ctx context.Context) ([]Donation, error)
	GetDonation(ctx context.Context, id int64) (Donation, error)
	CreateDonation(ctx context.Context, name, description string, donated bool) (Donation, error)
	SetDonated(ctx context.Context, id int64, donated bool) error
	DeleteDonation(ctx context.Context, id int64) error

	Close() error
}
