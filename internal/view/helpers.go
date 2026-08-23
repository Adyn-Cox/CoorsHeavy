package view

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
	"github.com/a-h/templ"
)

// matchup renders a game from Coors Heavy's perspective: "vs X" at home, "@ X" away.
func matchup(g store.Game) string {
	if g.Home {
		return "vs " + g.Opponent
	}
	return "@ " + g.Opponent
}

// homeAway is matchup's prefix on its own, for rows where the opponent is a
// dropdown rather than text.
func homeAway(g store.Game) string {
	if g.Home {
		return "vs"
	}
	return "@"
}

// scoreVal prefills the admin score inputs (blank for unplayed games).
func scoreVal(g store.Game, us bool) string {
	if !g.Played {
		return ""
	}
	if us {
		return strconv.Itoa(g.UsScore)
	}
	return strconv.Itoa(g.ThemScore)
}

// itoa renders an int64 ID as a string for use in element ids / URLs.
func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// attLabel / donatedLabel are the badge texts.
func attLabel(attended bool) string {
	if attended {
		return "Here"
	}
	return "Absent"
}

func donatedLabel(donated bool) string {
	if donated {
		return "Donated"
	}
	return "Not yet"
}

// badgeClass styles a yes/no badge (used for donations).
func badgeClass(on bool) string {
	base := "inline-block border border-white px-2 py-1 text-xs font-semibold uppercase tracking-wide "
	if on {
		return base + "bg-white text-black"
	}
	return base + "bg-black text-white"
}

// attBadgeClass styles the Here/Absent attendance badge in Coors blue when on.
func attBadgeClass(on bool) string {
	base := "inline-block border px-2 py-1 text-xs font-semibold uppercase tracking-wide "
	if on {
		return base + "border-coors-blue text-white bg-black"
	}
	return base + "border-white bg-black text-white"
}

// displayPosition converts the stored "Bench" value to the friendlier "Benched" label.
func displayPosition(pos string) string {
	if pos == "Bench" || pos == "" {
		return "Benched"
	}
	return pos
}

// barWidth is the inline style for the beer progress bar (racks out of 2).
func barWidth(racks float64) string {
	pct := racks / 2 * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return fmt.Sprintf("width: %.0f%%", pct)
}

// racksLabel formats a rack count without trailing zeros (e.g. "1.5", "2").
func racksLabel(r float64) string {
	return strconv.FormatFloat(r, 'g', -1, 64)
}

// rackOptions are the selectable rack amounts (0 to 2 thirty-racks, half steps).
func rackOptions() []float64 { return []float64{0, 0.5, 1, 1.5, 2} }

// slotLabel returns "Song 1" or "Song 2" for the search input placeholder.
func slotLabel(slot int) string { return "Song " + strconv.Itoa(slot) }

// slotStr converts a slot int to string for use in element IDs / URLs.
func slotStr(slot int) string { return strconv.Itoa(slot) }

// trackValsJSON returns an hx-vals JSON object for a track selection button.
// The JSON is HTML-attribute-safe; browsers decode entities before HTMX reads them.
func trackValsJSON(trackID, trackName, artistName string) string {
	b, _ := json.Marshal(map[string]string{
		"track_id":    trackID,
		"track_name":  trackName,
		"artist_name": artistName,
	})
	return string(b)
}

// rate formats a baseball rate stat the way a box score does: three decimals,
// and no leading zero below 1.000 (".412", but "1.750"). An undefined rate —
// a zero denominator, which store.ratio signals with NaN — renders as a dash
// rather than a misleading ".000".
func rate(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "—"
	}
	s := strconv.FormatFloat(f, 'f', 3, 64)
	if strings.HasPrefix(s, "0.") {
		return s[1:]
	}
	if strings.HasPrefix(s, "-0.") {
		return "-" + s[2:]
	}
	return s
}

// count formats a stat that is a quantity rather than a rate — at-bats per home
// run, runs per 7 innings — to one decimal place.
func count(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "—"
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

// statCell renders a counting stat, dimming zeroes so the numbers that matter
// stand out in a wide table.
func statCell(n int) string { return strconv.Itoa(n) }

// zeroDim returns a CSS class that fades zero values in a stat table.
func zeroDim(n int) string {
	if n == 0 {
		return "px-2 py-1 text-right text-gray-600"
	}
	return "px-2 py-1 text-right"
}

// qualified reports whether a stat line has enough at-bats to rank on the
// rate-stat leaderboards.
func qualified(b store.Batting) bool { return b.AB >= store.MinQualifiedAB }

// seasonPath builds a page URL for a given season.
func seasonPath(base string, seasonID int64) string {
	return base + "?season=" + strconv.FormatInt(seasonID, 10)
}

// recordLabel summarises a season's games as "5-3-1" (W-L-T), omitting ties
// when there are none.
func recordLabel(games []store.Game) string {
	var w, l, t int
	for _, g := range games {
		if !g.Played {
			continue
		}
		switch {
		case g.UsScore > g.ThemScore:
			w++
		case g.UsScore < g.ThemScore:
			l++
		default:
			t++
		}
	}
	if w+l+t == 0 {
		return "No games played yet"
	}
	if t > 0 {
		return fmt.Sprintf("%d-%d-%d", w, l, t)
	}
	return fmt.Sprintf("%d-%d", w, l)
}

// statsTitle names the stats page for the browser tab.
func statsTitle(sel *store.Game) string {
	if sel == nil {
		return "Stats"
	}
	return "Box Score · " + sel.Label()
}

// boxScoreLink points a schedule row at that game's stats. The season travels
// with it so following the link out of an archived season doesn't silently
// bounce back to the current one.
func boxScoreLink(g store.Game) templ.SafeURL {
	return templ.SafeURL("/stats?season=" + itoa(g.SeasonID) + "&game=" + itoa(g.ID))
}

// sheetLink opens the entry grid, on the selected game when there is one.
func sheetLink(season store.Season, sel *store.Game) templ.SafeURL {
	u := "/statsheet?season=" + itoa(season.ID)
	if sel != nil {
		u += "&game=" + itoa(sel.ID)
	}
	return templ.SafeURL(u)
}

// gameOption labels a game in the stats page's game picker: enough to pick the
// right night out of a dropdown without opening it.
func gameOption(g store.Game) string {
	label := g.Label() + "  " + matchup(g)
	if r := g.Result(); r != "" {
		label += "  " + r
	}
	return label
}

// joinDot is the site's separator for a run of short facts.
func joinDot(parts []string) string { return strings.Join(parts, " · ") }

// statsSubtitle is assembled in Go rather than in the template because templ
// puts a space between adjacent expressions, which turns every " · " separator
// into a wider gap than the one beside it.
func statsSubtitle(season store.Season, sel *store.Game) string {
	parts := []string{}
	if sel != nil {
		parts = append(parts, sel.Label(), matchup(*sel))
		if r := sel.Result(); r != "" {
			parts = append(parts, r)
		}
		parts = append(parts, season.Name)
	} else {
		parts = append(parts, "Batting", season.Name)
		if season.League != "" {
			parts = append(parts, season.League)
		}
	}
	return joinDot(parts)
}

// gameTime renders a start time, or TBD for a playoff game that hasn't been
// seeded. A blank cell would read as missing data rather than as "not yet set".
func gameTime(g store.Game) string {
	if g.Time == "" {
		return store.TBDOpponent
	}
	return g.Time
}
