package store

import (
	"math"
	"strings"
	"testing"
)

// A full slow-pitch line worked out by hand against the league's stat key.
// 7 AB, 1 BB. 4 hits: 2 singles, a double, a home run. 3 runs, 5 RBI.
//
//	1B     = 4 - 1 - 0 - 1 = 2
//	TB     = 2 + 2(1) + 3(0) + 4(1) = 8
//	PA     = 7 + 1 = 8          (at bats + walks; no sac-fly bucket)
//	OB     = 4 + 1 = 5          (hits + walks)
//	AVG    = 4/7  = .571
//	OB%    = 5/8  = .625
//	Slug   = 8/7  = 1.143
//	SlugBB = (1+8)/8 = 1.125
//	HRF    = 7/1  = 7.0
//	RP7    = ((21/(1-.625)) * 1.143)/3 = 21.33
//	RProd  = 5 + 3 - 1 = 7
//	OE     = 7/8  = .875
//	AVGnoHR= (4-1)/(7-1) = .500
//	OBnoHR = (5-1)/(8-1) = .571
func TestBattingRatesFollowTheLeagueKey(t *testing.T) {
	b := Batting{AB: 7, R: 3, H: 4, B2: 1, HR: 1, RBI: 5, BB: 1, SO: 1, SF: 1}

	for _, tt := range []struct {
		name string
		got  int
		want int
	}{
		{"Singles", b.Singles(), 2},
		{"TB", b.TB(), 8},
		{"PA", b.PA(), 8},
		{"OB", b.OB(), 5},
		{"RProd", b.RProd(), 7},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}

	assertRate(t, "AVG", b.AVG(), 4.0/7.0)
	assertRate(t, "OB%", b.OBP(), 5.0/8.0)
	assertRate(t, "Slug", b.SLG(), 8.0/7.0)
	assertRate(t, "SlugBB", b.SlugBB(), 9.0/8.0)
	assertRate(t, "HRF", b.HRF(), 7.0)
	assertRate(t, "RP7", b.RP7(), ((21/(1-0.625))*(8.0/7.0))/3)
	assertRate(t, "2B%", b.Pct2B(), 1.0/7.0)
	assertRate(t, "3B%", b.Pct3B(), 0.0)
	assertRate(t, "HR%", b.PctHR(), 1.0/7.0)
	assertRate(t, "BB%", b.PctBB(), 1.0/8.0)
	assertRate(t, "RBIpAB", b.RBIPerAB(), 5.0/7.0)
	assertRate(t, "OE", b.OE(), 7.0/8.0)
	assertRate(t, "AVGnoHR", b.AVGNoHR(), 0.5)
	assertRate(t, "OBnoHR", b.OBNoHR(), 4.0/7.0)
}

// Sacrifice flies are recorded but, per the league key, do not get their own
// bucket: PA is at-bats plus walks only. This is the one place the key departs
// from standard baseball, so pin it.
func TestSacFliesDoNotEnterPlateAppearances(t *testing.T) {
	withSF := Batting{AB: 4, H: 1, BB: 1, SF: 3}
	if got, want := withSF.PA(), 5; got != want {
		t.Errorf("PA() = %d, want %d — sac flies must not inflate PA", got, want)
	}
	assertRate(t, "OB%", withSF.OBP(), 2.0/5.0)
}

// A player who has never made an out has no defined runs-per-7: the estimate
// needs 21 outs and never gets there.
func TestRP7UndefinedWhenAlwaysOnBase(t *testing.T) {
	if got := (Batting{AB: 2, H: 2}).RP7(); !math.IsNaN(got) {
		t.Errorf("RP7() = %v, want NaN for a 1.000 on-base rate", got)
	}
}

// HRF is at-bats per home run, so it is undefined until one is hit — not zero,
// which would read as "a homer every no at-bats".
func TestHRFUndefinedWithoutHomers(t *testing.T) {
	if got := (Batting{AB: 20, H: 6}).HRF(); !math.IsNaN(got) {
		t.Errorf("HRF() = %v, want NaN", got)
	}
	assertRate(t, "HRF", Batting{AB: 20, HR: 4, H: 4}.HRF(), 5.0)
}

// Removing home runs from both sides can empty a rate entirely: a player whose
// every at-bat was a home run has no non-homer at-bats to average.
func TestNoHRRatesWhenEveryHitIsAHomer(t *testing.T) {
	b := Batting{AB: 3, H: 3, HR: 3, R: 3, RBI: 3}
	if got := b.AVGNoHR(); !math.IsNaN(got) {
		t.Errorf("AVGNoHR() = %v, want NaN", got)
	}
	// PA is 3 and all three were homers, so OBnoHR has nothing left either.
	if got := b.OBNoHR(); !math.IsNaN(got) {
		t.Errorf("OBNoHR() = %v, want NaN", got)
	}
}

// Slow pitch has no hit-by-pitch, so OB% must not reserve room for one: a
// player who only ever walks still reaches base every time.
func TestOBPWithoutHitByPitch(t *testing.T) {
	b := Batting{AB: 0, BB: 3}
	assertRate(t, "OB%", b.OBP(), 1.0)
}

// An empty line must not divide by zero. Every rate is undefined rather than
// .000, so a benched player renders as "—" instead of looking like a terrible
// hitter.
func TestBattingZeroDivision(t *testing.T) {
	var b Batting
	for name, got := range map[string]float64{
		"AVG": b.AVG(), "OBP": b.OBP(), "SLG": b.SLG(), "SlugBB": b.SlugBB(),
		"HRF": b.HRF(), "RP7": b.RP7(), "Pct2B": b.Pct2B(), "Pct3B": b.Pct3B(),
		"PctHR": b.PctHR(), "PctBB": b.PctBB(), "RBIPerAB": b.RBIPerAB(),
		"OE": b.OE(), "AVGNoHR": b.AVGNoHR(), "OBNoHR": b.OBNoHR(),
	} {
		if !math.IsNaN(got) {
			t.Errorf("%s() on an empty line = %v, want NaN", name, got)
		}
	}
	if !b.Empty() {
		t.Error("Empty() = false on a zero line")
	}
}

func TestBattingAdd(t *testing.T) {
	total := Batting{AB: 4, H: 2, HR: 1}
	total.Add(Batting{AB: 3, H: 1, BB: 1})
	if total.AB != 7 || total.H != 3 || total.HR != 1 || total.BB != 1 {
		t.Errorf("Add() = %+v", total)
	}
}

func TestBattingValidate(t *testing.T) {
	tests := []struct {
		name    string
		b       Batting
		wantErr string
	}{
		{"valid", Batting{AB: 4, H: 2, B2: 1}, ""},
		{"more hits than at-bats", Batting{AB: 3, H: 5}, "H (5) cannot exceed AB (3)"},
		{"extra-base hits exceed hits", Batting{AB: 4, H: 2, B2: 2, B3: 1}, "2B+3B+HR (3) cannot exceed H (2)"},
		{"more strikeouts than at-bats", Batting{AB: 2, SO: 3}, "SO (3) cannot exceed AB (2)"},
		{"negative", Batting{E: -1}, "E cannot be negative (got -1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.b.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Validate() = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// Slugs are the stats CSV's player key, so they have to be stable and safe.
func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Cooper":      "cooper",
		"  Patty M. ": "patty-m",
		"Jo-Jo":       "jo-jo",
		"Big Sticks":  "big-sticks",
		"!!!":         "",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSeasonNoteLines(t *testing.T) {
	s := Season{Notes: "first\n\n  second  \n"}
	got := s.NoteLines()
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Errorf("NoteLines() = %q", got)
	}
}

func TestGameResultAndLabel(t *testing.T) {
	tests := []struct {
		g    Game
		want string
	}{
		{Game{Played: true, UsScore: 13, ThemScore: 12}, "W 13-12"},
		{Game{Played: true, UsScore: 5, ThemScore: 13}, "L 5-13"},
		{Game{Played: true, UsScore: 7, ThemScore: 7}, "T 7-7"},
		{Game{Played: false}, ""},
	}
	for _, tt := range tests {
		if got := tt.g.Result(); got != tt.want {
			t.Errorf("Result() = %q, want %q", got, tt.want)
		}
	}
	// Label falls back to the ISO date when the league label is missing.
	if got := (Game{PlayedOn: "2026-05-14"}).Label(); got != "2026-05-14" {
		t.Errorf("Label() fallback = %q", got)
	}
	if got := (Game{Date: "Thu 5/14", PlayedOn: "2026-05-14"}).Label(); got != "Thu 5/14" {
		t.Errorf("Label() = %q", got)
	}
}

// The 2026 schedule is all Thursdays; deriving the label from the ISO date is
// what caught the season having been recorded as 2025, where every one of those
// dates is a Wednesday.
func TestFormatGameDate(t *testing.T) {
	if got, want := FormatGameDate("2026-05-14"), "Thu 5/14"; got != want {
		t.Errorf("FormatGameDate() = %q, want %q", got, want)
	}
	if got, want := FormatGameDate("garbage"), "garbage"; got != want {
		t.Errorf("FormatGameDate() on bad input = %q, want %q", got, want)
	}
}

// Every embedded schedule must be Thursdays only. This is the check that
// caught a whole season written against the wrong year: 2025-05-14 is a
// Wednesday, and the dates only reveal it once formatted.
func TestEmbeddedSchedulesAreEveryThursday(t *testing.T) {
	for _, season := range []struct {
		name  string
		games int
	}{
		{"Summer 2026", 12},
		{"Fall 2026", 10},
	} {
		b, err := EmbeddedSchedule(season.name)
		if err != nil {
			t.Fatalf("EmbeddedSchedule(%q): %v", season.name, err)
		}
		games, err := ParseScheduleCSV(strings.NewReader(string(b)), 1)
		if err != nil {
			t.Fatalf("ParseScheduleCSV(%q): %v", season.name, err)
		}
		if len(games) != season.games {
			t.Fatalf("%s: got %d games, want %d", season.name, len(games), season.games)
		}
		for _, g := range games {
			if !strings.HasPrefix(g.Date, "Thu ") {
				t.Errorf("%s: %s (%s) is not a Thursday — check the year", season.name, g.PlayedOn, g.Date)
			}
		}
	}
}

func TestEmbeddedSummerScheduleMatchesTheFinishedSeason(t *testing.T) {
	b, err := EmbeddedSchedule("Summer 2026")
	if err != nil {
		t.Fatalf("EmbeddedSchedule: %v", err)
	}
	games, err := ParseScheduleCSV(strings.NewReader(string(b)), 1)
	if err != nil {
		t.Fatalf("ParseScheduleCSV: %v", err)
	}
	if games[0].Opponent != "Hailraisers" || games[0].Home || !games[0].Played {
		t.Errorf("first game = %+v", games[0])
	}
	if games[0].UsScore != 13 || games[0].ThemScore != 12 {
		t.Errorf("first game score = %d-%d, want 13-12", games[0].UsScore, games[0].ThemScore)
	}
	if !games[11].Playoff {
		t.Error("last game should be a playoff game")
	}
}

// The fall playoff row carries no opponent and no time: the bracket seeds by
// final standing, so both are unknown until the regular season ends.
func TestFallPlayoffRowIsUnseeded(t *testing.T) {
	b, err := EmbeddedSchedule("Fall 2026")
	if err != nil {
		t.Fatalf("EmbeddedSchedule: %v", err)
	}
	games, err := ParseScheduleCSV(strings.NewReader(string(b)), 2)
	if err != nil {
		t.Fatalf("ParseScheduleCSV: %v", err)
	}
	last := games[len(games)-1]
	if !last.Playoff {
		t.Fatal("last fall game should be a playoff game")
	}
	if last.Opponent != TBDOpponent {
		t.Errorf("playoff opponent = %q, want %q", last.Opponent, TBDOpponent)
	}
	if last.Time != "" {
		t.Errorf("playoff time = %q, want blank until seeding", last.Time)
	}
	// Two of the nine regular-season games are in the 9 PM slot the fall
	// league added, so ValidGameTime has to accept it.
	var nine int
	for _, g := range games {
		if g.Time == "9:00 PM" {
			nine++
		}
	}
	if nine != 2 {
		t.Errorf("got %d games at 9:00 PM, want 2", nine)
	}
}

func assertRate(t *testing.T, name string, got, want float64) {
	t.Helper()
	// NaN compares false against everything, so an accidental NaN would slip
	// through a plain tolerance check and silently "pass".
	if math.IsNaN(got) {
		t.Errorf("%s() = NaN, want %v", name, want)
		return
	}
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s() = %v, want %v", name, got, want)
	}
}

// The seed roster has two Patty and two Parker, and players.slug is uniquely
// indexed — collisions must be settled before the insert, not after it.
func TestUniqueSlugs(t *testing.T) {
	got := UniqueSlugs([]string{"Cooper", "Patty", "Parker", "Patty", "Parker", "!!!", "!!!"})
	want := []string{"cooper", "patty", "parker", "patty-2", "parker-2", "player", "player-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("UniqueSlugs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	seen := map[string]bool{}
	for _, s := range UniqueSlugs(seedRoster) {
		if seen[s] {
			t.Errorf("duplicate slug %q from the seed roster", s)
		}
		seen[s] = true
	}
}

// Forrest is on the roster. Guards against a bad merge quietly dropping him.
func TestSeedRosterIncludesForrest(t *testing.T) {
	var found bool
	for _, name := range seedRoster {
		if name == "Forrest" {
			found = true
		}
	}
	if !found {
		t.Error("Forrest is missing from the seed roster")
	}
}
