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
