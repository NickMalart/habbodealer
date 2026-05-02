# Wave Timer App

Independent sibling app for G-Earth that sends a wave packet (`A^`) on a timer.

## What It Does

- Opens a small fixed window
- Lets you set interval in minutes
- Lets you enable humanized timing jitter
- Start/Stop toggle for looping wave sends
- Uses G-Earth connection via `goearth` and sends `WAVE`

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

- This project is fully independent from the main dealer app.
- It only reuses the same core extension connection pattern for G-Earth.
- `WAVE` maps to the outgoing wave packet (`A^`) in the extension packet map.
