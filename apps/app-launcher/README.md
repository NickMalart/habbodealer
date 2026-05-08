# App Launcher

A simple desktop launcher that lets you pick and open your tool apps from one menu.

## Current discovery targets

- roll-origins executable (preferred path: `build/bin/roll-origins.exe`)
- wave timer executable (preferred path: `wave-timer-app/wave-timer-app.exe`)
- trade tracker executable (preferred path: `trade-tracker/build/bin/trade-tracker.exe`)
- any other `.exe` found in:
  - workspace root
  - `build/bin`
  - `wave-timer-app`
  - `trade-tracker`

## Run

```powershell
cd app-launcher
wails dev
```

## Build

```powershell
cd app-launcher
wails build
```

Then run `app-launcher.exe` and choose which app to open.
