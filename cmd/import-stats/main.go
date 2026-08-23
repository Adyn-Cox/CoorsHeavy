// Command import-stats loads batting lines from a CSV into one season.
//
// The CSV is what the /statsheet grid exports. It keys games by date and
// opponent and players by slug rather than by database id, because the sheet is
// meant to be run locally against a copy of the database and then imported into
// production, where autoincrement ids differ. Names alone are not enough: the
// roster has had two Patty and two Parker.
//
//	date,opponent,player,ab,r,h,2b,3b,hr,rbi,bb,so,sf,e
//	2025-05-14,Hailraisers,cooper,4,2,3,1,0,1,4,0,0,0,1
//
// Run it with -dry-run first. That reports every unresolved key and every
// impossible line without writing, and prints the reconciliation table showing
// whether the runs you typed match each game's recorded final score.
//
//	go run ./cmd/import-stats -season 1 -file stats.csv -dry-run
//	go run ./cmd/import-stats -season 1 -file stats.csv
//	fly ssh console -C "/app/import-stats -season 1 -file /data/stats.csv"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Adyn-Cox/CoorsHeavy/internal/config"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

func main() {
	seasonID := flag.Int64("season", 0, "season id to import into (required)")
	file := flag.String("file", "", "stats CSV path (required)")
	dryRun := flag.Bool("dry-run", false, "report what would happen without writing")
	replace := flag.Bool("replace", false, "clear each game's existing lines first, instead of merging")
	flag.Parse()

	if *seasonID == 0 || *file == "" {
		fmt.Fprintln(os.Stderr, "error: -season and -file are both required")
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*seasonID, *file, *dryRun, *replace); err != nil {
		log.Fatal(err)
	}
}

func run(seasonID int64, file string, dryRun, replace bool) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	rows, err := store.ParseStatsCSV(f)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("%s has no stat lines", file)
	}

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
	games, err := st.ListGames(ctx, seasonID)
	if err != nil {
		return err
	}
	if len(games) == 0 {
		return fmt.Errorf("season %s has no schedule loaded — run import-schedule first", season.Name)
	}

	res, err := resolve(ctx, st, rows, games)
	if err != nil {
		return err
	}
	if len(res.problems) > 0 {
		return fmt.Errorf("%d unresolved row(s) in %s:\n  %s",
			len(res.problems), file, strings.Join(res.problems, "\n  "))
	}

	fmt.Printf("%s → season %s (%s)\n", file, season.Name, cfg.DBPath)
	fmt.Printf("  %d lines across %d games, %d players\n\n",
		len(res.lines), len(res.gameIDs), countPlayers(res.lines))

	reconcile(res, games)

	if dryRun {
		fmt.Println("\ndry run — nothing written")
		return nil
	}

	var replaceGames []int64
	if replace {
		replaceGames = res.gameIDs
	}
	if err := st.ImportBattingLines(ctx, res.lines, replaceGames); err != nil {
		return fmt.Errorf("import: %w", err)
	}
	fmt.Printf("\nimported %d batting lines\n", len(res.lines))
	return nil
}

type resolved struct {
	lines    []store.BattingLine
	gameIDs  []int64
	byGame   map[int64][]store.BattingLine
	problems []string
}

// resolve turns the CSV's human-readable keys into database ids, collecting
// every failure rather than stopping at the first — a scorebook transcription
// produces mistakes in batches.
func resolve(ctx context.Context, st store.Store, rows []store.StatRow, games []store.Game) (resolved, error) {
	res := resolved{byGame: map[int64][]store.BattingLine{}}

	// Index the schedule by date and by (date, opponent). Two games can share a
	// date only if the league ever double-headers, so the date alone is usually
	// enough — but the opponent is checked when given, to catch a typo.
	byDate := map[string][]store.Game{}
	for _, g := range games {
		if g.PlayedOn == "" {
			continue
		}
		byDate[g.PlayedOn] = append(byDate[g.PlayedOn], g)
	}

	players := map[string]store.Player{}
	seen := map[[2]int64]int{} // (game, player) -> source line, to catch duplicates

	for _, row := range rows {
		candidates := byDate[row.Date]
		if len(candidates) == 0 {
			res.problems = append(res.problems, fmt.Sprintf(
				"line %d: no game on %s in this season (is -season right?)", row.Line, row.Date))
			continue
		}
		var game store.Game
		var found bool
		for _, g := range candidates {
			if strings.EqualFold(g.Opponent, row.Opponent) {
				game, found = g, true
				break
			}
		}
		if !found {
			if len(candidates) == 1 && row.Opponent == "" {
				game, found = candidates[0], true
			} else {
				var names []string
				for _, g := range candidates {
					names = append(names, g.Opponent)
				}
				res.problems = append(res.problems, fmt.Sprintf(
					"line %d: %s has no game against %q (that date has: %s)",
					row.Line, row.Date, row.Opponent, strings.Join(names, ", ")))
				continue
			}
		}

		p, ok := players[row.Player]
		if !ok {
			var err error
			p, err = st.GetPlayerBySlug(ctx, row.Player)
			if err != nil {
				res.problems = append(res.problems, fmt.Sprintf(
					"line %d: no player with slug %q", row.Line, row.Player))
				continue
			}
			players[row.Player] = p
		}

		key := [2]int64{game.ID, p.ID}
		if prev, dup := seen[key]; dup {
			res.problems = append(res.problems, fmt.Sprintf(
				"line %d: %s already has a line for %s (line %d)", row.Line, row.Player, row.Date, prev))
			continue
		}
		seen[key] = row.Line

		line := store.BattingLine{GameID: game.ID, PlayerID: p.ID, Batting: row.Batting}
		res.lines = append(res.lines, line)
		if _, ok := res.byGame[game.ID]; !ok {
			res.gameIDs = append(res.gameIDs, game.ID)
		}
		res.byGame[game.ID] = append(res.byGame[game.ID], line)
	}

	sort.Slice(res.gameIDs, func(i, j int) bool { return res.gameIDs[i] < res.gameIDs[j] })
	return res, nil
}

// reconcile prints runs-entered against each game's recorded final score. A
// mismatch is a warning, not a failure: the score in the app might be the wrong
// one. But it is the single best catch for a missed or doubled line.
func reconcile(res resolved, games []store.Game) {
	byID := map[int64]store.Game{}
	for _, g := range games {
		byID[g.ID] = g
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  GAME\tLINES\tR ENTERED\tFINAL\tCHECK")
	var mismatches int
	for _, id := range res.gameIDs {
		g := byID[id]
		lines := res.byGame[id]
		var runs int
		for _, l := range lines {
			runs += l.R
		}
		check := "—"
		if g.Played {
			if runs == g.UsScore {
				check = "ok"
			} else {
				check = fmt.Sprintf("MISMATCH (%+d)", runs-g.UsScore)
				mismatches++
			}
		}
		final := "not recorded"
		if g.Played {
			final = fmt.Sprintf("%d-%d", g.UsScore, g.ThemScore)
		}
		fmt.Fprintf(w, "  %s %s\t%d\t%d\t%s\t%s\n",
			g.Label(), matchupLabel(g), len(lines), runs, final, check)
	}
	w.Flush()

	if mismatches > 0 {
		fmt.Printf("\n  %d game(s) where runs entered don't match the final score.\n", mismatches)
		fmt.Println("  Check for a missed line, a doubled line, or a wrong score in the app.")
	}
}

func countPlayers(lines []store.BattingLine) int {
	seen := map[int64]bool{}
	for _, l := range lines {
		seen[l.PlayerID] = true
	}
	return len(seen)
}

func matchupLabel(g store.Game) string {
	if g.Home {
		return "vs " + g.Opponent
	}
	return "@ " + g.Opponent
}
