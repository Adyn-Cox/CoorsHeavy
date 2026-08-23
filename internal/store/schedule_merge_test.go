package store

import (
	"strings"
	"testing"
)

// The embedded schedule must match the season as it actually finished, so a
// fresh database loads real results rather than a half-played snapshot.
func TestEmbeddedScheduleMatchesTheFinishedSeason(t *testing.T) {
	b, err := EmbeddedSchedule("Summer 2026")
	if err != nil {
		t.Fatalf("EmbeddedSchedule: %v", err)
	}
	games, err := ParseScheduleCSV(strings.NewReader(string(b)), 1)
	if err != nil {
		t.Fatalf("ParseScheduleCSV: %v", err)
	}

	var w, l int
	for _, g := range games {
		if !g.Played {
			t.Errorf("%s vs %s has no result — the 2026 season is over", g.Date, g.Opponent)
			continue
		}
		switch {
		case g.UsScore > g.ThemScore:
			w++
		case g.UsScore < g.ThemScore:
			l++
		}
	}
	if w != 8 || l != 4 {
		t.Errorf("record = %d-%d, want 8-4", w, l)
	}

	// Playoff seeding was settled, so neither post-season game is still TBD.
	for _, g := range games {
		if g.Playoff && g.Opponent == TBDOpponent {
			t.Errorf("playoff game on %s is still TBD", g.PlayedOn)
		}
	}

	// The 5/21 score was corrected on the site from 5-13 to 3-13; the file has
	// to carry the corrected figure, not the original.
	for _, g := range games {
		if g.PlayedOn == "2026-05-21" && g.UsScore != 3 {
			t.Errorf("5/21 us_score = %d, want the corrected 3", g.UsScore)
		}
	}
}
