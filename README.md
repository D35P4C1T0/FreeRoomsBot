# Free Classrooms Bot · UNITN

A Telegram bot that shows which University of Trento classrooms are free or occupied and when their availability changes. Send `/aiuto` for the list of campus commands or try `/povo`.

## Run with Docker Compose

```sh
cp example.env .env
```

Set `Bot__BotToken` in `.env`, then start the bot and MongoDB.

```sh
docker compose up --build -d
```

Set `LOG_RETENTION_DAYS` in `.env` to change the default 90-day retention. For standalone runs, see [appsettings.example.json](appsettings.example.json).

## Private usage dashboard

The dashboard listens on `127.0.0.1:8080` inside the bot container. Keep that port unpublished. Export a private HTML snapshot from the Compose project directory.

```sh
(umask 077; docker compose exec -T free-classrooms-bot /app/free-classrooms-bot usage > usage.html)
```

For a remote deployment, run the Compose command over SSH and keep the redirection on your workstation. Open `usage.html` in your browser. To filter or sort the snapshot, pass a query after `usage`, for example `"q=a3f0&sort=total&page=2"`.

## MongoDB storage

Each supported command or room button makes one usage update. Room refreshes and dashboard reads make no document writes. Usage IDs are stable hashes of Telegram IDs, and group titles are retained.

Inactive usage and older request logs expire automatically. Legacy chat records are kept. On upgrades and retention changes, the MongoDB account needs index management permissions and may need `collMod`. Compose disables periodic MongoDB diagnostic capture to reduce idle disk writes.

## Code layout

- `cmd/locuspocusbot` starts the process and handles configuration and health checks.
- `internal/bot` handles Telegram commands, buttons, and replies.
- `internal/rooms` loads EasyRoom schedules and calculates availability.
- `internal/usage` stores usage and serves the dashboard.

Run `make test race vet` to check changes.

Created by Telegram user `@kirbychan`.
