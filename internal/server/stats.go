package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
	"github.com/Adyn-Cox/CoorsHeavy/internal/view"
)

// StatsPage is the batting table. With no ?game it shows season totals; with
// one it shows that game's box score in the same table, so the schedule can
// link straight to a night's numbers.
func (h *Handlers) StatsPage(w http.ResponseWriter, r *http.Request) {
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

	// Looking the id up in this season's games is also the validation: a game
	// from another season, or one that doesn't exist, falls back to totals
	// rather than rendering an empty table under a stale header.
	selected := findGame(games, gameParam(r))

	var rows []store.PlayerBatting
	if selected != nil {
		rows, err = h.store.BoxScore(r.Context(), selected.ID)
	} else {
		rows, err = h.store.SeasonBatting(r.Context(), season.ID)
	}
	if err != nil {
		serverError(w, err)
		return
	}

	var team store.Batting
	for _, row := range rows {
		team.Add(row.Batting)
	}
	_ = view.Stats(season, seasons, games, selected, rows, team).Render(r.Context(), w)
}

// gameParam reads ?game=N, returning 0 when it is absent or unparseable.
func gameParam(r *http.Request) int64 {
	id, err := strconv.ParseInt(r.URL.Query().Get("game"), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// findGame returns the game with the given id, or nil. It searches the season's
// own list so a game id can never pull a row in from a different season.
func findGame(games []store.Game, id int64) *store.Game {
	if id == 0 {
		return nil
	}
	for i := range games {
		if games[i].ID == id {
			return &games[i]
		}
	}
	return nil
}

// PlayerStatsPage is one player's game log plus season and career totals.
func (h *Handlers) PlayerStatsPage(w http.ResponseWriter, r *http.Request) {
	season, seasons, err := h.seasonContext(r)
	if err != nil {
		serverError(w, err)
		return
	}
	p, err := h.store.GetPlayerBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		serverError(w, err)
		return
	}
	log, err := h.store.PlayerGameLog(r.Context(), p.ID, season.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	var seasonTotal store.Batting
	for _, gb := range log {
		seasonTotal.Add(gb.Batting)
	}
	career, careerGames, err := h.store.CareerBatting(r.Context(), p.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	_ = view.PlayerStats(p, season, seasons, log, seasonTotal, career, careerGames).Render(r.Context(), w)
}

// --- Stat sheet -------------------------------------------------------------

// statSheetData is what the entry grid needs to render, serialised into the
// page: the season's games, the roster, and whatever is already stored for each
// game so opening a night that has been typed shows its numbers rather than a
// blank grid.
type statSheetData struct {
	Season   statSheetSeason   `json:"season"`
	Columns  []string          `json:"columns"`
	Games    []statSheetGame   `json:"games"`
	Roster   []statSheetPlayer `json:"roster"`
	Selected int64             `json:"selected"` // game to open, 0 for the first
}

type statSheetSeason struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type statSheetGame struct {
	ID       int64  `json:"id"`
	Date     string `json:"date"`  // ISO
	Label    string `json:"label"` // "Thu 5/14"
	Opponent string `json:"opponent"`
	Home     bool   `json:"home"`
	Played   bool   `json:"played"`
	Us       int    `json:"us"`
	Them     int    `json:"them"`
	// Lines already stored for this game, slug -> column -> value.
	Lines map[string]map[string]int `json:"lines"`
}

type statSheetPlayer struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// StatSheetPage renders the transcription grid. Admin-only.
func (h *Handlers) StatSheetPage(w http.ResponseWriter, r *http.Request) {
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
	roster, err := h.store.ListRoster(r.Context(), season.ID)
	if err != nil {
		serverError(w, err)
		return
	}

	data := statSheetData{
		Season:  statSheetSeason{ID: season.ID, Name: season.Name},
		Columns: store.StatColumns,
	}
	if g := findGame(games, gameParam(r)); g != nil {
		data.Selected = g.ID
	}
	for _, g := range games {
		// A game with no real date can't be told apart from the other TBD
		// playoff slots, so it can't be typed in yet.
		if g.PlayedOn == "" {
			continue
		}
		lines, err := h.store.BoxScore(r.Context(), g.ID)
		if err != nil {
			serverError(w, err)
			return
		}
		data.Games = append(data.Games, statSheetGame{
			ID:       g.ID,
			Date:     g.PlayedOn,
			Label:    g.Label(),
			Opponent: g.Opponent,
			Home:     g.Home,
			Played:   g.Played,
			Us:       g.UsScore,
			Them:     g.ThemScore,
			Lines:    storedLines(lines),
		})
	}
	for _, p := range roster {
		data.Roster = append(data.Roster, statSheetPlayer{Slug: p.Slug, Name: p.Name})
	}

	// templ.JSONScript encodes this into a <script type="application/json">
	// block, escaping <, > and & so the payload can't break out of the tag.
	_ = view.StatSheet(season, seasons, data).Render(r.Context(), w)
}

// storedLines flattens a box score into the slug -> column -> value shape the
// grid reads, using the same column names the grid posts back.
func storedLines(rows []store.PlayerBatting) map[string]map[string]int {
	out := make(map[string]map[string]int, len(rows))
	for _, row := range rows {
		line := make(map[string]int, len(store.StatColumns))
		for _, col := range store.StatColumns {
			line[col] = store.Stat(row.Batting, col)
		}
		out[row.Slug] = line
	}
	return out
}

// saveSheetRequest is what the grid posts: one game, and every non-empty line
// typed for it. Lines are keyed by slug and column name for the same reason the
// CSV is — ids differ between a local database and production.
type saveSheetRequest struct {
	GameID int64                     `json:"game_id"`
	Lines  map[string]map[string]int `json:"lines"`
}

type saveSheetResponse struct {
	OK      bool   `json:"ok"`
	Saved   int    `json:"saved"`
	Message string `json:"message"`
}

// SaveStatSheet writes one game's batting lines straight to the database,
// replacing whatever was there. Admin-only.
//
// Replacing rather than merging is deliberate: clearing a line on the sheet and
// saving has to actually remove it, or a doubled-up line typed by mistake could
// never be taken back out.
func (h *Handlers) SaveStatSheet(w http.ResponseWriter, r *http.Request) {
	var req saveSheetRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		sheetError(w, http.StatusBadRequest, "Could not read the lines being saved.")
		return
	}
	game, err := h.store.GetGame(r.Context(), req.GameID)
	if err != nil {
		sheetError(w, http.StatusNotFound, "That game is no longer on the schedule.")
		return
	}

	roster, err := h.store.ListAllPlayers(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	bySlug := make(map[string]int64, len(roster))
	for _, p := range roster {
		bySlug[p.Slug] = p.ID
	}

	// The grid renders the roster in batting order, and the JSON object it
	// posts has no order of its own, so spots come from the season roster.
	spots, err := h.store.ListRoster(r.Context(), game.SeasonID)
	if err != nil {
		serverError(w, err)
		return
	}
	spotOf := make(map[string]int, len(spots))
	for i, p := range spots {
		spotOf[p.Slug] = i + 1
	}

	var lines []store.BattingLine
	for slug, cols := range req.Lines {
		playerID, ok := bySlug[slug]
		if !ok {
			sheetError(w, http.StatusBadRequest, "Unknown player \""+slug+"\".")
			return
		}
		line := store.BattingLine{GameID: game.ID, PlayerID: playerID, LineupSpot: spotOf[slug]}
		for col, v := range cols {
			if !store.SetStat(&line.Batting, col, v) {
				sheetError(w, http.StatusBadRequest, "Unknown stat column \""+col+"\".")
				return
			}
		}
		if err := line.Batting.Validate(); err != nil {
			sheetError(w, http.StatusBadRequest, slug+": "+err.Error())
			return
		}
		// An all-zero line means the player didn't bat. Storing it would count
		// as a game played and drag every rate down.
		if line.Batting.Empty() && line.Batting.RBI == 0 {
			continue
		}
		lines = append(lines, line)
	}

	if err := h.store.ImportBattingLines(r.Context(), lines, []int64{game.ID}); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saveSheetResponse{
		OK:      true,
		Saved:   len(lines),
		Message: "Saved " + strconv.Itoa(len(lines)) + " line(s) for " + game.Label() + ".",
	})
}

func sheetError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, saveSheetResponse{OK: false, Message: msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
