package store

// seedRoster is the initial player list, inserted on first run (empty table).
// NOTE: 19 names as provided. Two appear twice (Patty, Parker) — the unique
// slug index forces them apart as "patty-10"/"patty-13" etc. Rename them from
// the lineup page to give each a real distinguishing name.
//
// The schedule is NOT seeded here any more. It lives in data/*.csv and loads
// through cmd/import-schedule, so opening a new season needs no code change.
var seedRoster = []string{
	"Cooper", "Cade", "Ethan", "Luke", "Nick", "Sam", "Harry", "Adyn", "John", "Patty",
	"Jackson", "Kwan", "Patty", "Parker", "Brandon", "Eral", "Diggy", "Parker", "Sky",
	"Forrest",
}
