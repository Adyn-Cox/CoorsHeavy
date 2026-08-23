package store

import (
	"strings"
	"testing"
)

func TestParseStatsCSV(t *testing.T) {
	in := `date,opponent,player,ab,r,h,2b,3b,hr,rbi,bb,so,sf,e
2026-05-14,Hailraisers,cooper,4,2,3,1,0,1,4,0,0,0,0
2026-05-14,Hailraisers,patty-10,3,0,0,0,0,0,0,0,2,0,1
`
	rows, err := ParseStatsCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseStatsCSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	got := rows[0]
	if got.Date != "2026-05-14" || got.Opponent != "Hailraisers" || got.Player != "cooper" {
		t.Errorf("keys = %+v", got)
	}
	want := Batting{AB: 4, R: 2, H: 3, B2: 1, HR: 1, RBI: 4}
	if got.Batting != want {
		t.Errorf("Batting = %+v, want %+v", got.Batting, want)
	}
}

// Column order must not matter, extra columns are ignored, headers are
// case-insensitive, blank cells are zero, and #-comments are skipped — a
// spreadsheet export is not going to match our exact column order.
func TestParseStatsCSVIsForgiving(t *testing.T) {
	in := `# exported from somewhere else
PLAYER,Date,Opponent,H,AB,notes,R
cooper,2026-05-14,Hailraisers,3,4,who cares,

`
	rows, err := ParseStatsCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseStatsCSV: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].AB != 4 || rows[0].H != 3 || rows[0].R != 0 {
		t.Errorf("Batting = %+v", rows[0].Batting)
	}
}

// All-zero rows mean "didn't bat" and must not become a game played.
func TestParseStatsCSVSkipsEmptyLines(t *testing.T) {
	in := `date,opponent,player,ab,r,h
2026-05-14,Hailraisers,cooper,0,0,0
2026-05-14,Hailraisers,cade,3,1,1
`
	rows, err := ParseStatsCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseStatsCSV: %v", err)
	}
	if len(rows) != 1 || rows[0].Player != "cade" {
		t.Fatalf("got %+v, want only cade", rows)
	}
}

// Every problem in the file should surface at once: transcribing a scorebook
// produces mistakes in batches, and fixing them one run at a time is miserable.
func TestParseStatsCSVReportsEveryProblem(t *testing.T) {
	in := `date,opponent,player,ab,r,h,2b
2026-13-99,Hailraisers,cooper,4,2,3,1
2026-05-14,Hailraisers,,4,2,3,1
2026-05-14,Hailraisers,cade,3,1,9,0
2026-05-14,Hailraisers,luke,3,1,x,0
`
	_, err := ParseStatsCSV(strings.NewReader(in))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{
		"is not YYYY-MM-DD",
		"player is blank",
		"H (9) cannot exceed AB (3)",
		"is not a whole number",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
}

func TestParseStatsCSVRequiresKeyColumns(t *testing.T) {
	_, err := ParseStatsCSV(strings.NewReader("date,opponent,ab\n2026-05-14,Hailraisers,4\n"))
	if err == nil || !strings.Contains(err.Error(), `missing required column "player"`) {
		t.Fatalf("got %v", err)
	}
}

func TestParseScheduleCSVRejectsBadRows(t *testing.T) {
	_, err := ParseScheduleCSV(strings.NewReader(
		"date,time,opponent\n2026-05-14,9:99 PM,Hailraisers\n"), 1)
	if err == nil || !strings.Contains(err.Error(), "is not one of") {
		t.Fatalf("got %v", err)
	}
}

// A "played" row with no score is a postponement placeholder, not a 0-0 tie.
func TestParseScheduleCSVTreatsBlankScoreAsUnplayed(t *testing.T) {
	games, err := ParseScheduleCSV(strings.NewReader(
		"date,opponent,played,us,them\n2026-05-14,Hailraisers,1,,\n"), 1)
	if err != nil {
		t.Fatalf("ParseScheduleCSV: %v", err)
	}
	if games[0].Played {
		t.Error("blank score with played=1 should be treated as unplayed")
	}
}

func TestOpponentsSkipsTBD(t *testing.T) {
	got := Opponents([]Game{
		{Opponent: "Hailraisers"},
		{Opponent: "Hailraisers"},
		{Opponent: TBDOpponent},
		{Opponent: "Big Sticks"},
		{Opponent: ""},
	})
	if len(got) != 2 || got[0] != "Hailraisers" || got[1] != "Big Sticks" {
		t.Errorf("Opponents() = %q", got)
	}
}
