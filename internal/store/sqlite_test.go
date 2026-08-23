package store

import (
	"context"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *SQLite {
	t.Helper()
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// A fresh database migrates, creates season 1, and seeds the roster onto it.
// The schedule is deliberately NOT seeded — it loads from CSV.
func TestFreshDatabase(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	season, err := st.CurrentSeason(ctx)
	if err != nil {
		t.Fatalf("CurrentSeason: %v", err)
	}
	if season.Year != 2026 {
		t.Errorf("season year = %d, want 2026", season.Year)
	}

	roster, err := st.ListRoster(ctx, season.ID)
	if err != nil {
		t.Fatalf("ListRoster: %v", err)
	}
	if len(roster) != len(seedRoster) {
		t.Errorf("roster = %d players, want %d", len(roster), len(seedRoster))
	}

	// Duplicate seed names must still produce unique slugs.
	seen := map[string]bool{}
	for _, p := range roster {
		if p.Slug == "" {
			t.Errorf("%s has an empty slug", p.Name)
		}
		if seen[p.Slug] {
			t.Errorf("duplicate slug %q", p.Slug)
		}
		seen[p.Slug] = true
	}

	games, err := st.ListGames(ctx, season.ID)
	if err != nil {
		t.Fatalf("ListGames: %v", err)
	}
	if len(games) != 0 {
		t.Errorf("fresh database has %d games, want 0 (the schedule loads from CSV)", len(games))
	}
}

// The whole point of the season model: work on season 2 must not touch season 1.
func TestSeasonsAreIsolated(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	s1, _ := st.CurrentSeason(ctx)
	g1, err := st.CreateGame(ctx, Game{SeasonID: s1.ID, SortOrder: 1, PlayedOn: "2026-05-14", Opponent: "Hailraisers", Played: true, UsScore: 13, ThemScore: 12})
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	roster, _ := st.ListRoster(ctx, s1.ID)
	player := roster[0]
	if err := st.UpsertBattingLine(ctx, BattingLine{GameID: g1.ID, PlayerID: player.ID, Batting: Batting{AB: 4, R: 2, H: 3}}); err != nil {
		t.Fatalf("UpsertBattingLine: %v", err)
	}
	if err := st.SetBeerRacks(ctx, s1.ID, player.ID, 2); err != nil {
		t.Fatalf("SetBeerRacks: %v", err)
	}

	s2, err := st.CreateSeason(ctx, Season{Name: "2027", Year: 2027})
	if err != nil {
		t.Fatalf("CreateSeason: %v", err)
	}
	if _, err := st.CarryRosterForward(ctx, s1.ID, s2.ID); err != nil {
		t.Fatalf("CarryRosterForward: %v", err)
	}
	if _, err := st.CreateGame(ctx, Game{SeasonID: s2.ID, SortOrder: 1, PlayedOn: "2027-05-13", Opponent: "Hailraisers"}); err != nil {
		t.Fatalf("CreateGame season 2: %v", err)
	}

	// Beer resets in the new season but is preserved in the old one.
	old, err := st.GetRosterPlayer(ctx, s1.ID, player.ID)
	if err != nil {
		t.Fatalf("GetRosterPlayer season 1: %v", err)
	}
	if old.BeerRacks != 2 {
		t.Errorf("season 1 beer = %v, want 2", old.BeerRacks)
	}
	fresh, err := st.GetRosterPlayer(ctx, s2.ID, player.ID)
	if err != nil {
		t.Fatalf("GetRosterPlayer season 2: %v", err)
	}
	if fresh.BeerRacks != 0 {
		t.Errorf("season 2 beer = %v, want 0", fresh.BeerRacks)
	}

	// Clearing season 2's schedule must leave season 1 and its stats alone.
	if err := st.DeleteGamesInSeason(ctx, s2.ID); err != nil {
		t.Fatalf("DeleteGamesInSeason: %v", err)
	}
	games, _ := st.ListGames(ctx, s1.ID)
	if len(games) != 1 {
		t.Fatalf("season 1 has %d games after clearing season 2, want 1", len(games))
	}
	lines, _ := st.ListBattingForGame(ctx, g1.ID)
	if len(lines) != 1 {
		t.Errorf("season 1 batting lines = %d, want 1", len(lines))
	}
}

// Foreign keys are enforced (PRAGMA foreign_keys is set in the DSN), so
// deleting a game takes its batting lines with it. That cascade is exactly why
// import-schedule updates games in place instead of deleting them.
func TestDeletingAGameCascadesToStats(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	s, _ := st.CurrentSeason(ctx)
	roster, _ := st.ListRoster(ctx, s.ID)

	g, _ := st.CreateGame(ctx, Game{SeasonID: s.ID, PlayedOn: "2026-05-14", Opponent: "Hailraisers"})
	if err := st.UpsertBattingLine(ctx, BattingLine{GameID: g.ID, PlayerID: roster[0].ID, Batting: Batting{AB: 4, H: 2}}); err != nil {
		t.Fatalf("UpsertBattingLine: %v", err)
	}
	if err := st.DeleteGame(ctx, g.ID); err != nil {
		t.Fatalf("DeleteGame: %v", err)
	}
	lines, _ := st.ListBattingForGame(ctx, g.ID)
	if len(lines) != 0 {
		t.Errorf("batting lines survived the game delete: %d", len(lines))
	}
}

// Re-importing a corrected line updates rather than duplicating.
func TestUpsertBattingLineIsIdempotent(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	s, _ := st.CurrentSeason(ctx)
	roster, _ := st.ListRoster(ctx, s.ID)
	g, _ := st.CreateGame(ctx, Game{SeasonID: s.ID, PlayedOn: "2026-05-14", Opponent: "Hailraisers"})

	for _, h := range []int{2, 3} {
		if err := st.UpsertBattingLine(ctx, BattingLine{GameID: g.ID, PlayerID: roster[0].ID, Batting: Batting{AB: 4, H: h}}); err != nil {
			t.Fatalf("UpsertBattingLine: %v", err)
		}
	}
	lines, _ := st.ListBattingForGame(ctx, g.ID)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].H != 3 {
		t.Errorf("H = %d, want the corrected 3", lines[0].H)
	}
}

// A stats import lands completely or not at all.
func TestImportBattingLinesIsAtomic(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	s, _ := st.CurrentSeason(ctx)
	roster, _ := st.ListRoster(ctx, s.ID)
	g, _ := st.CreateGame(ctx, Game{SeasonID: s.ID, PlayedOn: "2026-05-14", Opponent: "Hailraisers"})

	// The second line references a player that doesn't exist, so the whole
	// batch must roll back.
	err := st.ImportBattingLines(ctx, []BattingLine{
		{GameID: g.ID, PlayerID: roster[0].ID, Batting: Batting{AB: 4, H: 2}},
		{GameID: g.ID, PlayerID: 999999, Batting: Batting{AB: 3, H: 1}},
	}, nil)
	if err == nil {
		t.Fatal("expected a foreign key error")
	}
	lines, _ := st.ListBattingForGame(ctx, g.ID)
	if len(lines) != 0 {
		t.Errorf("partial import left %d lines behind", len(lines))
	}
}

func TestSetCurrentSeasonMovesTheFlag(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	s1, _ := st.CurrentSeason(ctx)
	s2, _ := st.CreateSeason(ctx, Season{Name: "2027", Year: 2027})

	if err := st.SetCurrentSeason(ctx, s2.ID); err != nil {
		t.Fatalf("SetCurrentSeason: %v", err)
	}
	seasons, _ := st.ListSeasons(ctx)
	var current int
	for _, s := range seasons {
		if s.IsCurrent {
			current++
			if s.ID != s2.ID {
				t.Errorf("current season = %d, want %d", s.ID, s2.ID)
			}
		}
	}
	if current != 1 {
		t.Errorf("%d seasons marked current, want exactly 1", current)
	}
	if _, err := st.GetSeason(ctx, s1.ID); err != nil {
		t.Errorf("the old season should still be readable: %v", err)
	}
}

// Adding a player whose name is already taken must work: this roster keeps
// producing duplicate first names, and players.slug is uniquely indexed.
func TestCreatePlayerUniquesTheSlug(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	// A name that isn't in the seed roster, so the first one gets the bare slug.
	var slugs []string
	for i := 0; i < 3; i++ {
		p, err := st.CreatePlayer(ctx, "Tucker")
		if err != nil {
			t.Fatalf("CreatePlayer #%d: %v", i+1, err)
		}
		if p.Name != "Tucker" {
			t.Errorf("Name = %q, want Tucker", p.Name)
		}
		slugs = append(slugs, p.Slug)
	}
	want := []string{"tucker", "tucker-2", "tucker-3"}
	for i := range want {
		if slugs[i] != want[i] {
			t.Errorf("slug %d = %q, want %q", i, slugs[i], want[i])
		}
	}

	// A name that collides with the seed roster's duplicates must also settle.
	p, err := st.CreatePlayer(ctx, "Patty")
	if err != nil {
		t.Fatalf("CreatePlayer(Patty): %v", err)
	}
	if p.Slug == "" || p.Slug == "patty" {
		t.Errorf("slug = %q, want a suffixed slug (patty is already taken)", p.Slug)
	}
}

func TestCreatePlayerRejectsAnEmptyName(t *testing.T) {
	st := open(t)
	if _, err := st.CreatePlayer(context.Background(), "  !!!  "); err == nil {
		t.Error("expected an error for a name with no letters or digits")
	}
}

// A new player joins the current season's roster, benched and absent.
func TestAddToRosterPutsThemAtTheEnd(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	s, _ := st.CurrentSeason(ctx)

	before, _ := st.ListRoster(ctx, s.ID)
	p, err := st.CreatePlayer(ctx, "Tucker")
	if err != nil {
		t.Fatalf("CreatePlayer: %v", err)
	}
	if err := st.AddToRoster(ctx, s.ID, p.ID); err != nil {
		t.Fatalf("AddToRoster: %v", err)
	}
	// Adding twice must not duplicate the roster spot.
	if err := st.AddToRoster(ctx, s.ID, p.ID); err != nil {
		t.Fatalf("AddToRoster twice: %v", err)
	}

	after, _ := st.ListRoster(ctx, s.ID)
	if len(after) != len(before)+1 {
		t.Fatalf("roster went from %d to %d, want +1", len(before), len(after))
	}
	rp, err := st.GetRosterPlayer(ctx, s.ID, p.ID)
	if err != nil {
		t.Fatalf("GetRosterPlayer: %v", err)
	}
	if rp.Position != "Bench" || rp.Attended || rp.BeerRacks != 0 {
		t.Errorf("new player = %+v, want benched, absent, no beer", rp)
	}
	if rp.LineupOrder <= before[len(before)-1].LineupOrder {
		t.Errorf("lineup_order = %d, want after the existing roster", rp.LineupOrder)
	}
}
