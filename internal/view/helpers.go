package view

import (
	"fmt"
	"strconv"

	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

// matchup renders a game from Coors Heavy's perspective: "vs X" at home, "@ X" away.
func matchup(g store.Game) string {
	if g.Home {
		return "vs " + g.Opponent
	}
	return "@ " + g.Opponent
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
