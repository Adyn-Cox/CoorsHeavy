package config

import "os"

// Config holds runtime configuration, sourced from environment variables so the
// same binary runs identically locally and in the cloud.
type Config struct {
	Port   string // HTTP port. Cloud platforms (Fly.io) inject PORT.
	DBPath string // Path to the SQLite database file.
	Env    string // "development" or "production".

	// Admin login — the single hardcoded account. Set these in the environment
	// (e.g. Fly secrets). Defaults are for local development ONLY.
	AdminUsername string
	AdminPassword string
	// SessionSecret signs the login cookie. MUST be set to a long random value
	// in production (e.g. `openssl rand -hex 32`).
	SessionSecret string

	// Spotify integration. Leave blank to disable song assignment and playlist sync.
	SpotifyClientID     string
	SpotifyClientSecret string
	SpotifyRefreshToken string
	SpotifyPlaylistID   string
	SpotifyRedirectURI  string
}

// Load reads configuration from the environment, applying sensible defaults.
func Load() Config {
	return Config{
		Port:                getenv("PORT", "8080"),
		DBPath:              getenv("DB_PATH", "coorsheavy.db"),
		Env:                 getenv("ENV", "development"),
		AdminUsername:       getenv("ADMIN_USERNAME", "admin"),
		AdminPassword:       getenv("ADMIN_PASSWORD", "changeme"),
		SessionSecret:       getenv("SESSION_SECRET", "dev-insecure-secret-change-me"),
		SpotifyClientID:     getenv("SPOTIFY_CLIENT_ID", ""),
		SpotifyClientSecret: getenv("SPOTIFY_CLIENT_SECRET", ""),
		SpotifyRefreshToken: getenv("SPOTIFY_REFRESH_TOKEN", ""),
		SpotifyPlaylistID:   getenv("SPOTIFY_PLAYLIST_ID", ""),
		SpotifyRedirectURI:  getenv("SPOTIFY_REDIRECT_URI", "http://[::1]:8080/spotify/callback"),
	}
}

// SpotifyEnabled reports whether Spotify credentials are configured.
func (c Config) SpotifyEnabled() bool {
	return c.SpotifyClientID != "" && c.SpotifyClientSecret != ""
}

// IsProduction reports whether the app is running in production mode.
func (c Config) IsProduction() bool { return c.Env == "production" }

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
