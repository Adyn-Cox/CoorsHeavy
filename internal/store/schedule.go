package store

import (
	"embed"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

//go:embed data/*.csv
var scheduleFS embed.FS

// ISODate is the layout schedule and stats CSVs use for dates.
const ISODate = "2006-01-02"

// displayDate is the league's own label format, e.g. "Thu 5/14".
const displayDate = "Mon 1/2"

// EmbeddedSchedule returns the schedule CSV baked into the binary for a season
// name, e.g. "2025" -> data/schedule-2025.csv. Shipping schedules inside the
// binary is what lets `fly ssh console -C /app/import-schedule` work: the
// production image is distroless and has no files to read.
func EmbeddedSchedule(seasonName string) ([]byte, error) {
	// Slugified so a season named "Fall 2026" finds schedule-fall-2026.csv
	// rather than a file with a space in its name.
	name := "data/schedule-" + Slugify(seasonName) + ".csv"
	b, err := scheduleFS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("no schedule baked in for season %q (looked for %s): %w", seasonName, name, err)
	}
	return b, nil
}

// FormatGameDate renders an ISO date as the league's display label.
func FormatGameDate(iso string) string {
	t, err := time.Parse(ISODate, iso)
	if err != nil {
		return iso
	}
	return t.Format(displayDate)
}

// ParseScheduleCSV reads a schedule into Games for one season. Columns are
// matched by header name, so order doesn't matter and extra columns are
// ignored. Blank lines and #-comments are skipped.
//
//	date,time,opponent,home,location,played,us,them,playoff
//	2025-05-14,7:00 PM,Hailraisers,0,Stazio #2,1,13,12,0
//
// The display label ("Thu 5/14") is derived from date rather than stored in the
// file, so there is one source of truth for when a game is.
func ParseScheduleCSV(r io.Reader, seasonID int64) ([]Game, error) {
	recs, header, err := readCSV(r)
	if err != nil {
		return nil, err
	}
	for _, required := range []string{"date", "opponent"} {
		if _, ok := header[required]; !ok {
			return nil, fmt.Errorf("schedule csv: missing required column %q", required)
		}
	}

	var games []Game
	var problems []string
	for _, rec := range recs {
		get := func(col string) string { return rec.get(header, col) }

		iso := get("date")
		if _, err := time.Parse(ISODate, iso); err != nil {
			problems = append(problems, fmt.Sprintf("line %d: date %q is not YYYY-MM-DD", rec.line, iso))
			continue
		}
		played := parseBool(get("played"))
		us, errUs := parseOptionalInt(get("us"))
		them, errThem := parseOptionalInt(get("them"))
		if errUs != nil || errThem != nil {
			problems = append(problems, fmt.Sprintf("line %d: scores must be whole numbers or blank", rec.line))
			continue
		}
		// A blank score with played=1 is a postponement placeholder, not a 0-0
		// tie. Treat it as unplayed so the schedule doesn't show "T 0-0".
		if played && get("us") == "" && get("them") == "" {
			played = false
		}

		gameTime := get("time")
		if gameTime != "" && !ValidGameTime(gameTime) {
			problems = append(problems, fmt.Sprintf("line %d: time %q is not one of %v", rec.line, gameTime, GameTimes))
			continue
		}

		opponent := get("opponent")
		if opponent == "" {
			opponent = TBDOpponent
		}

		games = append(games, Game{
			SeasonID:  seasonID,
			SortOrder: len(games) + 1,
			Date:      FormatGameDate(iso),
			PlayedOn:  iso,
			Time:      gameTime,
			Opponent:  opponent,
			Home:      parseBool(get("home")),
			Location:  get("location"),
			Played:    played,
			UsScore:   us,
			ThemScore: them,
			Playoff:   parseBool(get("playoff")),
		})
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("schedule csv:\n  %s", strings.Join(problems, "\n  "))
	}
	if len(games) == 0 {
		return nil, fmt.Errorf("schedule csv: no games found")
	}
	return games, nil
}

// --- shared CSV plumbing ----------------------------------------------------

// csvRecord is one data row plus the file line it came from, so errors can
// point at the row the user actually typed.
type csvRecord struct {
	fields []string
	line   int
}

func (r csvRecord) get(header map[string]int, col string) string {
	i, ok := header[col]
	if !ok || i >= len(r.fields) {
		return ""
	}
	return strings.TrimSpace(r.fields[i])
}

// readCSV parses a CSV into a header index and records, skipping blank lines
// and #-comments. Header names are lower-cased and trimmed so "AB" and "ab"
// both work.
func readCSV(r io.Reader) ([]csvRecord, map[string]int, error) {
	cr := csv.NewReader(r)
	cr.Comment = '#'
	cr.FieldsPerRecord = -1 // ragged rows are reported per-field instead
	cr.TrimLeadingSpace = true

	rows, err := cr.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("read csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("read csv: file is empty")
	}

	header := make(map[string]int, len(rows[0]))
	for i, name := range rows[0] {
		key := strings.ToLower(strings.TrimSpace(name))
		if key != "" {
			header[key] = i
		}
	}

	var recs []csvRecord
	for i, row := range rows[1:] {
		if isBlankRow(row) {
			continue
		}
		recs = append(recs, csvRecord{fields: row, line: i + 2})
	}
	return recs, header, nil
}

func isBlankRow(row []string) bool {
	for _, f := range row {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

// parseBool accepts the several ways a person might write yes in a spreadsheet.
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "t":
		return true
	}
	return false
}

// parseOptionalInt treats blank as zero, so empty stat cells are fine.
func parseOptionalInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}
