package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Adyn-Cox/CoorsHeavy/internal/auth"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
	"github.com/Adyn-Cox/CoorsHeavy/internal/view"
)

// Handlers groups the HTTP handlers and their dependencies.
type Handlers struct {
	store store.Store
	authn *auth.Authenticator
}

// --- Pages ------------------------------------------------------------------

func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	_ = view.Home().Render(r.Context(), w)
}

func (h *Handlers) LineupPage(w http.ResponseWriter, r *http.Request) {
	players, err := h.store.ListPlayers(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var present, absent []store.Player
	for _, p := range players {
		if p.Attended {
			present = append(present, p)
		} else {
			absent = append(absent, p)
		}
	}
	_ = view.Lineup(present, absent).Render(r.Context(), w)
}

func (h *Handlers) SchedulePage(w http.ResponseWriter, r *http.Request) {
	games, err := h.store.ListGames(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	_ = view.Schedule(games).Render(r.Context(), w)
}

func (h *Handlers) BeerPage(w http.ResponseWriter, r *http.Request) {
	players, err := h.store.ListPlayers(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	donations, err := h.store.ListDonations(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	_ = view.Beer(players, donations).Render(r.Context(), w)
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
	if err := h.store.SaveLineup(r.Context(), ids, positions); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/lineup", http.StatusSeeOther)
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
	if err := h.store.UpdatePosition(r.Context(), id, position); err != nil {
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
	p, err := h.store.GetPlayer(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	p.Attended = !p.Attended
	if err := h.store.SetAttended(r.Context(), id, p.Attended); err != nil {
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
	if err := h.store.SetBeerRacks(r.Context(), id, racks); err != nil {
		serverError(w, err)
		return
	}
	p, err := h.store.GetPlayer(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	_ = view.BeerRow(p).Render(r.Context(), w)
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
	d, err := h.store.CreateDonation(r.Context(), name, description, donated)
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

// --- helpers ----------------------------------------------------------------

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func serverError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
