# Free Classrooms Bot - UNITN

Modern Go implementation of the UNITN classroom availability Telegram bot.

## Layout

- `cmd/locuspocusbot`: executable entrypoint
- `internal/bot`: bot app, config, MongoDB logging, EasyAcademy room loading, rendering, and tests
- root files: module, Docker, compose, Makefile, example config

## Features

- Telegram long polling with `gopkg.in/telebot.v3`
- `/start`, `/aiuto`, and department commands
- group/supergroup startup messages when the bot is added
- EasyAcademy room loading and department-specific room-name filtering
- free/occupied/all room grouping with inline callback buttons
- MongoDB `chats` and `logs` collections
- pseudonymous per-user usage tracking in the MongoDB `usage` collection
- local-only, read-only usage dashboard at `http://127.0.0.1:8080/usage`
- hourly room refresh
- quiet Docker logging defaults

## Run

```sh
cp example.env .env
docker compose up --build
```

Config supports `appsettings.json` plus env overrides:

- `Bot__BotToken`
- `Bot__BotName`
- `Database__ConnectionString`
- `Database__LogRetentionDays` (default: `90`; MongoDB TTL expiry for usage logs)

With Docker Compose, set `LOG_RETENTION_DAYS` in `.env` to override retention.
- `Health__Port`
- `Logging__LogLevel__Default`

The Docker Compose configuration sets the bot log level to `Warning` and
enables Docker log rotation at 10 MB per file with 3 retained files per
container.

MongoDB usage logs expire after 90 days by default. MongoDB reuses freed
WiredTiger space internally; expiry bounds future growth but does not
immediately shrink an already enlarged volume.

## Flood safety

Storage and write volume stay bounded even under heavy request floods (for
example abusive automation against the bot):

- usage logs are aggregated into one document per chat, UTC hour, department,
  availability view, and request type (`Count` field), so a request storm
  cannot create unbounded documents — at most a few dozen buckets per chat per
  hour regardless of volume;
- a per-chat interaction gate throttles log-bucket writes to at most one write
  per second per chat, collapsing sub-second bursts;
- per-user usage statistics stay exact and bounded by the number of distinct
  Telegram users (their Telegram IDs are never stored), and every write is a
  cheap indexed upsert;
- incoming request volume is additionally bounded by Telegram's own rate
  limits, and all collections are TTL-expired.

## Usage dashboard

A minimal, read-only admin dashboard is served at `/usage` on the health
server. It binds to `127.0.0.1` only and Docker Compose publishes no ports, so
it is never reachable from the network. To view it from the host, open a local
tunnel into the container (for example
`docker run --rm -it --network container:free-classrooms-bot-unitn-free-classrooms-bot-1 alpine/socat tcp-listen:8081,fork,reuseaddr tcp:127.0.0.1:8080`
and browse `http://127.0.0.1:8081/usage`) or run the binary locally against
the same MongoDB instance.

For each user it shows a pseudonymous internal ID (Telegram IDs are never
stored there), total interactions, first/last usage, a usage-frequency
classification (frequent / occasional / rare), the last 14 days of activity,
and the chats the user interacted from.

## Author

@kirbychan on Telegram

## Verify

```sh
GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-mod go test ./...
GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-mod go test -race ./...
GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-mod go vet ./...
GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-mod go build -o bin/free-classrooms-bot ./cmd/locuspocusbot
docker build --network=host .
```

Equivalent shortcuts:

```sh
make test race vet docker compose-config
```
