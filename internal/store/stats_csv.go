package store

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// StatRow is one line of a stats CSV, still keyed by the human-readable values
// the entry grid writes. Resolving those to ids is the importer's job.
//
// Games are keyed by (date, opponent) and players by slug rather than by
// database id on purpose: stats are typed against a local database, where
// autoincrement ids differ from production's. Names alone won't do either — the
// roster has had two Patty and two Parker.
type StatRow struct {
	Line     int    // source line number, for error messages
	Date     string // ISO, e.g. "2025-05-14"
	Opponent string
	Player   string // player slug
	Batting
}

// StatsCSVHeader is the header row the entry grid exports and the importer
// expects. Extra columns are ignored and column order doesn't matter.
var StatsCSVHeader = append([]string{"date", "opponent", "player"}, StatColumns...)

// statColumnField maps a CSV column name to the field it fills.
var statColumnField = map[string]func(*Batting) *int{
	"ab":  func(b *Batting) *int { return &b.AB },
	"r":   func(b *Batting) *int { return &b.R },
	"h":   func(b *Batting) *int { return &b.H },
	"2b":  func(b *Batting) *int { return &b.B2 },
	"3b":  func(b *Batting) *int { return &b.B3 },
	"hr":  func(b *Batting) *int { return &b.HR },
	"rbi": func(b *Batting) *int { return &b.RBI },
	"bb":  func(b *Batting) *int { return &b.BB },
	"so":  func(b *Batting) *int { return &b.SO },
	"sf":  func(b *Batting) *int { return &b.SF },
	"e":   func(b *Batting) *int { return &b.E },
}

// ParseStatsCSV reads batting lines. Every problem in the file is collected and
// reported together — transcribing a scorebook produces typos in batches, and
// failing on the first one means running the importer a dozen times.
//
//	date,opponent,player,ab,r,h,2b,3b,hr,rbi,bb,so,sf,e
//	2025-05-14,Hailraisers,cooper,4,2,3,1,0,1,4,0,0,0,1
func ParseStatsCSV(r io.Reader) ([]StatRow, error) {
	recs, header, err := readCSV(r)
	if err != nil {
		return nil, err
	}
	for _, required := range []string{"date", "opponent", "player"} {
		if _, ok := header[required]; !ok {
			return nil, fmt.Errorf("stats csv: missing required column %q (expected header: %s)",
				required, strings.Join(StatsCSVHeader, ","))
		}
	}

	var rows []StatRow
	var problems []string
	for _, rec := range recs {
		get := func(col string) string { return rec.get(header, col) }

		row := StatRow{
			Line:     rec.line,
			Date:     get("date"),
			Opponent: get("opponent"),
			Player:   strings.ToLower(get("player")),
		}
		if _, err := time.Parse(ISODate, row.Date); err != nil {
			problems = append(problems, fmt.Sprintf("line %d: date %q is not YYYY-MM-DD", rec.line, row.Date))
			continue
		}
		if row.Player == "" {
			problems = append(problems, fmt.Sprintf("line %d: player is blank", rec.line))
			continue
		}

		bad := false
		for col, field := range statColumnField {
			if _, ok := header[col]; !ok {
				continue
			}
			v, err := parseOptionalInt(get(col))
			if err != nil {
				problems = append(problems, fmt.Sprintf("line %d: %s = %q is not a whole number", rec.line, strings.ToUpper(col), get(col)))
				bad = true
				continue
			}
			*field(&row.Batting) = v
		}
		if bad {
			continue
		}
		if err := row.Batting.Validate(); err != nil {
			problems = append(problems, fmt.Sprintf("line %d (%s): %v", rec.line, row.Player, err))
			continue
		}
		// A row of all zeroes means the player didn't bat; skip rather than
		// storing an empty line that would count as a game played.
		if row.Batting.Empty() && row.Batting.RBI == 0 {
			continue
		}
		rows = append(rows, row)
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("stats csv: %d problem(s):\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	return rows, nil
}

// SetStat writes one of the entry-grid columns by name, reporting whether the
// name is one the grid knows. Stat reads the same column back.
//
// Both go through the mapping the CSV parser uses, so a line typed in the
// browser, a line read from a file and a line loaded from the database can
// never disagree about which column names which field.
func SetStat(b *Batting, col string, v int) bool {
	f, ok := statColumnField[strings.ToLower(col)]
	if !ok {
		return false
	}
	*f(b) = v
	return true
}

// Stat reads one of the entry-grid columns by name, 0 for an unknown name.
func Stat(b Batting, col string) int {
	f, ok := statColumnField[strings.ToLower(col)]
	if !ok {
		return 0
	}
	return *f(&b)
}
