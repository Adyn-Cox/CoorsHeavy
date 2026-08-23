// Command new-season opens a new season without touching the old one.
//
// Seasons own their games, donations and roster, so creating one is additive:
// season 1 keeps every row it had, and stays reachable at /schedule?season=1
// forever. Only the "current" flag moves, which is what the site shows by
// default and what the lineup page edits.
//
//	go run ./cmd/new-season -name 2026 -year 2026 -carry-roster
//	go run ./cmd/new-season -name 2026 -year 2026 -league "Thursday Men's E Rec D2" -location "Stazio #2"
//	fly ssh console -C "/app/new-season -name 2026 -year 2026 -carry-roster"
//
// Afterwards, load its schedule:
//
//	go run ./cmd/import-schedule -season <new id> -file schedule-2026.csv
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Adyn-Cox/CoorsHeavy/internal/config"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

func main() {
	name := flag.String("name", "", "season name, e.g. 2026 (required)")
	year := flag.Int("year", 0, "calendar year (required)")
	league := flag.String("league", "", "league name, e.g. \"Thursday Men's E Rec D2\"")
	location := flag.String("location", "", "home field, e.g. \"Stazio #2\"")
	notes := flag.String("notes", "", "footnotes shown under the schedule, one per line")
	carry := flag.Bool("carry-roster", false, "copy the current season's roster forward (beer and attendance reset)")
	current := flag.Bool("make-current", true, "make this the season the site shows by default")
	flag.Parse()

	if *name == "" || *year == 0 {
		fmt.Fprintln(os.Stderr, "error: -name and -year are both required")
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*name, *year, *league, *location, *notes, *carry, *current); err != nil {
		log.Fatal(err)
	}
}

func run(name string, year int, league, location, notes string, carry, makeCurrent bool) error {
	cfg := config.Load()
	st, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()

	previous, err := st.CurrentSeason(ctx)
	if err != nil {
		return err
	}

	season, err := st.CreateSeason(ctx, store.Season{
		Name:     name,
		Year:     year,
		League:   league,
		Location: location,
		Notes:    notes,
	})
	if err != nil {
		return fmt.Errorf("create season: %w", err)
	}
	fmt.Printf("created season %d (%s)\n", season.ID, season.Name)

	if carry {
		n, err := st.CarryRosterForward(ctx, previous.ID, season.ID)
		if err != nil {
			return fmt.Errorf("carry roster: %w", err)
		}
		fmt.Printf("carried %d players forward from %s (beer and attendance reset)\n", n, previous.Name)
	}

	if makeCurrent {
		if err := st.SetCurrentSeason(ctx, season.ID); err != nil {
			return fmt.Errorf("set current season: %w", err)
		}
		fmt.Printf("%s is now the current season (%s is archived and still viewable)\n", season.Name, previous.Name)
	}

	fmt.Printf("\nNext: load the schedule with\n  go run ./cmd/import-schedule -season %d -file <schedule.csv>\n", season.ID)
	return nil
}
