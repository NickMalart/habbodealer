# Trade Tracker App

Standalone sibling app that records incoming trades into timestamped sessions.

## What It Does

- Start and stop tracking sessions manually
- Records each incoming trade-open event while session is running
- Stores timestamp, trade id, resolved username (when available), and raw payload hex
- Keeps completed sessions in memory for review

## Run

From this folder:

```powershell
go mod tidy
go run .
```

Or build:

```powershell
go build .
```

## Notes

- This project is independent from the dealer app.
- It uses the same G-Earth extension connection pattern.
- Name resolution uses the existing parser script at `../../internal/scripts/parse_users28.py`.
- Database config is loaded from a shared `db.local.json` file at the workspace root (or any parent directory from the app runtime path).
