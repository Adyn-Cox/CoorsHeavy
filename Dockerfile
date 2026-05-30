# syntax=docker/dockerfile:1

# --- Build stage: compile a static, CGO-free binary -------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src

# Cache dependencies separately from source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Generated *_templ.go files are committed, so we only need `go build` here.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/web ./cmd/web

# --- Run stage: tiny distroless image --------------------------------------
FROM gcr.io/distroless/static-debian12 AS run
WORKDIR /app
COPY --from=build /bin/web /app/web

ENV PORT=8080
# On Fly.io the persistent volume is mounted at /data (see fly.toml).
ENV DB_PATH=/data/coorsheavy.db

EXPOSE 8080
ENTRYPOINT ["/app/web"]
