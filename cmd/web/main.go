package main

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Adyn-Cox/CoorsHeavy/internal/auth"
	"github.com/Adyn-Cox/CoorsHeavy/internal/config"
	"github.com/Adyn-Cox/CoorsHeavy/internal/server"
	"github.com/Adyn-Cox/CoorsHeavy/internal/spotify"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

// loadDotEnv reads KEY=VALUE pairs from a .env file and sets any that aren't
// already present in the process environment. Silently skips missing files.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

func main() {
	loadDotEnv(".env")
	loadDotEnv(".env.example") // fallback so credentials in the example file work during dev

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

	var spotifyClient *spotify.Client
	if cfg.SpotifyEnabled() {
		spotifyClient = spotify.New(cfg.SpotifyClientID, cfg.SpotifyClientSecret, cfg.SpotifyRefreshToken, cfg.SpotifyPlaylistID)
		log.Info("spotify integration enabled", "playlist_id", cfg.SpotifyPlaylistID)
	}

	// Cancel the root context on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(cfg, log, st, authn, spotifyClient)
	if err := srv.Run(ctx); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
