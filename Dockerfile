# syntax=docker/dockerfile:1

# --- Build stage: compile a static, CGO-free binary -------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache dependencies separately from source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Generated *_templ.go files are committed, so we only need `go build` here.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/web ./cmd/web
# Shipped alongside the server so they can be run against the live volume with
# fly ssh console -C "/app/<name> ...". Schedules are embedded in the binary,
# so import-schedule needs no files present in the image.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/import-schedule ./cmd/import-schedule
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/import-stats ./cmd/import-stats
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/new-season ./cmd/new-season

# --- Run stage: tiny distroless image --------------------------------------
FROM gcr.io/distroless/static-debian12 AS run
WORKDIR /app
COPY --from=build /bin/web /app/web
COPY --from=build /bin/import-schedule /app/import-schedule
COPY --from=build /bin/import-stats /app/import-stats
COPY --from=build /bin/new-season /app/new-season

ENV PORT=8080
# On Fly.io the persistent volume is mounted at /data (see fly.toml).
ENV DB_PATH=/data/coorsheavy.db

EXPOSE 8080
ENTRYPOINT ["/app/web"]
