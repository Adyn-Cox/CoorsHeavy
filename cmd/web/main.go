package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Adyn-Cox/CoorsHeavy/internal/auth"
	"github.com/Adyn-Cox/CoorsHeavy/internal/config"
	"github.com/Adyn-Cox/CoorsHeavy/internal/server"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := config.Load()
	if cfg.IsProduction() && cfg.SessionSecret == "dev-insecure-secret-change-me" {
		log.Warn("SESSION_SECRET is unset in production — set it to a long random value")
	}

	// Open the database (SQLite). Migrations run and the roster is seeded.
	st, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	authn := auth.New(cfg.AdminUsername, cfg.AdminPassword, cfg.SessionSecret, cfg.IsProduction())

	// Cancel the root context on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(cfg, log, st, authn)
	if err := srv.Run(ctx); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
