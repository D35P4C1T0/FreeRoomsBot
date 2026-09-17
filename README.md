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

## Usage storage

Request logs use one bucket per chat, UTC hour, department, availability view,
and request type. Every successful write increments `Count`, including bursts.
There is no write-rate throttle: database work scales with incoming requests.

`Database__LogRetentionDays` (default 90) controls log expiry, retained daily
usage history, retained chat associations, and expiry of inactive usage users.
Active users keep lifetime interaction totals and first-seen dates; daily/chat
history is pruned atomically on each interaction and at startup for legacy data.
Inactive usage users expire after the configured interval. MongoDB TTL deletion
is asynchronous; the dashboard excludes expired users immediately. The existing
`chats` metadata collection is not TTL-expired. Usage retention setup failures
stop startup rather than silently disabling expiry.

Usage IDs are stable hashes, not anonymous identities: someone who knows a
Telegram ID can compute its hash. Group titles are retained; private-user names
are not stored in `usage`. Existing `chats` and `logs` still contain Telegram IDs.

## Private usage dashboard (Docker inside Ubuntu LXC)

`/usage` binds **only to 127.0.0.1 inside the bot container**. Compose publishes
no ports. Do not add a port mapping, host networking, public reverse proxy, or
change the listener to `0.0.0.0`. The LXC IP cannot reach this endpoint directly.
The handler also rejects non-loopback peers and non-local Host headers. Docker
administrators and processes sharing the container network namespace can access
it; loopback isolation is not authentication against those administrators.

The dashboard uses server-rendered HTML with no JavaScript, CDN, fonts, or other
network dependencies. It includes global summary cards, 30-day activity counts,
ID-prefix search, sorting, 50-user pages, and expandable chat details. Frequency
uses active days in the last 30 UTC days (frequent ≥12, occasional ≥4, rare ≥1).
Summary cards include all retained users regardless of the current filter/page.

To view it without exposing any HTTP listener, export a private HTML snapshot.
From your workstation, use your existing SSH access to the Ubuntu LXC:

```sh
# Replace host and directory with your LXC SSH host and Compose project path.
# The shell redirection saves the file on your workstation, not the server.
(umask 077; ssh user@lxc-host 'cd /path/to/FreeRoomsBot && docker compose exec -T free-classrooms-bot /app/free-classrooms-bot usage' > usage.html)
```

Open `usage.html` locally in your browser. Charts and expandable details work
offline. Search, sorting, and pagination need a new export with query parameters:

```sh
(umask 077; ssh user@lxc-host 'cd /path/to/FreeRoomsBot && docker compose exec -T free-classrooms-bot /app/free-classrooms-bot usage "sort=total&page=2"' > usage.html)
```

Use `q=a3f0` for an internal-ID prefix, and `sort=recent`, `sort=total`, or
`sort=first`. Export reads the running bot's local HTTP endpoint; it does not
start another bot or server. Treat the exported file as private admin data.
No socat sidecar, published port, or public dashboard is needed.

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
