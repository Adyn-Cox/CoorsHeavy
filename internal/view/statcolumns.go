package view

import (
	"math"

	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

// statColumn is one column of the stats table. Defining the columns as data
// rather than hand-written markup keeps the league's 27-stat key from turning
// every table into thirty near-identical cells, and guarantees the header, the
// player rows and the team totals row can never drift out of step.
type statColumn struct {
	Label string
	Group string // "key" (always shown), "std", or "adv"
	Text  bool   // sorts as text rather than a number
	Help  string // shown as a tooltip; the definition from the league key
	Best  bestDirection
	Rate  bool // a rate, so the qualifying at-bat threshold applies to leading it
	Cell  func(store.PlayerBatting) string
	// Value is Cell's number, for finding a column's leader. Rendered text
	// can't be compared — ".500" and "—" say nothing about which is larger.
	Value func(store.PlayerBatting) float64
}

// bestDirection says which end of a column is the achievement worth marking.
// Most columns have neither end: leading the team in games played or in
// strikeouts is not something to paint red.
type bestDirection int

const (
	bestNone bestDirection = iota
	bestHigh
	bestLow
)

// Groups. The league key defines 27 stats; showing them all at once is
// unreadable, so the table renders in three modes and hides the rest.
const (
	groupKey = "key"
	groupStd = "std"
	groupAdv = "adv"
)

// statColumns is the full table, in display order. Every stat in the league key
// appears exactly once, in one group or the other.
func statColumns() []statColumn {
	num := func(label, group, help string, f func(store.PlayerBatting) int) statColumn {
		return statColumn{Label: label, Group: group, Help: help, Best: bestHigh,
			Cell:  func(p store.PlayerBatting) string { return statCell(f(p)) },
			Value: func(p store.PlayerBatting) float64 { return float64(f(p)) }}
	}
	pct := func(label, group, help string, f func(store.PlayerBatting) float64) statColumn {
		return statColumn{Label: label, Group: group, Help: help, Best: bestHigh, Rate: true,
			Cell:  func(p store.PlayerBatting) string { return rate(f(p)) },
			Value: f}
	}
	qty := func(label, group, help string, f func(store.PlayerBatting) float64) statColumn {
		return statColumn{Label: label, Group: group, Help: help, Best: bestHigh, Rate: true,
			Cell:  func(p store.PlayerBatting) string { return count(f(p)) },
			Value: f}
	}

	cols := []statColumn{
		// Always on screen: identity plus the denominators most of the rates
		// below divide by, so an advanced column never floats without context.
		{Label: "Player", Group: groupKey, Text: true, Help: "Click for game log and career totals",
			Cell: func(p store.PlayerBatting) string { return p.Name }},
		num("GM", groupKey, "Games played", func(p store.PlayerBatting) int { return p.Games }),
		num("PA", groupKey, "Plate appearances (at bats + walks)", func(p store.PlayerBatting) int { return p.PA() }),
		num("AB", groupKey, "At bats", func(p store.PlayerBatting) int { return p.AB }),

		// --- standard -------------------------------------------------------
		num("Runs", groupStd, "Runs scored", func(p store.PlayerBatting) int { return p.R }),
		num("Hits", groupStd, "Singles + doubles + triples + homers", func(p store.PlayerBatting) int { return p.H }),
		num("1B", groupStd, "Singles", func(p store.PlayerBatting) int { return p.Singles() }),
		num("2B", groupStd, "Doubles", func(p store.PlayerBatting) int { return p.B2 }),
		num("3B", groupStd, "Triples", func(p store.PlayerBatting) int { return p.B3 }),
		num("HR", groupStd, "Home runs", func(p store.PlayerBatting) int { return p.HR }),
		num("RBI", groupStd, "Runs batted in", func(p store.PlayerBatting) int { return p.RBI }),
		num("BB", groupStd, "Walks", func(p store.PlayerBatting) int { return p.BB }),
		num("SO", groupStd, "Strikeouts", func(p store.PlayerBatting) int { return p.SO }),
		num("SF", groupStd, "Sacrifice flies (recorded, but not counted in PA)", func(p store.PlayerBatting) int { return p.SF }),
		num("E", groupStd, "Errors", func(p store.PlayerBatting) int { return p.E }),
		pct("Avg", groupStd, "Batting average: hits / at bats", func(p store.PlayerBatting) float64 { return p.AVG() }),
		pct("OB%", groupStd, "On base percentage: (hits + walks) / plate appearances", func(p store.PlayerBatting) float64 { return p.OBP() }),
		pct("Slug", groupStd, "Slugging: total bases / at bats", func(p store.PlayerBatting) float64 { return p.SLG() }),
		pct("OPS", groupStd, "On base plus slugging", func(p store.PlayerBatting) float64 { return p.OPS() }),

		// --- advanced -------------------------------------------------------
		num("OB", groupAdv, "Times on base: hits + walks (fielder's choices don't count)", func(p store.PlayerBatting) int { return p.OB() }),
		qty("HRF", groupAdv, "Home run frequency: at bats per home run (lower is better)", func(p store.PlayerBatting) float64 { return p.HRF() }),
		pct("SlugBB", groupAdv, "Slugging with walks counted as a base, over plate appearances", func(p store.PlayerBatting) float64 { return p.SlugBB() }),
		qty("RP7", groupAdv, "Runs per 7 innings if this player took every at bat", func(p store.PlayerBatting) float64 { return p.RP7() }),
		pct("2B%", groupAdv, "Doubles / at bats", func(p store.PlayerBatting) float64 { return p.Pct2B() }),
		pct("3B%", groupAdv, "Triples / at bats", func(p store.PlayerBatting) float64 { return p.Pct3B() }),
		pct("HR%", groupAdv, "Home runs / at bats", func(p store.PlayerBatting) float64 { return p.PctHR() }),
		pct("BB%", groupAdv, "Walks / plate appearances", func(p store.PlayerBatting) float64 { return p.PctBB() }),
		pct("RBIpAB", groupAdv, "RBI per at bat", func(p store.PlayerBatting) float64 { return p.RBIPerAB() }),
		num("RProd", groupAdv, "Runs produced: RBI + runs - home runs", func(p store.PlayerBatting) int { return p.RProd() }),
		pct("OE", groupAdv, "Offensive efficiency: runs produced / plate appearances", func(p store.PlayerBatting) float64 { return p.OE() }),
		pct("AVGnoHR", groupAdv, "Batting average with home runs removed from both sides", func(p store.PlayerBatting) float64 { return p.AVGNoHR() }),
		pct("OBnoHR", groupAdv, "On base percentage with home runs removed from both sides", func(p store.PlayerBatting) float64 { return p.OBNoHR() }),
	}

	// Columns where leading means nothing, or means the wrong thing. Playing
	// time is attendance, not hitting; and most of the roster has no
	// strikeouts, errors or sac flies, so a leader there marks a coincidence.
	for i := range cols {
		switch cols[i].Label {
		case "GM", "PA", "AB", "SF", "SO", "E":
			cols[i].Best = bestNone
		case "HRF":
			cols[i].Best = bestLow // at-bats per home run: fewer is better
		}
	}
	return cols
}

// statTable is the context a row needs that only the whole table knows: the
// leading value in each column, and whether this is a season table or one
// game's box score, which changes what leading is worth marking.
type statTable struct {
	Best   map[string]float64
	Season bool
}

// newStatTable finds the leader in every markable column.
//
// Counting leaders come from everyone: most home runs is most home runs,
// however many trips it took. Rate leaders come from qualified players only, so
// a 1.000 average on three at-bats can't lead the team — and in a box score
// they aren't marked at all, because over one night a 1-for-1 ties a 4-for-4
// and marking both says nothing.
func newStatTable(rows []store.PlayerBatting, season bool) statTable {
	t := statTable{Best: map[string]float64{}, Season: season}
	for _, c := range statColumns() {
		if c.Best == bestNone || c.Value == nil || (c.Rate && !season) {
			continue
		}
		var top float64
		found := false
		for _, row := range rows {
			if !t.eligible(c, row) {
				continue
			}
			v := c.Value(row)
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			if !found || (c.Best == bestHigh && v > top) || (c.Best == bestLow && v < top) {
				top, found = v, true
			}
		}
		// Nobody leads a category at zero. Without this, a season with no
		// triples would light up every player in the 3B column.
		if found && top != 0 {
			t.Best[c.Label] = top
		}
	}
	return t
}

func (t statTable) eligible(c statColumn, row store.PlayerBatting) bool {
	if isTeamTotals(row) {
		return false
	}
	return !(c.Rate && !qualified(row.Batting))
}

// leads reports whether this cell holds its column's leading value. Ties all
// light up: two players with the same number both led the team in it.
func (t statTable) leads(c statColumn, row store.PlayerBatting) bool {
	top, ok := t.Best[c.Label]
	if !ok || c.Value == nil || !t.eligible(c, row) {
		return false
	}
	return c.Value(row) == top
}

// sortKind is the data-sort value the click-to-sort script reads.
func (c statColumn) sortKind() string {
	if c.Text {
		return "text"
	}
	return "num"
}

// teamRow adapts a summed team line to the row shape the columns expect. GM is
// meaningless for a team total, so it renders as a dash.
func teamRow(b store.Batting) store.PlayerBatting {
	return store.PlayerBatting{Player: store.Player{Name: "Team"}, Batting: b, Games: -1}
}

// isTeamTotals reports whether a row is the synthetic team line.
func isTeamTotals(p store.PlayerBatting) bool { return p.Games < 0 }

// cellText renders a column for a row, blanking GM on the team totals line.
func cellText(c statColumn, p store.PlayerBatting) string {
	if isTeamTotals(p) && (c.Label == "GM" || c.Label == "Player") {
		if c.Label == "Player" {
			return "Team"
		}
		return "—"
	}
	return c.Cell(p)
}

// headerClass styles a stats header cell; advanced columns start hidden.
func headerClass(c statColumn) string {
	base := "cursor-pointer border border-white px-2 py-2 whitespace-nowrap "
	if c.Text {
		base += "text-left "
	}
	if c.Group == groupAdv {
		base += "hidden "
	}
	return base
}

// cellClass styles a stats cell: advanced columns start hidden, and zeroes are
// dimmed so the numbers that matter stand out across a very wide table.
func cellClass(c statColumn, p store.PlayerBatting, leads bool) string {
	base := "px-2 py-1 whitespace-nowrap "
	if c.Text {
		base += "text-left "
	}
	if c.Group == groupAdv {
		base += "hidden "
	}
	if isTeamTotals(p) {
		return base + "font-bold"
	}
	if leads {
		return base + "stat-lead"
	}
	if v := c.Cell(p); v == "0" {
		base += "text-gray-600 "
	}
	return base
}

// rowClass styles a stats row, separating the team totals line from the players.
func rowClass(p store.PlayerBatting) string {
	if isTeamTotals(p) {
		return "border-t-2 border-white font-bold"
	}
	return "border-b border-gray-700"
}

// statPair is one label/value pair for the player page's summary grid.
type statPair struct {
	Label string
	Value string
	Help  string
	Rate  bool // rate stats are emphasised over raw counts
}

// statSummary renders every stat in the league key for one line, reusing the
// table's column definitions so the player page and the leaderboard can never
// disagree about a formula.
func statSummary(b store.Batting, games int) []statPair {
	row := store.PlayerBatting{Batting: b, Games: games}
	var out []statPair
	for _, c := range statColumns() {
		if c.Label == "Player" {
			continue
		}
		out = append(out, statPair{
			Label: c.Label,
			Value: c.Cell(row),
			Help:  c.Help,
			Rate:  c.Group == groupAdv || isRateLabel(c.Label),
		})
	}
	return out
}

func isRateLabel(label string) bool {
	switch label {
	case "Avg", "OB%", "Slug", "OPS":
		return true
	}
	return false
}

// summaryValueClass emphasises rate stats over raw counts on the player page.
func summaryValueClass(p statPair) string {
	if p.Rate {
		return "text-base font-black"
	}
	return "text-base font-semibold text-gray-300"
}
