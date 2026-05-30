# CoorsHeavy

A black-and-white team manager for a 15-player squad: **lineup**, **schedule**,
and **beer/donations** tracking. Built with **Go + HTMX + templ**, SQLite for
storage, deployed to **Fly.io**. The public can view everything; a single admin
account (env vars) unlocks editing.

## Features

- **Lineup** — drag players to reorder the batting order, set each player's field
  position (P, C, 1B, 2B, 3B, SS, LF, CLF, CRF, RF, Bench), and tap a badge to
  mark attendance (Here / Absent).
- **Schedule** — Coors Heavy's games with home/away, time, location, and result.
  Admins record/edit each game's score inline (auto-computes W/L/T).
- **Beer** — per-player progress bar toward **2 thirty-racks**; admin sets the
  amount (0, 0.5, 1, 1.5, 2). Plus a **donations** list (who, what, donated y/n)
  the admin can add to, toggle, and remove.
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

> Roster note: 19 names are seeded as provided, including two duplicates
> (Patty ×2, Parker ×2) pending your review. Renaming players from the UI isn't
> wired up yet — edit `seedRoster` in `internal/store/seed.go` and delete the DB
> to reseed, or ask me to add inline name editing + add/remove on the lineup page.

## Add your logo

Drop a `logo.png` into `web/static/`. It shows in the header and on the home
page automatically (it's hidden until the file exists).

## Schedule

Coors Heavy's 10 league games (Thursday Men's E Rec D2, Stazio #2) live in
`internal/store/seed.go` and are loaded automatically on first run. The schedule
page shows date, time, matchup (`vs` home / `@` away), location, and result
(weeks 1–3 scores included; future games show `—`). Week 8 (bye) and the
playoffs are noted below the table.

**Recording scores:** log in as admin and edit a game's score inline on the
schedule page (enter our runs vs theirs, hit Save — W/L/T is computed; Clear
resets a game to unplayed). This works in production too, no redeploy needed.

To reset the whole schedule back to the values in `seedGames` (locally):

```bash
make import-schedule      # wipes the games table and reloads from seed.go
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
fly volumes create coorsheavy_data --region den --size 1

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
- `ENV=production` (set in `fly.toml`) turns on Secure cookies for HTTPS.
- Outgrowing SQLite later means adding a `postgres.go` implementing
  `store.Store`; handlers and views don't change.

## How HTMX is used (the pattern to copy)

- Page loads render a full page (`view.Lineup`, `view.Beer`, …).
- Edits POST to a small endpoint that returns **just the changed fragment**
  (e.g. `view.AttendanceBadge`, `view.BeerRow`, `view.DonationRow`), which HTMX
  swaps in place — no full reload.
- Drag-reorder uses SortableJS; on drop, HTMX POSTs the player IDs in their new
  order to `/lineup/reorder`.
