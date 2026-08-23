package server

import (
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Adyn-Cox/CoorsHeavy/internal/auth"
	"github.com/Adyn-Cox/CoorsHeavy/internal/config"
	"github.com/Adyn-Cox/CoorsHeavy/internal/spotify"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
	"github.com/Adyn-Cox/CoorsHeavy/internal/view"
)

// Handlers groups the HTTP handlers and their dependencies.
type Handlers struct {
	cfg     config.Config
	store   store.Store
	authn   *auth.Authenticator
	spotify *spotify.Client // nil when Spotify is not configured
	logger  *slog.Logger
}

// --- Season resolution ------------------------------------------------------

// season resolves which season a request is about: an explicit ?season=N (or a
// season form field on an HTMX post), otherwise the current one. Every
// season-scoped page and edit goes through here, so nothing silently reads or
// writes across seasons.
func (h *Handlers) season(r *http.Request) (store.Season, error) {
	raw := r.URL.Query().Get("season")
	if raw == "" {
		raw = r.FormValue("season")
	}
	if raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return h.store.GetSeason(r.Context(), id)
		}
	}
	return h.store.CurrentSeason(r.Context())
}

// seasonContext bundles the values every season-scoped page needs: the season
// being viewed and the full list for the switcher.
func (h *Handlers) seasonContext(r *http.Request) (store.Season, []store.Season, error) {
	sn, err := h.season(r)
	if err != nil {
		return store.Season{}, nil, err
	}
	all, err := h.store.ListSeasons(r.Context())
	return sn, all, err
}

// --- Pages ------------------------------------------------------------------

func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	_ = view.Home().Render(r.Context(), w)
}

// LineupPage always shows the current season: a lineup card is about tonight's
// game, so there is nothing to switch between.
func (h *Handlers) LineupPage(w http.ResponseWriter, r *http.Request) {
	season, err := h.store.CurrentSeason(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	players, err := h.store.ListRoster(r.Context(), season.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	allSongs, err := h.store.ListAllSongs(r.Context(), season.ID)
	if err != nil {
		serverError(w, err)
		return
	}

	// Build map from playerID to PlayerWithSongs.
	pwsMap := make(map[int64]*store.PlayerWithSongs, len(players))
	for i := range players {
		pws := &store.PlayerWithSongs{RosterPlayer: players[i]}
		pwsMap[players[i].ID] = pws
	}
	for i := range allSongs {
		s := &allSongs[i]
		if pws, ok := pwsMap[s.PlayerID]; ok {
			if s.Slot == 1 {
				pws.Song1 = s
			} else if s.Slot == 2 {
				pws.Song2 = s
			}
		}
	}

	var present, absent []store.PlayerWithSongs
	for _, p := range players {
		pws := *pwsMap[p.ID]
		if p.Attended {
			present = append(present, pws)
		} else {
			absent = append(absent, pws)
		}
	}
	_ = view.Lineup(present, absent, h.cfg.SpotifyEnabled()).Render(r.Context(), w)
}

func (h *Handlers) SchedulePage(w http.ResponseWriter, r *http.Request) {
	season, seasons, err := h.seasonContext(r)
	if err != nil {
		serverError(w, err)
		return
	}
	games, err := h.store.ListGames(r.Context(), season.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	_ = view.Schedule(season, seasons, games, store.Opponents(games)).Render(r.Context(), w)
}

func (h *Handlers) BeerPage(w http.ResponseWriter, r *http.Request) {
	season, seasons, err := h.seasonContext(r)
	if err != nil {
		serverError(w, err)
		return
	}
	players, err := h.store.ListRoster(r.Context(), season.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	donations, err := h.store.ListDonations(r.Context(), season.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	_ = view.Beer(season, seasons, players, donations).Render(r.Context(), w)
}

// --- Auth -------------------------------------------------------------------

func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	if auth.IsAdmin(r.Context()) {
		http.Redirect(w, r, "/lineup", http.StatusSeeOther)
		return
	}
	_ = view.Login("").Render(r.Context(), w)
}

func (h *Handlers) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if !h.authn.Check(username, password) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = view.Login("Invalid username or password.").Render(r.Context(), w)
		return
	}
	h.authn.IssueCookie(w)
	http.Redirect(w, r, "/lineup", http.StatusSeeOther)
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	h.authn.ClearCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- Lineup admin actions ---------------------------------------------------

func (h *Handlers) SaveLineup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	raw := r.Form["item"]
	ids := make([]int64, 0, len(raw))
	for _, s := range raw {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	positions := make(map[int64]string, len(r.Form))
	for key, vals := range r.Form {
		if strings.HasPrefix(key, "pos_") && len(vals) > 0 {
			if id, err := strconv.ParseInt(key[4:], 10, 64); err == nil {
				positions[id] = vals[0]
			}
		}
	}
	season, err := h.store.CurrentSeason(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if err := h.store.SaveLineup(r.Context(), season.ID, ids, positions); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/lineup", http.StatusSeeOther)
}

// AddPlayer creates a person and puts them on the current season's roster.
// They start benched and absent, so they land in the Absent section until
// someone marks them here — same as anyone who misses a week.
func (h *Handlers) AddPlayer(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	season, err := h.store.CurrentSeason(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	p, err := h.store.CreatePlayer(r.Context(), name)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := h.store.AddToRoster(r.Context(), season.ID, p.ID); err != nil {
		serverError(w, err)
		return
	}
	rp, err := h.store.GetRosterPlayer(r.Context(), season.ID, p.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	// idx -1 renders an un-numbered row: they're not in the batting order yet.
	_ = view.PlayerRow(-1, store.PlayerWithSongs{RosterPlayer: rp}, h.cfg.SpotifyEnabled()).
		Render(r.Context(), w)
}

// RenamePlayer changes a player's display name. This is what finally lets the
// duplicate seed names (two Patty, two Parker) be told apart — until they are,
// four players are ambiguous in the stat sheet and any name-keyed CSV.
//
// The slug is left alone: it is the key the stats CSV already references, so
// regenerating it would orphan lines already typed against the old one.
func (h *Handlers) RenamePlayer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name cannot be empty", http.StatusBadRequest)
		return
	}
	p, err := h.store.GetPlayer(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := h.store.RenamePlayer(r.Context(), id, name, p.Slug); err != nil {
		serverError(w, err)
		return
	}
	season, err := h.store.CurrentSeason(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	rp, err := h.store.GetRosterPlayer(r.Context(), season.ID, id)
	if err != nil {
		serverError(w, err)
		return
	}
	_ = view.PlayerName(rp).Render(r.Context(), w)
}

func (h *Handlers) SetPosition(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	position := r.FormValue("position")
	if !store.ValidPosition(position) {
		http.Error(w, "invalid position", http.StatusBadRequest)
		return
	}
	season, err := h.season(r)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := h.store.UpdatePosition(r.Context(), season.ID, id, position); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) ToggleAttendance(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	season, err := h.season(r)
	if err != nil {
		serverError(w, err)
		return
	}
	p, err := h.store.GetRosterPlayer(r.Context(), season.ID, id)
	if err != nil {
		serverError(w, err)
		return
	}
	p.Attended = !p.Attended
	if err := h.store.SetAttended(r.Context(), season.ID, id, p.Attended); err != nil {
		serverError(w, err)
		return
	}
	_ = view.AttendanceBadge(p).Render(r.Context(), w)
}

func (h *Handlers) SetBeer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	racks, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("racks")), 64)
	if err != nil {
		http.Error(w, "invalid racks value", http.StatusBadRequest)
		return
	}
	if racks < 0 {
		racks = 0
	}
	if racks > 2 {
		racks = 2
	}
	season, err := h.season(r)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := h.store.SetBeerRacks(r.Context(), season.ID, id, racks); err != nil {
		serverError(w, err)
		return
	}
	p, err := h.store.GetRosterPlayer(r.Context(), season.ID, id)
	if err != nil {
		serverError(w, err)
		return
	}
	_ = view.BeerRow(season.ID, p).Render(r.Context(), w)
}

// --- Schedule admin actions -------------------------------------------------

// SetScore records (or clears) a game's score and returns the updated cell.
func (h *Handlers) SetScore(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if r.FormValue("clear") == "1" {
		if err := h.store.SetGameScore(r.Context(), id, 0, 0, false); err != nil {
			serverError(w, err)
			return
		}
	} else {
		us, err1 := strconv.Atoi(strings.TrimSpace(r.FormValue("us")))
		them, err2 := strconv.Atoi(strings.TrimSpace(r.FormValue("them")))
		if err1 != nil || err2 != nil {
			http.Error(w, "enter both scores as numbers", http.StatusBadRequest)
			return
		}
		if err := h.store.SetGameScore(r.Context(), id, us, them, true); err != nil {
			serverError(w, err)
			return
		}
	}
	g, err := h.store.GetGame(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	_ = view.ScoreCell(g).Render(r.Context(), w)
}

// SetMatchup sets a playoff game's start time and/or opponent and returns the
// updated schedule row. Each <select> posts on change, so only the field that
// changed is present in the form — a missing field leaves that value alone.
func (h *Handlers) SetMatchup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Scope the opponent pick-list to the game's own season, so a playoff
	// matchup can never be set to a team from a different year.
	target, err := h.store.GetGame(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	games, err := h.store.ListGames(r.Context(), target.SeasonID)
	if err != nil {
		serverError(w, err)
		return
	}
	idx := slices.IndexFunc(games, func(g store.Game) bool { return g.ID == id })
	if idx < 0 {
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}
	g := games[idx]
	if !g.Playoff {
		http.Error(w, "only playoff games can be rescheduled", http.StatusBadRequest)
		return
	}

	gameTime, opponent := g.Time, g.Opponent
	opponents := store.Opponents(games)
	if v, ok := formValue(r, "time"); ok {
		if !store.ValidGameTime(v) {
			http.Error(w, "invalid start time", http.StatusBadRequest)
			return
		}
		gameTime = v
	}
	if v, ok := formValue(r, "opponent"); ok {
		if v != store.TBDOpponent && !slices.Contains(opponents, v) {
			http.Error(w, "unknown opponent", http.StatusBadRequest)
			return
		}
		opponent = v
	}

	if err := h.store.UpdateGameMatchup(r.Context(), id, gameTime, opponent); err != nil {
		serverError(w, err)
		return
	}
	g.Time, g.Opponent = gameTime, opponent
	_ = view.GameRow(g, opponents).Render(r.Context(), w)
}

// --- Donations admin actions ------------------------------------------------

func (h *Handlers) AddDonation(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	description := strings.TrimSpace(r.FormValue("description"))
	donated := r.FormValue("donated") == "true"
	season, err := h.season(r)
	if err != nil {
		serverError(w, err)
		return
	}
	d, err := h.store.CreateDonation(r.Context(), season.ID, name, description, donated)
	if err != nil {
		serverError(w, err)
		return
	}
	_ = view.DonationRow(d).Render(r.Context(), w)
}

func (h *Handlers) ToggleDonation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, err := h.store.GetDonation(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	d.Donated = !d.Donated
	if err := h.store.SetDonated(r.Context(), id, d.Donated); err != nil {
		serverError(w, err)
		return
	}
	_ = view.DonatedBadge(d).Render(r.Context(), w)
}

func (h *Handlers) DeleteDonation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.store.DeleteDonation(r.Context(), id); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK) // empty body -> HTMX outerHTML swap removes the row
}

// --- Spotify song actions ---------------------------------------------------

// SearchModal handles GET /songs/search-modal?player_id=&slot= and returns the
// inner HTML of the song search modal. Public — no auth required.
func (h *Handlers) SearchModal(w http.ResponseWriter, r *http.Request) {
	playerID, _ := strconv.ParseInt(r.URL.Query().Get("player_id"), 10, 64)
	slot, _ := strconv.Atoi(r.URL.Query().Get("slot"))
	if slot != 1 && slot != 2 {
		slot = 1
	}
	p, err := h.store.GetPlayer(r.Context(), playerID)
	if err != nil {
		http.Error(w, "player not found", http.StatusNotFound)
		return
	}
	_ = view.SongSearchModal(p.Name, playerID, slot).Render(r.Context(), w)
}

// SearchTracks handles GET /search/tracks?q=&player_id=&slot= and returns an
// HTMX fragment with up to 5 matching tracks. Public — no auth required.
func (h *Handlers) SearchTracks(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	playerID, _ := strconv.ParseInt(r.URL.Query().Get("player_id"), 10, 64)
	slot, _ := strconv.Atoi(r.URL.Query().Get("slot"))

	if len(q) < 2 || h.spotify == nil {
		_ = view.TrackSearchResults(nil, playerID, slot).Render(r.Context(), w)
		return
	}
	tracks, err := h.spotify.SearchTracks(r.Context(), q)
	if err != nil {
		h.logger.Error("spotify search failed", "err", err)
		_ = view.TrackSearchResults(nil, playerID, slot).Render(r.Context(), w)
		return
	}
	_ = view.TrackSearchResults(tracks, playerID, slot).Render(r.Context(), w)
}

// SetSong handles POST /players/{id}/songs/{slot} and saves a track selection.
// Public — no auth required so any teammate can assign songs.
func (h *Handlers) SetSong(w http.ResponseWriter, r *http.Request) {
	playerID, ok := pathID(w, r)
	if !ok {
		return
	}
	slot, err := strconv.Atoi(r.PathValue("slot"))
	if err != nil || (slot != 1 && slot != 2) {
		http.Error(w, "slot must be 1 or 2", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	song := store.PlayerSong{
		PlayerID:   playerID,
		Slot:       slot,
		TrackID:    strings.TrimSpace(r.FormValue("track_id")),
		TrackName:  strings.TrimSpace(r.FormValue("track_name")),
		ArtistName: strings.TrimSpace(r.FormValue("artist_name")),
	}
	if song.TrackID == "" || song.TrackName == "" {
		http.Error(w, "track_id and track_name required", http.StatusBadRequest)
		return
	}
	if err := h.store.SetPlayerSong(r.Context(), song); err != nil {
		serverError(w, err)
		return
	}
	_ = view.SongSlot(playerID, slot, &song).Render(r.Context(), w)
}

// DeleteSong handles DELETE /players/{id}/songs/{slot} and clears a song slot.
// Public — no auth required.
func (h *Handlers) DeleteSong(w http.ResponseWriter, r *http.Request) {
	playerID, ok := pathID(w, r)
	if !ok {
		return
	}
	slot, err := strconv.Atoi(r.PathValue("slot"))
	if err != nil || (slot != 1 && slot != 2) {
		http.Error(w, "slot must be 1 or 2", http.StatusBadRequest)
		return
	}
	if err := h.store.DeletePlayerSong(r.Context(), playerID, slot); err != nil {
		serverError(w, err)
		return
	}
	_ = view.SongSlot(playerID, slot, nil).Render(r.Context(), w)
}

// SyncPlaylist handles POST /playlist/sync (admin-only). It pushes all assigned
// walk-up songs into the configured Spotify playlist. Round 1 is every active
// player's first song; round 2 is their second song (or first again if absent).
// Players who are benched, not attending, or have no songs are skipped.
func (h *Handlers) SyncPlaylist(w http.ResponseWriter, r *http.Request) {
	if h.spotify == nil {
		http.Error(w, "spotify not configured", http.StatusServiceUnavailable)
		return
	}
	season, err := h.store.CurrentSeason(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	players, err := h.store.ListRoster(r.Context(), season.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	allSongs, err := h.store.ListAllSongs(r.Context(), season.ID)
	if err != nil {
		serverError(w, err)
		return
	}

	song1 := make(map[int64]store.PlayerSong)
	song2 := make(map[int64]store.PlayerSong)
	for _, s := range allSongs {
		if s.Slot == 1 {
			song1[s.PlayerID] = s
		} else if s.Slot == 2 {
			song2[s.PlayerID] = s
		}
	}

	// Include players who are present and have at least one song. Skip absent players.
	var active []store.RosterPlayer
	for _, p := range players {
		if !p.Attended {
			continue
		}
		if _, has := song1[p.ID]; !has {
			continue
		}
		active = append(active, p)
	}

	var trackIDs []string
	// Round 1 — everyone's first song.
	for _, p := range active {
		trackIDs = append(trackIDs, song1[p.ID].TrackID)
	}
	// Round 2 — second song if set, else repeat first.
	for _, p := range active {
		if s, ok := song2[p.ID]; ok {
			trackIDs = append(trackIDs, s.TrackID)
		} else {
			trackIDs = append(trackIDs, song1[p.ID].TrackID)
		}
	}

	if err := h.spotify.SyncPlaylist(r.Context(), trackIDs); err != nil {
		_ = view.SyncResult(false, 0, err.Error()).Render(r.Context(), w)
		return
	}
	_ = view.SyncResult(true, len(trackIDs), "").Render(r.Context(), w)
}

// --- helpers ----------------------------------------------------------------

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// formValue returns a trimmed form field and whether it was submitted at all,
// so callers can tell "left unchanged" apart from "cleared".
func formValue(r *http.Request, key string) (string, bool) {
	vals, ok := r.Form[key]
	if !ok || len(vals) == 0 {
		return "", false
	}
	return strings.TrimSpace(vals[0]), true
}

func serverError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
