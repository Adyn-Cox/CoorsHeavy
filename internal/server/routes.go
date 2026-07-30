package server

import (
	"io/fs"
	"net/http"

	"github.com/Adyn-Cox/CoorsHeavy/internal/auth"
	"github.com/Adyn-Cox/CoorsHeavy/web"
)

// routes declares the route table (Go 1.22+ method+path patterns). Mutating
// routes are wrapped in auth.RequireAdmin so only the logged-in admin can edit;
// everything else is public read-only.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Embedded static assets at /static/ (CSS, logo, vendored JS).
	staticFS, _ := fs.Sub(web.Static, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Health check for the platform.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Public pages.
	mux.HandleFunc("GET /{$}", s.handlers.Home)
	mux.HandleFunc("GET /lineup", s.handlers.LineupPage)
	mux.HandleFunc("GET /schedule", s.handlers.SchedulePage)
	mux.HandleFunc("GET /beer", s.handlers.BeerPage)

	// Auth.
	mux.HandleFunc("GET /login", s.handlers.LoginPage)
	mux.HandleFunc("POST /login", s.handlers.LoginSubmit)
	mux.HandleFunc("POST /logout", s.handlers.Logout)

	// Admin-only actions (HTMX endpoints).
	mux.HandleFunc("POST /lineup/save", auth.RequireAdmin(s.handlers.SaveLineup))
	mux.HandleFunc("POST /players/{id}/position", auth.RequireAdmin(s.handlers.SetPosition))
	mux.HandleFunc("POST /players/{id}/attendance", auth.RequireAdmin(s.handlers.ToggleAttendance))
	mux.HandleFunc("POST /players/{id}/beer", auth.RequireAdmin(s.handlers.SetBeer))
	mux.HandleFunc("POST /games/{id}/score", auth.RequireAdmin(s.handlers.SetScore))
	mux.HandleFunc("POST /games/{id}/matchup", auth.RequireAdmin(s.handlers.SetMatchup))
	mux.HandleFunc("POST /donations", auth.RequireAdmin(s.handlers.AddDonation))
	mux.HandleFunc("POST /donations/{id}/toggle", auth.RequireAdmin(s.handlers.ToggleDonation))
	mux.HandleFunc("POST /donations/{id}/delete", auth.RequireAdmin(s.handlers.DeleteDonation))

	// Spotify song assignment — public (any teammate can set/remove songs).
	mux.HandleFunc("GET /songs/search-modal", s.handlers.SearchModal)
	mux.HandleFunc("GET /search/tracks", s.handlers.SearchTracks)
	mux.HandleFunc("POST /players/{id}/songs/{slot}", s.handlers.SetSong)
	mux.HandleFunc("DELETE /players/{id}/songs/{slot}", s.handlers.DeleteSong)

	// Spotify playlist sync — admin only.
	mux.HandleFunc("POST /playlist/sync", auth.RequireAdmin(s.handlers.SyncPlaylist))

	return s.middleware(mux)
}
