// Command import-schedule loads a season's schedule from CSV.
//
// It updates games in place rather than wiping the table. That matters: it used
// to DELETE every game and re-insert, which handed out fresh autoincrement ids
// on each run. Now that batting_lines references games with ON DELETE CASCADE,
// the old behaviour would have silently deleted every recorded stat.
//
// Each CSV row is matched to an existing game by (opponent, home) for regular
// season games — unique within a season, so a postponed game keeps its result
// and its stats even though its date moved — and by position for playoff games,
// whose opponent is TBD until seeding is final.
//
// Results recorded in the app always win over the CSV's, so re-importing a
// schedule can never revert a score you corrected on the site. The CSV's
// scores only apply to games the database has no result for. Pass -fresh to
// take the file verbatim instead.
//
//	go run ./cmd/import-schedule -season 1                      # embedded 2025 schedule
//	go run ./cmd/import-schedule -season 2 -file sched.csv      # a new season
//	fly ssh console -C "/app/import-schedule -season 1"         # in production
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Adyn-Cox/CoorsHeavy/internal/config"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

// matchKey identifies a game independently of its date, so a postponed game
// still matches after being moved. Playoff games share the TBD opponent, so
// they are matched by their order in the file instead.
type matchKey struct {
	opponent string
	home     bool
}

func main() {
	seasonID := flag.Int64("season", 0, "season id to load the schedule into (required)")
	file := flag.String("file", "", "schedule CSV path (default: the schedule baked in for this season)")
	fresh := flag.Bool("fresh", false, "take the CSV verbatim, discarding scores recorded in the app")
	prune := flag.Bool("prune", false, "delete games missing from the CSV even if they have recorded stats")
	dryRun := flag.Bool("dry-run", false, "report what would change without writing")
	flag.Parse()

	if *seasonID == 0 {
		fmt.Fprintln(os.Stderr, "error: -season is required (there is more than one season now)")
		flag.Usage()
		os.Exit(2)
	}

	if err := run(*seasonID, *file, *fresh, *prune, *dryRun); err != nil {
		log.Fatal(err)
	}
}

func run(seasonID int64, file string, fresh, prune, dryRun bool) error {
	cfg := config.Load()
	st, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()

	season, err := st.GetSeason(ctx, seasonID)
	if err != nil {
		return err
	}

	raw, source, err := loadSchedule(file, season.Name)
	if err != nil {
		return err
	}
	incoming, err := store.ParseScheduleCSV(bytes.NewReader(raw), seasonID)
	if err != nil {
		return err
	}

	existing, err := st.ListGames(ctx, seasonID)
	if err != nil {
		return fmt.Errorf("read games: %w", err)
	}

	plan, err := buildPlan(ctx, st, incoming, existing, fresh)
	if err != nil {
		return err
	}

	// Refuse to throw away stats by accident. Deleting a game cascades to its
	// batting lines, so a game that has them needs an explicit -prune.
	if len(plan.deleteWithStats) > 0 && !prune {
		var names []string
		for _, g := range plan.deleteWithStats {
			names = append(names, fmt.Sprintf("%s %s", g.Label(), matchupLabel(g)))
		}
		return fmt.Errorf(
			"%d game(s) in the database are missing from %s and have recorded stats:\n  %s\n"+
				"Add them back to the CSV, or pass -prune to delete them and their stats.",
			len(names), source, strings.Join(names, "\n  "))
	}

	fmt.Printf("season %d (%s) · source %s\n", seasonID, season.Name, source)
	fmt.Printf("  %d update, %d insert, %d delete", len(plan.update), len(plan.insert), len(plan.delete)+len(plan.deleteWithStats))
	if plan.keptScores > 0 {
		fmt.Printf(" (%d recorded score(s) carried over)", plan.keptScores)
	}
	fmt.Println()

	if dryRun {
		for _, g := range plan.insert {
			fmt.Printf("  + %s %s %s\n", g.Label(), matchupLabel(g), g.Result())
		}
		for _, g := range plan.update {
			fmt.Printf("  ~ %s %s %s\n", g.Label(), matchupLabel(g), g.Result())
		}
		for _, g := range append(plan.delete, plan.deleteWithStats...) {
			fmt.Printf("  - %s %s\n", g.Label(), matchupLabel(g))
		}
		fmt.Println("dry run — nothing written")
		return nil
	}

	for _, g := range plan.update {
		if err := st.UpdateGameSchedule(ctx, g); err != nil {
			return fmt.Errorf("update game %q: %w", g.Label(), err)
		}
	}
	for _, g := range plan.insert {
		if _, err := st.CreateGame(ctx, g); err != nil {
			return fmt.Errorf("insert game %q: %w", g.Label(), err)
		}
	}
	for _, g := range append(plan.delete, plan.deleteWithStats...) {
		if err := st.DeleteGame(ctx, g.ID); err != nil {
			return fmt.Errorf("delete game %q: %w", g.Label(), err)
		}
	}

	fmt.Printf("schedule for season %s loaded into %s\n", season.Name, cfg.DBPath)
	return nil
}

type plan struct {
	update          []store.Game // matched an existing row: same id, new values
	insert          []store.Game
	delete          []store.Game // in the db, not in the csv, no stats recorded
	deleteWithStats []store.Game
	keptScores      int
}

func buildPlan(ctx context.Context, st store.Store, incoming, existing []store.Game, fresh bool) (plan, error) {
	var p plan

	byKey := map[matchKey]store.Game{}
	var playoffs []store.Game
	for _, g := range existing {
		if g.Playoff {
			playoffs = append(playoffs, g)
			continue
		}
		byKey[matchKey{g.Opponent, g.Home}] = g
	}

	matched := map[int64]bool{}
	playoffIdx := 0
	for _, g := range incoming {
		var prev store.Game
		var found bool
		if g.Playoff {
			if playoffIdx < len(playoffs) {
				prev, found = playoffs[playoffIdx], true
				playoffIdx++
			}
		} else {
			prev, found = byKey[matchKey{g.Opponent, g.Home}]
		}

		if !found {
			p.insert = append(p.insert, g)
			continue
		}

		// The CSV is the source of truth for scheduling — dates, times, who we
		// play. It is NOT the source of truth for results: those are recorded
		// and corrected in the app, so a real score in the database outranks
		// whatever the file happens to carry. Without this, re-importing the
		// schedule would silently revert a corrected score to a stale one.
		// Pass -fresh to take the file verbatim.
		if !fresh && prev.Played && !(prev.UsScore == 0 && prev.ThemScore == 0) {
			g.Played, g.UsScore, g.ThemScore = true, prev.UsScore, prev.ThemScore
			p.keptScores++
		}
		// A playoff matchup set by an admin outranks the CSV's TBD placeholder.
		if g.Playoff && g.Opponent == store.TBDOpponent && prev.Opponent != store.TBDOpponent {
			g.Opponent = prev.Opponent
			g.Time = prev.Time
		}
		g.ID = prev.ID
		matched[prev.ID] = true
		p.update = append(p.update, g)
	}

	for _, g := range existing {
		if matched[g.ID] {
			continue
		}
		lines, err := st.ListBattingForGame(ctx, g.ID)
		if err != nil {
			return p, fmt.Errorf("check stats for game %d: %w", g.ID, err)
		}
		if len(lines) > 0 {
			p.deleteWithStats = append(p.deleteWithStats, g)
		} else {
			p.delete = append(p.delete, g)
		}
	}
	return p, nil
}

func loadSchedule(file, seasonName string) ([]byte, string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", file, err)
		}
		return b, file, nil
	}
	b, err := store.EmbeddedSchedule(seasonName)
	if err != nil {
		return nil, "", fmt.Errorf("%w\nPass -file to load a schedule from disk.", err)
	}
	return b, "embedded schedule-" + seasonName + ".csv", nil
}

func matchupLabel(g store.Game) string {
	if g.Home {
		return "vs " + g.Opponent
	}
	return "@ " + g.Opponent
}
