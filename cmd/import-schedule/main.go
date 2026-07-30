// Command import-schedule reloads Coors Heavy's canonical schedule from
// store.SeedGames(). Run it after editing the schedule data
// (internal/store/seed.go) — e.g. to move a postponed game or add the playoffs.
//
// Scores already recorded through the admin UI are carried over: each existing
// game is matched to a seed game by opponent + home/away (unique across the
// season, so a rescheduled game keeps its result even though its date moved).
// A "played" 0-0 is treated as a postponement placeholder and dropped.
// Pass -fresh to skip carry-over entirely and load the seed values verbatim.
//
//	go run ./cmd/import-schedule             # uses DB_PATH (default coorsheavy.db)
//	DB_PATH=/data/coorsheavy.db go run ./cmd/import-schedule
//	fly ssh console -C /app/import-schedule  # in production
package main

import (
	"context"
	"flag"
	"log"

	"github.com/Adyn-Cox/CoorsHeavy/internal/config"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

// matchupKey identifies a game independently of its date, so a postponed game
// still matches after being moved. Playoff games are excluded — their opponent
// is TBD, so several would share a key.
type matchupKey struct {
	opponent string
	home     bool
}

func main() {
	fresh := flag.Bool("fresh", false, "discard recorded scores and load the seed values verbatim")
	flag.Parse()

	cfg := config.Load()

	st, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	// Snapshot the scores currently in the database before wiping.
	existing := map[matchupKey]store.Game{}
	if !*fresh {
		current, err := st.ListGames(ctx)
		if err != nil {
			log.Fatalf("read games: %v", err)
		}
		for _, g := range current {
			if g.Playoff || !g.Played {
				continue
			}
			// A "played" 0-0 is a placeholder, not a result — it's what a
			// postponed game gets marked as. Carrying it over would stamp a
			// meaningless "T 0-0" on the rescheduled date. Clearing a game in
			// the UI sets played=false, so a genuine result is never 0-0.
			if g.UsScore == 0 && g.ThemScore == 0 {
				continue
			}
			existing[matchupKey{g.Opponent, g.Home}] = g
		}
	}

	if err := st.DeleteAllGames(ctx); err != nil {
		log.Fatalf("clear games: %v", err)
	}

	games := store.SeedGames()
	var kept int
	for _, g := range games {
		// Only fill in scores the seed doesn't already carry, so seed.go stays
		// the source of truth wherever it has an opinion.
		if !g.Played && !g.Playoff {
			if prev, ok := existing[matchupKey{g.Opponent, g.Home}]; ok {
				g.Played, g.UsScore, g.ThemScore = true, prev.UsScore, prev.ThemScore
				kept++
			}
		}
		if _, err := st.CreateGame(ctx, g); err != nil {
			log.Fatalf("insert game %q: %v", g.Date, err)
		}
	}
	log.Printf("loaded %d games into %s (%d recorded scores carried over)", len(games), cfg.DBPath, kept)
}
