# CoorsHeavy

A black-and-white team manager for a 19-player squad: **lineup**, **schedule**,
**batting stats**, and **beer/donations** tracking, across multiple **seasons**.
Built with **Go + HTMX + templ**, SQLite for storage, deployed to **Fly.io**.
The public can view everything; a single admin account (env vars) unlocks
editing.

## Features

- **Lineup** — drag players to reorder the batting order, set each player's field
  position (P, C, 1B, 2B, 3B, SS, LF, CLF, CRF, RF, Bench), and tap a badge to
  mark attendance (Here / Absent). Admins can add players and rename them
  inline; a new player joins the current season's roster benched and absent.
- **Schedule** — Coors Heavy's games with home/away, time, location, and result.
  Admins record/edit each game's score inline (auto-computes W/L/T). Playoff
  rows also let an admin pick the start time (6/7/8 PM) and the opponent from
  the teams on the schedule, since seeding isn't known until the season ends.
- **Beer** — per-player progress bar toward **2 thirty-racks**; admin sets the
  amount (0, 0.5, 1, 1.5, 2). Plus a **donations** list (who, what, donated y/n)
  the admin can add to, toggle, and remove.
- **Stats** — per-player, per-game batting lines, with the league's full
  27-stat key (Avg, OB%, Slug, SlugBB, RP7, HRF, RProd, OE, …) derived from
  eleven typed counts. Season leaderboard at `/stats` with Standard / Advanced
  column modes, per-game box scores at `/stats?game=<id>` (linked from every
  schedule row), game log and career totals at `/stats/<player>`, and an entry
  grid at `/statsheet` for admins.
- **Seasons** — every game, donation and roster spot belongs to a season.
  Opening a new one never touches the old; past seasons stay viewable via the
  season picker in the header, on every page.
- **Auth** — one account from env vars; public read-only, login to edit.

## Stack

| Layer     | Choice                                   |
|-----------|------------------------------------------|
| Language  | Go 1.24 (stdlib `net/http` routing)      |
| Templates | [templ](https://templ.guide) (type-safe) |
| Frontend  | HTMX + Tailwind + SortableJS (via CDN)   |
| Database  | SQLite (`modernc.org/sqlite`, pure Go)   |
| Deploy    | Fly.io (Docker, persistent volume)       |

## Layout

```
cmd/web/main.go            # entrypoint: config -> store -> auth -> server
internal/
  config/                  # env-based configuration
  auth/                    # single-account login + signed-cookie sessions
  server/                  # http server, routes, middleware, handlers
  view/                    # templ components (.templ -> *_templ.go) + helpers
  store/                   # Store interface + SQLite impl + migrations + seed
web/static/                # embedded assets (app.css, logo.png)
Dockerfile · fly.toml · Makefile · .air.toml · .env.example
```

## Configuration

All via environment variables (see `.env.example`):

| Var              | Default                         | Purpose                          |
|------------------|---------------------------------|----------------------------------|
| `PORT`           | `8080`                          | HTTP port (Fly injects in prod)  |
| `ENV`            | `development`                   | `production` enables Secure cookies |
| `DB_PATH`        | `coorsheavy.db`                 | SQLite file path                 |
| `ADMIN_USERNAME` | `admin`                         | The admin login                  |
| `ADMIN_PASSWORD` | `changeme`                      | The admin password               |
| `SESSION_SECRET` | `dev-insecure-secret-change-me` | Signs the login cookie           |

## Run locally

```bash
make tools     # one-time: install templ + air
make run       # generates templates, starts http://localhost:8080
# or live-reload while editing:
make dev
```

On first run the database is created and seeded with the roster
(`internal/store/seed.go`) and Coors Heavy's schedule, all on the Bench.
Log in at `/login` to set positions/attendance.

> Roster note: 20 names are seeded, including two duplicates
> (Patty ×2, Parker ×2). Their slugs are forced apart automatically, and admins
> can rename players inline on the lineup page — do that first, because the
> stat sheet shows two identical "Patty" rows until you do.
>
> Renaming changes the display name only. The **slug** stays fixed, because it
> is what the stats CSV keys on: renaming can't orphan stats you've already
> typed. Adding a player whose name is already taken is fine — the slug gets a
> numeric suffix (`forrest`, `forrest-2`), so a third Patty works too.

## Add your logo

Drop a `logo.png` into `web/static/`. It shows in the header and on the home
page automatically (it's hidden until the file exists).

## Seasons

Every game, donation and roster spot belongs to a season, so a new one is purely
additive — the old season keeps every row it had and stays reachable forever.

| Season | | |
|---|---|---|
| 1 | **Summer 2026** | Thursday Men's E Rec D2, Stazio #2 · 10 league + 2 playoff games |
| 2 | **Fall 2026** | Stazio #4 · 9 league + 1 playoff game |

The picker lives in the **header, on every page**, and submits back to the page
you are on so switching seasons keeps you in place. It is in the layout rather
than on each page for two reasons: a season is a property of the whole view
rather than of one table, and a selector that appears on some pages and not
others reads as a bug. It renders nothing until a second season exists.

`/lineup` defaults to the current season but follows the picker like everything
else, and each of its edits carries that season in the URL — so looking at last
season can't rewrite this one.

### Opening a season

```bash
go run ./cmd/new-season -name "Fall 2026" -year 2026 -location "Stazio #4" \
    -roster "Luke,Sam,Adyn,Tanner,..."
go run ./cmd/import-schedule -season 2
```

`-roster` takes the season's players in batting order. **A name that matches an
existing player's slug is that player** — reusing the row is what keeps career
stats attached to one person across seasons. Anything else is a new player.
Matching is on the exact slug and nothing fuzzier, because a wrong guess makes a
duplicate person whose stats are split in two forever; `-dry-run` prints the
returning/new split so you can check before it happens.

That means casual spellings have to be resolved before they're passed in: the
fall list arrived as `forest`, `skyler` and `jon`, which are the existing
`forrest`, `sky` and `john`. Feeding those through verbatim would have made five
new players, not two.

`-carry-roster` is the alternative when everyone returns: it copies the previous
roster forward keeping positions and batting order, resetting beer and
attendance. `-make-current` (on by default) moves which season the site shows.

`-id` edits an existing season instead of creating one, applying only the flags
you actually pass:

```bash
go run ./cmd/new-season -id 1 -name "Summer 2026"     # rename, nothing else moves
```

In production: `fly ssh console -C "/app/new-season -name 'Fall 2026' ..."`.

## Schedule

Schedules are CSV, embedded in the binary so they can be loaded on the live
volume without copying files:

```csv
date,time,opponent,home,location,played,us,them,playoff
2026-05-14,7:00 PM,Hailraisers,0,Stazio #2,1,13,12,0
```

`date` is ISO — the `Thu 5/14` label the schedule page shows is derived from it,
so the two can never disagree. (Storing only the label is what hid the fact that
those dates are Thursdays in **2026**, not 2025.) Columns are matched by header
name, so order doesn't matter and extra columns are ignored.

Files are named after the slugified season: `Fall 2026` loads
`internal/store/data/schedule-fall-2026.csv`. Only Coors Heavy's own games go in
one — the league plays four a night on the same field.

A playoff row is written blank on purpose. The fall bracket seeds 2v1 at 7:00,
4v3 at 8:00, 6v5 at 6:00 and 8v7 at 9:00, so the opponent, the start time **and
who bats last** all wait on the final standings; all three are dropdowns on the
schedule page for admins, and the time offers TBD so it can be put back.

```bash
make import-schedule                                  # season 1, embedded CSV
go run ./cmd/import-schedule -season 2 -file s.csv    # a new season
fly ssh console -C "/app/import-schedule -season 1"   # in production
```

The import **updates games in place** rather than wiping the table, so game ids
survive and the batting lines that reference them are untouched. Each CSV row is
matched to an existing game by opponent + home/away — unique within a season, so
a postponed game keeps its result even though its date moved — and playoff games
are matched by position, since their opponent is `TBD` until seeding is final.

**Results recorded on the site always win over the CSV's.** The CSV is the
source of truth for scheduling — dates, times, who you play — not for scores,
which get entered and corrected in the app. So re-importing a schedule can never
revert a score you fixed on the site; the file's scores only apply to games the
database has no result for. `-fresh` takes the CSV verbatim instead. `-dry-run`
reports the plan without writing. If a game in the database is missing from the CSV *and* has
recorded stats, the import refuses rather than cascading them away; `-prune`
overrides that.

**Recording scores:** log in as admin and edit a game's score inline on the
schedule page (enter our runs vs theirs, hit Save — W/L/T is computed; Clear
resets a game to unplayed).

**Setting a playoff matchup:** on a playoff row an admin gets two dropdowns —
start time (6:00 / 7:00 / 8:00 PM) and opponent (any team from that season's
schedule, or `TBD`). Both save on change and update the row in place.

Both work in production, no redeploy needed.

## Stats

Classic slow pitch: no stolen bases, no hit-by-pitch, no sacrifice bunts. You
type eleven counts per player per game — `AB R H 2B 3B HR RBI BB SO SF E` —
and every stat below is **derived** from them, so adding a stat never means
re-typing a scorebook.

The formulas follow the league's own stat key, which departs from standard
baseball in two places worth knowing:

- **`PA = AB + BB`.** Sacrifice flies get no bucket of their own; a sac fly is
  an at-bat like any other out. (`SF` is still recorded, it just doesn't move
  any of these numbers.)
- **`OB = Hits + Walks`.** Reaching on a fielder's choice is not on base — and
  neither is reaching on an error, which isn't recorded.

| Stat | Meaning |
|------|---------|
| `GM` | Games played |
| `PA` | Plate appearances — `AB + BB` |
| `AB` | At bats |
| `Runs` | Runs scored (not counting running for the pitcher) |
| `Hits` | `1B + 2B + 3B + HR` |
| `1B` `2B` `3B` `HR` | Singles, doubles, triples, homers |
| `RBI` | Runs batted in |
| `BB` | Walks |
| `OB` | Times on base — `Hits + BB` |
| `Avg` | `Hits / AB` |
| `OB%` | `(Hits + BB) / PA` |
| `Slug` | `(1B + 2·2B + 3·3B + 4·HR) / AB` |
| `SlugBB` | `(BB + 1B + 2·2B + 3·3B + 4·HR) / PA` |
| `OPS` | `OB% + Slug` (not in the league key; kept because it's universal) |
| `HRF` | Home run frequency — `AB / HR`. **Lower is better** |
| `RP7` | Runs per 7 innings if that player took every at-bat — `((21/(1−OB%))·Slug)/3` |
| `2B%` `3B%` `HR%` | `2B / AB`, `3B / AB`, `HR / AB` |
| `BB%` | `BB / PA` |
| `RBIpAB` | `RBI / AB` |
| `RProd` | Runs produced — `RBI + Runs − HR` |
| `OE` | Offensive efficiency — `RProd / PA` |
| `AVGnoHR` | `(Hits − HR) / (AB − HR)` |
| `OBnoHR` | `(OB − HR) / (PA − HR)` |

A rate with a zero denominator shows as **`—`**, not `.000`: a player with no
at-bats has no batting average, and `HRF` is undefined until someone actually
hits one. `RP7` is likewise undefined for a 1.000 on-base rate — the estimate
needs 21 outs and never gets them.

`/stats` shows these in three modes — **Standard**, **Advanced**, **All** —
because 27 columns at once is unreadable. `Player`, `GM`, `PA` and `AB` stay on
screen in every mode, since almost every rate divides by one of them. Click any
header to sort; hover one for its definition. `/stats/<player>` shows a game log
plus the full line for the season and career.

**Leaders are red.** The best value in each column is marked, ties included.
Not every column has a leader worth marking: `GM`, `PA` and `AB` are attendance
rather than hitting, and most of the roster has no `SO`, `SF` or `E`, so a
leader there would mark a coincidence. `HRF` is at-bats per home run, so its
leader is the *minimum*. Nobody leads a column at zero — without that guard a
season with no triples would light up the whole `3B` column.

Rate leaders come from **qualified** players only, so a 1.000 average on three
at-bats can't lead the team; counting leaders come from everyone, since most
home runs is most home runs however many trips it took. A box score marks no
rate leaders at all: over one night a 1-for-1 ties a 4-for-4, and marking both
says nothing.

**Picking a game.** `/stats?game=<id>` is the same table scoped to one night —
a box score is a season table over a smaller set of games, and writing it as a
second page would guarantee the two drift apart. The game dropdown above the
table switches between them, and on `/schedule` both the date and the matchup
link straight to that game's box score. (On a playoff row an admin is still
seeding, the matchup cell is a dropdown, so the date is the way in.) Only the qualifying-at-bat marker changes: a `15 AB`
threshold is a season-long idea, so a box score doesn't star anyone.

The column definitions live in one place, `internal/view/statcolumns.go`, so the
header, the player rows and the team totals row cannot drift apart. The entry
grid's live figures are a JS mirror of `store.Batting` in
`web/static/statsheet.js` — if you change a formula, change it in both.

### Typing in a scorebook

The entry grid lives at `/statsheet`, admin-only. It isn't in the nav: stats are
always about one particular game, so it's reached from the game you mean —
**Enter stats** on `/stats`, or **Edit this game** on a box score, both of which
carry the game with them (`/statsheet?game=<id>`).

```bash
make sheet     # same as `make run`, then log in and pick a game
```

Pick a game, type a line per player, **Save**. The grid computes rates live,
flags impossible lines (more hits than at-bats, extra-base hits exceeding hits),
and warns when the runs you've entered don't match the game's recorded final
score — the best single catch for a missed or doubled line. A line that fails
those checks is refused before it's sent, so the offending cell is still
outlined in front of you.

The grid opens on whatever is already stored for that game, so correcting a
night is the same act as typing it the first time. Saving **replaces** the
game's lines rather than merging them: clearing a line has to actually remove
it, or a doubled-up line typed by mistake could never be taken back out. Lines
of all zeroes are dropped rather than stored — an empty line would count as a
game played and drag every rate down.

Typing only touches the browser's local storage; nothing reaches the database
until you save. That keeps a half-typed game across a refresh, and lets the
sheet tell you — **Unsaved changes** next to the button — when what you're
looking at isn't what's stored.

### Importing a CSV

```bash
go run ./cmd/import-stats -season 1 -file stats.csv -dry-run   # always first
go run ./cmd/import-stats -season 1 -file stats.csv
```

`-dry-run` reports every unresolved key and every impossible line at once —
transcription produces mistakes in batches — plus a table reconciling runs
entered against each game's final score. `-replace` clears a game's existing
lines instead of merging, for re-importing a corrected file. The whole import is
one transaction: it lands completely or not at all.

`cmd/import-stats` is for loading a file — a season transcribed offline, or a
copy moved between databases. Day-to-day entry goes through the sheet above.

Rows key games by `(date, opponent)` and players by **slug**, never by database
id. That is deliberate: a file typed against a local database has to import into
production, where autoincrement ids differ. Names alone won't do either — the
roster has two Patty and two Parker.

In production, put the file on the volume first:

```bash
fly ssh sftp shell        # then: put stats.csv /data/stats.csv
fly ssh console -C "/app/import-stats -season 1 -file /data/stats.csv"
```

## Build

```bash
make build      # -> bin/web (static, CGO-free)
make docker     # -> container image
```

## Deploy to Fly.io

```bash
# Install + login
curl -L https://fly.io/install.sh | sh
fly auth login

# Create the app (edit name/region in fly.toml first) and the SQLite volume
fly launch --no-deploy --copy-config --name coorsheavy
fly volumes create coorsheavy_data --region dfw --size 1

# Set the admin credentials + cookie secret as secrets (NOT in fly.toml)
fly secrets set \
  ADMIN_USERNAME=yourname \
  ADMIN_PASSWORD='a-strong-password' \
  SESSION_SECRET="$(openssl rand -hex 32)"

# Deploy
fly deploy && fly open
```

Notes:
- SQLite is single-writer — keep this to **one machine** (don't scale count > 1).
  `fly.toml` sets `strategy = "immediate"` for the same reason: a rolling deploy
  would need a second machine, and only one can hold the volume.
- `primary_region` and the volume's region must match (both `dfw` above) — a
  volume in another region can't attach.
- `ENV=production` (set in `fly.toml`) turns on Secure cookies for HTTPS.
- Fly health-checks `GET /healthz`; machines auto-stop when idle and auto-start
  on the next request, so a cold hit takes an extra second.
- Migrations run at startup and are tracked in a `schema_migrations` table, so a
  deploy applies any new `internal/store/migrations/*.sql` exactly once. Each
  file runs in a transaction with its own tracking row, so a failure part-way
  can't leave half-applied, unrecorded DDL. There are no down migrations —
  **back the volume up before deploying a schema change**:
  `fly ssh sftp get /data/coorsheavy.db ./backup-$(date +%F).db`.
- Keep the 4-digit migration filename prefix: ordering is lexical, so a file
  named `10_...` would sort before `0004_...`.
- If you're using Spotify, point the redirect URI at the deployed host:
  `fly secrets set SPOTIFY_REDIRECT_URI=https://coorsheavy.fly.dev/spotify/callback`.
- Outgrowing SQLite later means adding a `postgres.go` implementing
  `store.Store`; handlers and views don't change.

### Applying a schedule change to production

Schedules live in `internal/store/data/*.csv` and are embedded in the binary, so
pushing an edit (like the 6/25 → 7/30 postponement) means a deploy plus one
command against the live volume:

```bash
fly deploy
fly ssh console -C "/app/import-schedule -season 1"
```

Run it with `-dry-run` first if the change is more than a date move. Games are
updated in place, so recorded scores and batting lines survive.

## How HTMX is used (the pattern to copy)

- Page loads render a full page (`view.Lineup`, `view.Beer`, …).
- Edits POST to a small endpoint that returns **just the changed fragment**
  (e.g. `view.AttendanceBadge`, `view.BeerRow`, `view.DonationRow`), which HTMX
  swaps in place — no full reload.
- Drag-reorder uses SortableJS; the player IDs are submitted in their new order
  when you save the lineup (`POST /lineup/save`).
- The stat sheet at `/statsheet` is the one exception: it's a plain JS grid, not
  HTMX, because it's a bulk transcription tool that shouldn't hit the server on
  every keystroke. It reads its roster, schedule and stored lines from a
  `<script type="application/json">` block rendered into the page, and posts a
  whole game back as JSON to `POST /statsheet/save` when you press Save.
