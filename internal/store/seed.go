package store

// seedRoster is the initial player list, inserted on first run (empty table).
// NOTE: 19 names as provided. Two appear twice (Patty, Parker) — kept as-is for
// review. Rename/trim from the lineup once player editing is wired up, or delete
// the DB to reseed after editing this list.
var seedRoster = []string{
	"Cooper", "Cade", "Ethan", "Luke", "Nick", "Sam", "Harry", "Adyn", "John", "Patty",
	"Jackson", "Kwan", "Patty", "Parker", "Brandon", "Eral", "Diggy", "Parker", "Sky",
}

// seedGames is Coors Heavy's schedule (Thursday Men's E Rec D2, all at Stazio #2).
// Weeks 1–3 are played; admins can edit/record scores from the schedule page.
var seedGames = []Game{
	{SortOrder: 1, Date: "Thu 5/14", Time: "7:00 PM", Opponent: "Hailraisers", Home: false, Location: "Stazio #2", Played: true, UsScore: 13, ThemScore: 12},
	{SortOrder: 2, Date: "Thu 5/21", Time: "8:00 PM", Opponent: "Big Sticks", Home: false, Location: "Stazio #2", Played: true, UsScore: 3, ThemScore: 13},
	{SortOrder: 3, Date: "Thu 5/28", Time: "7:00 PM", Opponent: "The Slo Leftovers", Home: true, Location: "Stazio #2", Played: true, UsScore: 6, ThemScore: 5},
	{SortOrder: 4, Date: "Thu 6/4", Time: "6:00 PM", Opponent: "The Benchwarmers", Home: false, Location: "Stazio #2"},
	{SortOrder: 5, Date: "Thu 6/11", Time: "6:00 PM", Opponent: "The MasterBatters", Home: false, Location: "Stazio #2"},
	{SortOrder: 6, Date: "Thu 6/18", Time: "6:00 PM", Opponent: "Hailraisers", Home: true, Location: "Stazio #2"},
	{SortOrder: 7, Date: "Thu 6/25", Time: "8:00 PM", Opponent: "Big Sticks", Home: true, Location: "Stazio #2"},
	{SortOrder: 8, Date: "Thu 7/9", Time: "7:00 PM", Opponent: "The Slo Leftovers", Home: false, Location: "Stazio #2"},
	{SortOrder: 9, Date: "Thu 7/16", Time: "8:00 PM", Opponent: "The Benchwarmers", Home: true, Location: "Stazio #2"},
	{SortOrder: 10, Date: "Thu 7/23", Time: "7:00 PM", Opponent: "The MasterBatters", Home: true, Location: "Stazio #2"},
}

// SeedGames returns a copy of the canonical schedule, for the import command to
// wipe-and-reload (e.g. after you edit scores).
func SeedGames() []Game { return append([]Game(nil), seedGames...) }
