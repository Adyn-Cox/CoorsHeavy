.PHONY: run dev build generate test tidy docker tools clean import-schedule import-stats sheet

## generate: compile .templ files into Go (*_templ.go)
generate:
	templ generate

## run: generate templates and run the server
run: generate
	go run ./cmd/web

## dev: live-reload during development (requires air + templ; run `make tools`)
dev:
	air

## build: produce a static binary in ./bin/web
build: generate
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/web ./cmd/web

## import-schedule: load a season's schedule from CSV (SEASON=1 by default)
import-schedule:
	go run ./cmd/import-schedule -season $(or $(SEASON),1)

## import-stats: load batting lines from CSV (make import-stats FILE=stats.csv)
import-stats:
	go run ./cmd/import-stats -season $(or $(SEASON),1) -file $(FILE)

## sheet: run the site locally so you can type stats at /statsheet
##        (reached from Stats > Enter stats, or a game's box score)
sheet: run

## test: run all tests
test:
	go test ./...

## tidy: sync go.mod / go.sum
tidy:
	go mod tidy

## docker: build the container image
docker:
	docker build -t coorsheavy .

## tools: install dev tooling (templ + air)
tools:
	go install github.com/a-h/templ/cmd/templ@latest
	go install github.com/air-verse/air@latest

## clean: remove build artifacts and local DBs
clean:
	rm -rf bin tmp *.db *.db-shm *.db-wal
