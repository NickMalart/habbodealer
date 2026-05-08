# Free Raffle Bot

Free Raffle Bot is a standalone sub-application that watches Neon for newly completed bets from roll-origins and maintains raffle tickets for an active session.

## Rules

- First qualifying bet in a session adds the player to the raffle with 1 ticket.
- Each additional bet increases that player's bet count.
- Ticket formula per player:
  - tickets = 1 + floor(betCount / bonusEvery)
- Default bonusEvery is 5 (every 5th bet grants an extra ticket).

## Requirements

- Shared Neon config in `db.local.json` (same pattern as other apps).
- Owner key must match the roll-origins owner key.
- Bot must be enabled and connected/in-room to ingest new bets.

## Run

From this folder:

```powershell
go mod tidy
go run .
```

Or build:

```powershell
go build -o .\build\bin\free-raffle-bot.exe .
```
