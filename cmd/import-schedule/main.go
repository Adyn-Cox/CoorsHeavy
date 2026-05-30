// Command import-schedule wipes the games table and reloads Coors Heavy's
// canonical schedule from store.SeedGames(). Run it after editing the schedule
// data (internal/store/seed.go) — e.g. to add a weekly score.
//
//	go run ./cmd/import-schedule        # uses DB_PATH (default coorsheavy.db)
//	DB_PATH=/data/coorsheavy.db go run ./cmd/import-schedule
package main

import (
	"context"
	"log"

	"github.com/Adyn-Cox/CoorsHeavy/internal/config"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

func main() {
	cfg := config.Load()

	st, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	if err := st.DeleteAllGames(ctx); err != nil {
		log.Fatalf("clear games: %v", err)
	}

	games := store.SeedGames()
	for _, g := range games {
		if _, err := st.CreateGame(ctx, g); err != nil {
			log.Fatalf("insert game %q: %v", g.Date, err)
		}
	}
	log.Printf("loaded %d games into %s", len(games), cfg.DBPath)
}
