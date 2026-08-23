// Command new-season opens a new season, and edits an existing one.
//
// Seasons own their games, donations and roster, so creating one is additive:
// last season keeps every row it had and stays reachable at /schedule?season=1
// forever. Only the "current" flag moves, which is what the site shows by
// default and what the lineup page edits.
//
//	go run ./cmd/new-season -name "Fall 2026" -year 2026 -location "Stazio #4" \
//	    -roster "Luke,Sam,Adyn,Tanner"
//	go run ./cmd/new-season -id 1 -name "Summer 2026"    # rename in place
//	fly ssh console -C "/app/new-season -name 'Fall 2026' -year 2026 -roster ..."
//
// Afterwards, load its schedule:
//
//	go run ./cmd/import-schedule -season <new id>
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Adyn-Cox/CoorsHeavy/internal/config"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

type options struct {
	id          int64
	name        string
	year        int
	league      string
	location    string
	notes       string
	roster      string
	carry       bool
	makeCurrent bool
	dryRun      bool
}

func main() {
	var o options
	flag.Int64Var(&o.id, "id", 0, "edit this existing season instead of creating one")
	flag.StringVar(&o.name, "name", "", "season name, e.g. \"Fall 2026\" (required when creating)")
	flag.IntVar(&o.year, "year", 0, "calendar year (required when creating)")
	flag.StringVar(&o.league, "league", "", "league name, e.g. \"Thursday Men's E Rec D2\"")
	flag.StringVar(&o.location, "location", "", "home field, e.g. \"Stazio #4\"")
	flag.StringVar(&o.notes, "notes", "", "footnotes shown under the schedule, one per line")
	flag.StringVar(&o.roster, "roster", "", "comma-separated player names for this season; existing players are reused, new names are created")
	flag.BoolVar(&o.carry, "carry-roster", false, "copy the previous season's roster forward instead (beer and attendance reset)")
	flag.BoolVar(&o.makeCurrent, "make-current", true, "make this the season the site shows by default")
	flag.BoolVar(&o.dryRun, "dry-run", false, "report what would happen without writing")
	flag.Parse()

	if o.id == 0 && (o.name == "" || o.year == 0) {
		fmt.Fprintln(os.Stderr, "error: -name and -year are both required when creating a season")
		flag.Usage()
		os.Exit(2)
	}
	if o.carry && o.roster != "" {
		fmt.Fprintln(os.Stderr, "error: pass either -roster or -carry-roster, not both")
		os.Exit(2)
	}
	if err := run(o); err != nil {
		log.Fatal(err)
	}
}

func run(o options) error {
	cfg := config.Load()
	st, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx := context.Background()
	if o.id != 0 {
		return edit(ctx, st, o)
	}
	return create(ctx, st, o)
}

// edit updates one season's label and details. Nothing else moves: the id is
// the same, so every game, donation and stat line stays exactly where it was.
func edit(ctx context.Context, st *store.SQLite, o options) error {
	season, err := st.GetSeason(ctx, o.id)
	if err != nil {
		return err
	}
	before := season

	// Only the flags actually passed are applied, so `-id 1 -name "Summer
	// 2026"` renames without blanking the league and location.
	set := passedFlags()
	if set["name"] {
		season.Name = o.name
	}
	if set["year"] {
		season.Year = o.year
	}
	if set["league"] {
		season.League = o.league
	}
	if set["location"] {
		season.Location = o.location
	}
	if set["notes"] {
		season.Notes = o.notes
	}

	fmt.Printf("season %d\n", season.ID)
	describe("name", before.Name, season.Name)
	describe("year", fmt.Sprint(before.Year), fmt.Sprint(season.Year))
	describe("league", before.League, season.League)
	describe("location", before.Location, season.Location)
	describe("notes", firstLine(before.Notes), firstLine(season.Notes))

	if o.dryRun {
		fmt.Println("\ndry run — nothing written")
		return nil
	}
	if err := st.UpdateSeason(ctx, season); err != nil {
		return fmt.Errorf("update season: %w", err)
	}
	if o.roster != "" {
		if _, err := applyRoster(ctx, st, season, o.roster, false); err != nil {
			return err
		}
	}
	fmt.Println("\nsaved")
	return nil
}

func create(ctx context.Context, st *store.SQLite, o options) error {
	previous, err := st.CurrentSeason(ctx)
	if err != nil {
		return err
	}

	// Resolving the roster first means a typo'd name fails before a half-built
	// season exists, rather than after.
	if o.roster != "" && o.dryRun {
		fmt.Printf("would create season %q (%d)\n", o.name, o.year)
		_, err := applyRoster(ctx, st, store.Season{}, o.roster, true)
		return err
	}
	if o.dryRun {
		fmt.Printf("would create season %q (%d)\n", o.name, o.year)
		return nil
	}

	season, err := st.CreateSeason(ctx, store.Season{
		Name:     o.name,
		Year:     o.year,
		League:   o.league,
		Location: o.location,
		Notes:    o.notes,
	})
	if err != nil {
		return fmt.Errorf("create season: %w", err)
	}
	fmt.Printf("created season %d (%s)\n", season.ID, season.Name)

	switch {
	case o.roster != "":
		if _, err := applyRoster(ctx, st, season, o.roster, false); err != nil {
			return err
		}
	case o.carry:
		n, err := st.CarryRosterForward(ctx, previous.ID, season.ID)
		if err != nil {
			return fmt.Errorf("carry roster: %w", err)
		}
		fmt.Printf("carried %d players forward from %s (beer and attendance reset)\n", n, previous.Name)
	}

	if o.makeCurrent {
		if err := st.SetCurrentSeason(ctx, season.ID); err != nil {
			return fmt.Errorf("set current season: %w", err)
		}
		fmt.Printf("%s is now the current season (%s is archived and still viewable)\n", season.Name, previous.Name)
	}

	fmt.Printf("\nNext: load the schedule with\n  go run ./cmd/import-schedule -season %d\n", season.ID)
	return nil
}

// applyRoster puts a list of names on a season's roster, in the order given —
// which becomes the batting order.
//
// A name that matches an existing player's slug is that player: reusing the row
// is what keeps career stats attached to one person across seasons. Anything
// else is a new player. Matching is on the exact slug and nothing fuzzier,
// because the cost of a wrong guess is a duplicate person whose stats are split
// in two forever.
func applyRoster(ctx context.Context, st *store.SQLite, season store.Season, list string, plan bool) (int, error) {
	existing, err := st.ListAllPlayers(ctx)
	if err != nil {
		return 0, err
	}
	bySlug := make(map[string]store.Player, len(existing))
	for _, p := range existing {
		bySlug[p.Slug] = p
	}

	names := splitList(list)
	if len(names) == 0 {
		return 0, fmt.Errorf("-roster was given but contained no names")
	}

	var reused, created []string
	seen := map[string]bool{}
	for _, name := range names {
		slug := store.Slugify(name)
		if slug == "" {
			return 0, fmt.Errorf("roster entry %q has no letters or digits", name)
		}
		if seen[slug] {
			return 0, fmt.Errorf("roster lists %q twice", name)
		}
		seen[slug] = true

		p, ok := bySlug[slug]
		if ok {
			reused = append(reused, p.Name)
		} else {
			created = append(created, name)
			if plan {
				continue
			}
			if p, err = st.CreatePlayer(ctx, name); err != nil {
				return 0, fmt.Errorf("create player %q: %w", name, err)
			}
		}
		if plan {
			continue
		}
		if err := st.AddToRoster(ctx, season.ID, p.ID); err != nil {
			return 0, fmt.Errorf("add %q to roster: %w", name, err)
		}
	}

	verb := "roster"
	if plan {
		verb = "would set roster"
	}
	fmt.Printf("\n%s: %d players (%d returning, %d new)\n", verb, len(names), len(reused), len(created))
	if len(reused) > 0 {
		fmt.Printf("  returning: %s\n", strings.Join(reused, ", "))
	}
	if len(created) > 0 {
		fmt.Printf("  new:       %s\n", strings.Join(created, ", "))
	}
	return len(names), nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// passedFlags reports which flags were actually given, so edit mode can tell
// "-league ”" (clear it) from "-league not mentioned" (leave it alone).
func passedFlags() map[string]bool {
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

func describe(label, before, after string) {
	if before == after {
		fmt.Printf("  %-9s %s\n", label+":", blank(before))
		return
	}
	fmt.Printf("  %-9s %s → %s\n", label+":", blank(before), blank(after))
}

func blank(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
