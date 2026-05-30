.PHONY: run dev build generate test tidy docker tools clean

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

## import-schedule: wipe & reload the schedule from internal/store/seed.go
import-schedule:
	go run ./cmd/import-schedule

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
