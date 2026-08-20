package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// 笏笏 Build target definitions 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

type buildTarget struct {
	ID         string
	Name       string
	SrcDir     string   // relative to workspace root ("." = root itself)
	BuildType  string   // "wails" | "go"
	CleanPaths []string // relative to workspace root, deleted before each build
	OutExe     string   // relative to workspace root, expected output path
}

func knownBuildTargets() []buildTarget {
	return []buildTarget{
		{
			ID:        "roll-origins",
			Name:      "roll-origins",
			SrcDir:    ".",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("build", "bin", "roll-origins.exe"),
				// also wipe old name if it lingers
				filepath.Join("build", "bin", "Gamba-Suite.exe"),
			},
			OutExe: filepath.Join("build", "bin", "roll-origins.exe"),
		},
		{
			ID:        "wave-timer",
			Name:      "Wave Timer",
			SrcDir:    "wave-timer-app",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("wave-timer-app", "build", "bin", "wave-timer-app.exe"),
				filepath.Join("wave-timer-app", "wave-timer-app.exe"),
				filepath.Join("wave-timer-app", "wave-timer.exe"),
			},
			OutExe: filepath.Join("wave-timer-app", "build", "bin", "wave-timer-app.exe"),
		},
		{
			ID:        "trade-tracker",
			Name:      "Trade Tracker",
			SrcDir:    "trade-tracker",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("trade-tracker", "build", "bin", "trade-tracker.exe"),
				filepath.Join("trade-tracker", "trade-tracker.exe"),
			},
			OutExe: filepath.Join("trade-tracker", "build", "bin", "trade-tracker.exe"),
		},
		{
			ID:        "free-raffle-bot",
			Name:      "Free Raffle Bot",
			SrcDir:    "free-raffle-bot",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("free-raffle-bot", "build", "bin", "free-raffle-bot.exe"),
				filepath.Join("free-raffle-bot", "free-raffle-bot.exe"),
			},
			OutExe: filepath.Join("free-raffle-bot", "build", "bin", "free-raffle-bot.exe"),
		},
		{
			ID:        "winner-picker",
			Name:      "Winner Picker",
			SrcDir:    "winner-picker",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("winner-picker", "build", "bin", "winner-picker.exe"),
				filepath.Join("winner-picker", "winner-picker.exe"),
			},
			OutExe: filepath.Join("winner-picker", "build", "bin", "winner-picker.exe"),
		},
		{
			ID:        "casino-statistics",
			Name:      "Casino Statistics",
			SrcDir:    "casino-statistics",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("casino-statistics", "build", "bin", "casino-statistics.exe"),
				filepath.Join("casino-statistics", "casino-statistics.exe"),
			},
			OutExe: filepath.Join("casino-statistics", "build", "bin", "casino-statistics.exe"),
		},
		{
			ID:        "auto-payout",
			Name:      "Auto Payout Bot",
			SrcDir:    "auto-payout",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("auto-payout", "build", "bin", "auto-payout.exe"),
				filepath.Join("auto-payout", "auto-payout.exe"),
			},
			OutExe: filepath.Join("auto-payout", "build", "bin", "auto-payout.exe"),
		},
		{
			ID:        "pickup-drop",
			Name:      "Pickup-Drop",
			SrcDir:    "pickup-drop",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("pickup-drop", "build", "bin", "pickup-drop.exe"),
				filepath.Join("pickup-drop", "pickup-drop.exe"),
			},
			OutExe: filepath.Join("pickup-drop", "build", "bin", "pickup-drop.exe"),
		},
		{
			ID:        "anniversary-bot",
			Name:      "Anniversary Bot",
			SrcDir:    "anniversary-event",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("anniversary-event", "build", "bin", "anniversary-bot.exe"),
				filepath.Join("anniversary-event", "anniversary-bot.exe"),
			},
			OutExe: filepath.Join("anniversary-event", "build", "bin", "anniversary-bot.exe"),
		},
		{
			ID:        "exit-building",
			Name:      "Exit-Building",
			SrcDir:    "exit-building",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("exit-building", "build", "bin", "exit-building.exe"),
				filepath.Join("exit-building", "exit-building.exe"),
			},
			OutExe: filepath.Join("exit-building", "build", "bin", "exit-building.exe"),
		},
		{
			ID:        "multipurpose-app",
			Name:      "Multipurpose App",
			SrcDir:    "multipurpose-app",
			BuildType: "wails",
			CleanPaths: []string{
				filepath.Join("multipurpose-app", "build", "bin", "multipurpose-app.exe"),
				filepath.Join("multipurpose-app", "multipurpose-app.exe"),
			},
			OutExe: filepath.Join("multipurpose-app", "build", "bin", "multipurpose-app.exe"),
		},
	}
}

// 笏笏 App types 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

type LaunchAppItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Running bool   `json:"running"`
}

type GEarthStatus struct {
	Exists  bool `json:"exists"`
	Running bool `json:"running"`
}

type App struct {
	ctx       context.Context
	mu        sync.Mutex
	apps      []LaunchAppItem
	processes map[string]*os.Process
	building  bool
	// When true, App Launcher will stop/kill tracked and known apps on shutdown.
	// Default is false to avoid unexpectedly terminating other apps when closing the
	// launcher window.
	stopChildrenOnExit bool
}

func NewApp() *App {
	return &App{processes: map[string]*os.Process{}, stopChildrenOnExit: false}
}

type dbConfig struct {
	DatabaseURL string `json:"databaseUrl"`
}

func readDatabaseURL(root string) (string, error) {
	candidates := []string{
		filepath.Join(root, "db.local.json"),
		filepath.Join(root, "app-launcher", "db.local.json"),
	}

	for _, candidate := range candidates {
		if !fileExists(candidate) {
			continue
		}

		data, err := os.ReadFile(candidate)
		if err != nil {
			return "", err
		}

		var cfg dbConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return "", err
		}
		if strings.TrimSpace(cfg.DatabaseURL) != "" {
			return cfg.DatabaseURL, nil
		}
	}

	return "", fmt.Errorf("database config not found in %s", root)
}

func (a *App) CreateMissingTables() string {
	root := a.resolveWorkspaceRoot()
	connString, err := readDatabaseURL(root)
	if err != nil {
		msg := fmt.Sprintf("CreateMissingTables failed: %v", err)
		a.emitLog(msg, "error")
		return msg
	}

	a.emitLog(fmt.Sprintf("Creating database schema using %s", root), "info")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		msg := fmt.Sprintf("CreateMissingTables failed: parse config: %v", err)
		a.emitLog(msg, "error")
		return msg
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = make(map[string]string)
	}
	cfg.ConnConfig.RuntimeParams["default_query_exec_mode"] = "simple_protocol"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		msg := fmt.Sprintf("CreateMissingTables failed: connect: %v", err)
		a.emitLog(msg, "error")
		return msg
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		msg := fmt.Sprintf("CreateMissingTables failed: ping: %v", err)
		a.emitLog(msg, "error")
		return msg
	}

	resetQueries := []string{
		`DROP TABLE IF EXISTS public.ui_settings CASCADE;`,
		`DROP TABLE IF EXISTS public.blocked_players CASCADE;`,
		`DROP TABLE IF EXISTS public.trade_entry_items CASCADE;`,
		`DROP TABLE IF EXISTS public.trade_entries CASCADE;`,
		`DROP TABLE IF EXISTS public.trade_sessions CASCADE;`,
		`DROP TABLE IF EXISTS public.raffle_participants CASCADE;`,
		`DROP TABLE IF EXISTS public.raffle_sessions CASCADE;`,
		`DROP TABLE IF EXISTS public.auto_payout_settings CASCADE;`,
		`DROP TABLE IF EXISTS public.banned_players CASCADE;`,
		`DROP TABLE IF EXISTS public.banker_trades CASCADE;`,
		`DROP TABLE IF EXISTS public.auto_payouts CASCADE;`,
		`DROP TABLE IF EXISTS public.banker_inventory CASCADE;`,
		`DROP TABLE IF EXISTS public.dealer_shouts CASCADE;`,
		`DROP TABLE IF EXISTS public.stocked_items CASCADE;`,
		`DROP TABLE IF EXISTS public.trade_ledger CASCADE;`,
		`DROP TABLE IF EXISTS public.game_history_items CASCADE;`,
		`DROP TABLE IF EXISTS public.game_history_entries CASCADE;`,
	}
	for _, query := range resetQueries {
		if _, err := pool.Exec(ctx, query); err != nil {
			msg := fmt.Sprintf("CreateMissingTables reset failed: %v\nquery: %s", err, query)
			a.emitLog(msg, "error")
			return msg
		}
	}

	queries := []string{
		`CREATE TABLE IF NOT EXISTS public.game_history_entries (
			id BIGSERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			ended_at TIMESTAMPTZ NULL,
			game_type TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL DEFAULT ''
		);`,
		`ALTER TABLE public.game_history_entries ADD COLUMN IF NOT EXISTS raffle_session_id BIGINT NOT NULL DEFAULT 0;`,
		`ALTER TABLE public.game_history_entries ADD COLUMN IF NOT EXISTS rolls JSONB NOT NULL DEFAULT '[]'::jsonb;`,
		`CREATE TABLE IF NOT EXISTS public.game_history_items (
			id BIGSERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			entry_id BIGINT NOT NULL DEFAULT 0,
			item_type TEXT NOT NULL DEFAULT '',
			item_index INTEGER NOT NULL DEFAULT 0,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb
		);`,
		`CREATE TABLE IF NOT EXISTS public.trade_ledger (
			id BIGSERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			trade_type TEXT NOT NULL DEFAULT '',
			item_name TEXT NOT NULL DEFAULT '',
			quantity INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS public.stocked_items (
			id BIGSERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			raw_name TEXT NOT NULL DEFAULT '',
			canonical_name TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_stocked_items_owner_raw ON public.stocked_items(owner_key, raw_name) WHERE raw_name <> '';`,
		`CREATE TABLE IF NOT EXISTS public.dealer_shouts (
			id BIGSERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			target_player TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			shout_type TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMPTZ NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_dealer_shouts_status_owner ON public.dealer_shouts(status, owner_key);`,
		`CREATE TABLE IF NOT EXISTS public.banker_inventory (
			banker_name TEXT NOT NULL DEFAULT '',
			item_name TEXT NOT NULL DEFAULT '',
			quantity INTEGER NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (banker_name, item_name)
		);`,
		`CREATE TABLE IF NOT EXISTS public.auto_payouts (
			id TEXT PRIMARY KEY,
			player_name TEXT NOT NULL,
			item_name TEXT NOT NULL,
			quantity INTEGER NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			player_trade_id INTEGER NULL,
			banker_trade_id INTEGER NULL,
			notified BOOLEAN DEFAULT FALSE
		);`,
		`CREATE TABLE IF NOT EXISTS public.banker_trades (
			id BIGSERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			status TEXT NOT NULL DEFAULT 'idle',
			bet_amount INTEGER DEFAULT 0,
			risk_bank INTEGER DEFAULT 0,
			risk_status TEXT DEFAULT 'idle'
		);`,
		`CREATE TABLE IF NOT EXISTS public.banned_players (
			ban_key TEXT PRIMARY KEY,
			expires_at TIMESTAMP NOT NULL,
			message TEXT NOT NULL,
			is_active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT NOW()
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_banned_players_ban_key ON public.banned_players (ban_key);`,
		`CREATE TABLE IF NOT EXISTS public.auto_payout_settings (
			setting_key TEXT PRIMARY KEY,
			setting_value TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_auto_payout_settings_key ON public.auto_payout_settings (setting_key);`,
		`CREATE TABLE IF NOT EXISTS public.raffle_sessions (
			id BIGSERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			status TEXT NOT NULL DEFAULT 'pending',
			raffle_name TEXT NOT NULL DEFAULT 'Flame Raffle',
			prize_name TEXT NOT NULL DEFAULT 'Purple Dragon Lamp'
		);`,
		`CREATE TABLE IF NOT EXISTS public.raffle_participants (
			id BIGSERIAL PRIMARY KEY,
			session_id BIGINT NOT NULL DEFAULT 0,
			owner_key TEXT NOT NULL DEFAULT '',
			username_key TEXT NOT NULL DEFAULT '',
			ticket_count INTEGER NOT NULL DEFAULT 1,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS public.trade_sessions (
			id BIGSERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS public.trade_entries (
			id BIGSERIAL PRIMARY KEY,
			session_id BIGINT NOT NULL DEFAULT 0,
			owner_key TEXT NOT NULL DEFAULT '',
			occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			payload JSONB NOT NULL DEFAULT '{}'::jsonb
		);`,
		`CREATE TABLE IF NOT EXISTS public.trade_entry_items (
			id BIGSERIAL PRIMARY KEY,
			trade_entry_id BIGINT NOT NULL DEFAULT 0,
			owner_key TEXT NOT NULL DEFAULT '',
			item_name TEXT NOT NULL DEFAULT '',
			quantity INTEGER NOT NULL DEFAULT 1,
			raw_data TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS public.blocked_players (
			id BIGSERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS public.ui_settings (
			id BIGSERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			setting_key TEXT NOT NULL DEFAULT '',
			setting_value TEXT NOT NULL DEFAULT ''
		);`,
	}

	for _, query := range queries {
		if _, err := pool.Exec(ctx, query); err != nil {
			msg := fmt.Sprintf("CreateMissingTables failed: %v\nquery: %s", err, query)
			a.emitLog(msg, "error")
			return msg
		}
	}

	return "Database reset and recreated successfully"
}

// SetStopChildrenOnExit controls whether App Launcher will stop tracked/known
// child processes when the launcher shuts down. Exposed for UI control.
func (a *App) SetStopChildrenOnExit(v bool) {
	a.mu.Lock()
	a.stopChildrenOnExit = v
	a.mu.Unlock()
}

func (a *App) GetStopChildrenOnExit() bool {
	a.mu.Lock()
	v := a.stopChildrenOnExit
	a.mu.Unlock()
	return v
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.RefreshApps()
}

// 笏笏 Workspace root detection 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

func (a *App) resolveWorkspaceRoot() string {
	cwd, _ := os.Getwd()
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	candidates := []string{cwd, exeDir, filepath.Dir(cwd), filepath.Dir(exeDir)}
	seen := map[string]struct{}{}

	for _, c := range candidates {
		if c == "" {
			continue
		}
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		// workspace root has both app.go and main.go side by side
		if fileExists(filepath.Join(abs, "app.go")) && fileExists(filepath.Join(abs, "main.go")) {
			return abs
		}
	}
	if cwd != "" {
		return cwd
	}
	if exeDir != "" {
		return exeDir
	}
	return "."
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func canonicalNameFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.TrimSpace(base)
	if base == "" {
		return "Unknown App"
	}
	words := strings.Fields(base)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// 笏笏 App discovery 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

func (a *App) discoverApps() []LaunchAppItem {
	root := a.resolveWorkspaceRoot()
	targets := knownBuildTargets()

	items := make([]LaunchAppItem, 0)
	seenPath := map[string]struct{}{}

	for _, t := range targets {
		candidates := []string{}
		if t.OutExe != "" {
			candidates = append(candidates, filepath.Join(root, t.OutExe))
		}
		for _, cp := range t.CleanPaths {
			candidates = append(candidates, filepath.Join(root, cp))
		}
		foundPath := ""
		for _, c := range candidates {
			if fileExists(c) {
				foundPath = c
				break
			}
		}
		item := LaunchAppItem{ID: t.ID, Name: t.Name, Path: foundPath, Exists: foundPath != ""}
		if foundPath != "" {
			seenPath[strings.ToLower(foundPath)] = struct{}{}
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Exists != items[j].Exists {
			return items[i].Exists
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	a.mu.Lock()
	for i := range items {
		if a.hasRunningInstanceLocked(items[i].ID) {
			items[i].Running = true
		}
	}
	a.mu.Unlock()

	return items
}

func (a *App) RefreshApps() []LaunchAppItem {
	apps := a.discoverApps()
	a.mu.Lock()
	a.apps = apps
	a.mu.Unlock()
	return apps
}

func (a *App) GetApps() []LaunchAppItem {
	a.mu.Lock()
	if len(a.apps) > 0 {
		apps := make([]LaunchAppItem, len(a.apps))
		copy(apps, a.apps)
		a.mu.Unlock()
		return apps
	}
	a.mu.Unlock()
	return a.RefreshApps()
}

func (a *App) findAppByID(id string) (LaunchAppItem, error) {
	apps := a.GetApps()
	for _, it := range apps {
		if it.ID == id {
			return it, nil
		}
	}
	return LaunchAppItem{}, errors.New("app not found")
}

func (a *App) gEarthExePath() string {
	root := a.resolveWorkspaceRoot()
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	candidates := []string{
		filepath.Join(root, "G-Earth.windows-x64", "G-Earth.exe"),
		filepath.Join(root, "app-launcher", "G-Earth.windows-x64", "G-Earth.exe"),
		filepath.Join(exeDir, "G-Earth.windows-x64", "G-Earth.exe"),
	}

	for _, c := range candidates {
		if fileExists(c) {
			return c
		}
	}

	// Default expected location when it is not present yet.
	return candidates[0]
}

func (a *App) GetGEarthStatus() GEarthStatus {
	exePath := a.gEarthExePath()
	return GEarthStatus{Exists: fileExists(exePath), Running: false}
}

// 笏笏 Launch 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

func buildInstanceKey(appID, port string) string {
	if strings.TrimSpace(port) == "" {
		return appID + "|default"
	}
	return appID + "|" + strings.TrimSpace(port)
}

func (a *App) hasRunningInstanceLocked(appID string) bool {
	prefix := appID + "|"
	for key, proc := range a.processes {
		if proc != nil && strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func normalizePort(raw string) (string, error) {
	port := strings.TrimSpace(raw)
	if port == "" {
		return "", nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", errors.New("port must be a number between 1 and 65535")
	}
	return strconv.Itoa(n), nil
}

func (a *App) LaunchApp(appID string, port string) string {
	item, err := a.findAppByID(strings.TrimSpace(appID))
	if err != nil {
		return err.Error()
	}
	if !item.Exists || strings.TrimSpace(item.Path) == "" {
		return "executable not found 窶・build it first"
	}

	normalizedPort, err := normalizePort(port)
	if err != nil {
		return err.Error()
	}

	args := []string{}
	if normalizedPort != "" {
		args = []string{"-p", normalizedPort}
	}

	// Always allow launching a new instance when user clicks Run.
	// Use a unique key so stale/default entries never block future launches.
	instanceKey := fmt.Sprintf("%s|%s|%d", item.ID, normalizedPort, time.Now().UnixNano())

	cmd := exec.Command(item.Path, args...)
	cmd.Dir = filepath.Dir(item.Path)
	hideWindow(cmd)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("failed to launch: %v", err)
	}

	a.mu.Lock()
	a.processes[instanceKey] = cmd.Process
	a.mu.Unlock()

	// Consume stdout and stderr but do not stream them to the build log.
	go func() {
		defer stdout.Close()
		_, _ = io.Copy(io.Discard, stdout)
	}()
	go func() {
		defer stderr.Close()
		_, _ = io.Copy(io.Discard, stderr)
	}()

	go func(key string, c *exec.Cmd, name string) {
		_ = c.Wait()
		a.mu.Lock()
		delete(a.processes, key)
		a.mu.Unlock()
		// No longer emitting "Process exited" to build log to keep it clean.
	}(instanceKey, cmd, item.Name)

	a.RefreshApps()
	return "ok"
}

func (a *App) LaunchGEarth() string {
	exePath := a.gEarthExePath()
	if !fileExists(exePath) {
		return "G-Earth.exe not found at G-Earth.windows-x64/G-Earth.exe"
	}

	if runtime.GOOS == "windows" {
		exeEscaped := strings.ReplaceAll(exePath, "'", "''")
		dirEscaped := strings.ReplaceAll(filepath.Dir(exePath), "'", "''")
		psCmd := fmt.Sprintf("Start-Process -FilePath '%s' -WorkingDirectory '%s' -Verb RunAs", exeEscaped, dirEscaped)
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
		hideWindow(cmd)
		if err := cmd.Run(); err != nil {
			return fmt.Sprintf("failed to launch G-Earth as admin: %v", err)
		}
		return "ok"
	}

	cmd := exec.Command(exePath)
	cmd.Dir = filepath.Dir(exePath)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("failed to launch G-Earth: %v", err)
	}

	go func(c *exec.Cmd) {
		_ = c.Wait()
	}(cmd)

	return "ok"
}

// 笏笏 Build helpers 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

func (a *App) emitLog(line, kind string) {
	if a.ctx == nil {
		return
	}
	wailsrt.EventsEmit(a.ctx, "buildLog", map[string]string{
		"line": line,
		"kind": kind,
	})
}

// findTool looks up a CLI tool in PATH then common Windows install locations.
func findTool(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	if runtime.GOOS == "windows" {
		extras := []string{
			filepath.Join(os.Getenv("GOPATH"), "bin", name+".exe"),
			filepath.Join(os.Getenv("USERPROFILE"), "go", "bin", name+".exe"),
		}
		for _, e := range extras {
			if fileExists(e) {
				return e
			}
		}
	}
	return name
}

func (a *App) streamToLog(r io.ReadCloser, prefix, defaultKind string) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		kind := defaultKind
		low := strings.ToLower(line)
		if strings.Contains(low, "error") || strings.Contains(low, "failed") || strings.Contains(low, "cannot") {
			kind = "error"
		}
		a.emitLog(prefix+line, kind)
	}
}

func (a *App) runCmd(dir string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	hideWindow(cmd)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return err
	}

	go a.streamToLog(stdout, "    ", "info")
	go a.streamToLog(stderr, "    ", "error")

	return cmd.Wait()
}

func (a *App) killTrackedProcessesForApp(appID string) int {
	a.mu.Lock()
	keys := make([]string, 0)
	procs := make([]*os.Process, 0)
	for key, proc := range a.processes {
		if proc != nil && strings.HasPrefix(key, appID+"|") {
			keys = append(keys, key)
			procs = append(procs, proc)
		}
	}
	a.mu.Unlock()

	killed := 0
	for i, p := range procs {
		if p == nil {
			continue
		}
		if err := p.Kill(); err == nil {
			killed++
		} else {
			a.emitLog(fmt.Sprintf("  warn: could not kill tracked process %s: %v", keys[i], err), "error")
		}
	}

	a.mu.Lock()
	for _, key := range keys {
		delete(a.processes, key)
	}
	a.mu.Unlock()

	return killed
}

func (a *App) stopProcessByExePath(exePath string) {
	if runtime.GOOS != "windows" || strings.TrimSpace(exePath) == "" {
		return
	}

	escaped := strings.ReplaceAll(exePath, "'", "''")
	ps := fmt.Sprintf("Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq '%s' } | ForEach-Object { Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue }", escaped)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	hideWindow(cmd)
	_ = cmd.Run()
}

func (a *App) stopProcessByExeName(exePath string) {
	if runtime.GOOS != "windows" || strings.TrimSpace(exePath) == "" {
		return
	}

	name := filepath.Base(exePath)
	if strings.TrimSpace(name) == "" {
		return
	}
	cmd := exec.Command("taskkill", "/F", "/IM", name)
	hideWindow(cmd)
	_ = cmd.Run()
}

func (a *App) closeBuildTargetProcesses(t buildTarget, root string) {
	killedTracked := a.killTrackedProcessesForApp(t.ID)
	if killedTracked > 0 {
		a.emitLog(fmt.Sprintf("  stopped %d tracked instance(s) for %s", killedTracked, t.Name), "info")
	}

	seen := map[string]struct{}{}
	paths := make([]string, 0)
	if t.OutExe != "" {
		full := filepath.Join(root, t.OutExe)
		paths = append(paths, full)
		seen[strings.ToLower(full)] = struct{}{}
	}
	for _, rel := range t.CleanPaths {
		full := filepath.Join(root, rel)
		low := strings.ToLower(full)
		if _, ok := seen[low]; ok {
			continue
		}
		seen[low] = struct{}{}
		paths = append(paths, full)
	}

	for _, p := range paths {
		a.stopProcessByExePath(p)
		a.stopProcessByExeName(p)
	}
}

func (a *App) removeWithRetries(full string) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := os.Remove(full); err == nil {
			return nil
		} else {
			lastErr = err
		}

		// Try to release any file lock from still-running instances.
		a.stopProcessByExePath(full)
		a.stopProcessByExeName(full)
		time.Sleep(150 * time.Millisecond)
	}
	return lastErr
}

func (a *App) buildOne(t buildTarget, root string) error {
	a.closeBuildTargetProcesses(t, root)

	// Delete old exes before building
	for _, rel := range t.CleanPaths {
		full := filepath.Join(root, rel)
		if fileExists(full) {
			a.emitLog(fmt.Sprintf("  rm  %s", rel), "info")
			if err := a.removeWithRetries(full); err != nil {
				a.emitLog(fmt.Sprintf("  warn: could not remove %s: %v", rel, err), "error")
				a.emitLog("  hint: run App Launcher as Administrator if process permissions block cleanup", "error")
			}
		}
	}

	srcDir := filepath.Join(root, t.SrcDir)
	switch t.BuildType {
	case "wails":
		exe := findTool("wails")
		a.emitLog(fmt.Sprintf("  wails build  [dir: %s]", t.SrcDir), "info")
		return a.runCmd(srcDir, exe, "build")
	case "go":
		exe := findTool("go")
		a.emitLog(fmt.Sprintf("  go build .   [dir: %s]", t.SrcDir), "info")
		return a.runCmd(srcDir, exe, "build", ".")
	default:
		return fmt.Errorf("unknown build type %q", t.BuildType)
	}
}

// BuildAll removes old exes and rebuilds every known app.
// Progress is streamed as "buildLog" events; returns "building" immediately.
func (a *App) BuildAll() string {
	a.mu.Lock()
	if a.building {
		a.mu.Unlock()
		return "build already in progress"
	}
	a.building = true
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.building = false
			a.mu.Unlock()
			a.RefreshApps()
			a.emitLog("笊絶武笊絶武 All builds complete 笊絶武笊絶武", "success")
		}()

		root := a.resolveWorkspaceRoot()
		targets := knownBuildTargets()

		a.emitLog(fmt.Sprintf("Root: %s", root), "info")
		a.emitLog(fmt.Sprintf("Building %d app(s)窶ｦ", len(targets)), "info")

		for _, t := range targets {
			a.emitLog(fmt.Sprintf("笆ｶ [%s]", t.Name), "info")
			start := time.Now()
			if err := a.buildOne(t, root); err != nil {
				a.emitLog(fmt.Sprintf("笨・[%s] FAILED: %v", t.Name, err), "error")
			} else {
				a.emitLog(fmt.Sprintf("笨・[%s] done (%.1fs)", t.Name, time.Since(start).Seconds()), "success")
			}
		}
	}()

	return "building"
}

// BuildSingle removes old exe and rebuilds one app by ID.
func (a *App) BuildSingle(appID string) string {
	root := a.resolveWorkspaceRoot()
	targets := knownBuildTargets()

	var found *buildTarget
	for i := range targets {
		if targets[i].ID == appID {
			found = &targets[i]
			break
		}
	}
	if found == nil {
		return fmt.Sprintf("no build target for %q", appID)
	}

	a.mu.Lock()
	if a.building {
		a.mu.Unlock()
		return "build already in progress"
	}
	a.building = true
	a.mu.Unlock()

	go func(t buildTarget) {
		defer func() {
			a.mu.Lock()
			a.building = false
			a.mu.Unlock()
			a.RefreshApps()
			a.emitLog("笊絶武笊絶武 Build complete 笊絶武笊絶武", "success")
		}()

		a.emitLog(fmt.Sprintf("笆ｶ [%s]", t.Name), "info")
		start := time.Now()
		if err := a.buildOne(t, root); err != nil {
			a.emitLog(fmt.Sprintf("笨・[%s] FAILED: %v", t.Name, err), "error")
		} else {
			a.emitLog(fmt.Sprintf("笨・[%s] done (%.1fs)", t.Name, time.Since(start).Seconds()), "success")
		}
	}(*found)

	return "building"
}

// GetBuildTargets returns known build target IDs/names for the UI.
func (a *App) GetBuildTargets() []map[string]string {
	targets := knownBuildTargets()
	out := make([]map[string]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, map[string]string{"id": t.ID, "name": t.Name})
	}
	return out
}

// IsBuilding reports whether a build is currently running.
func (a *App) IsBuilding() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.building
}

// KillApp forcibly stops all tracked instances for a specific app ID.
func (a *App) KillApp(appID string) string {
	a.emitLog(fmt.Sprintf("KillApp invoked for %s", appID), "info")

	root := a.resolveWorkspaceRoot()
	targets := knownBuildTargets()

	var target *buildTarget
	for i := range targets {
		if targets[i].ID == appID {
			target = &targets[i]
			break
		}
	}

	if target == nil {
		// Even if not a known target, try to kill any tracked processes with this prefix
		killed := a.killTrackedProcessesForApp(appID)
		a.RefreshApps()
		return fmt.Sprintf("killed %d untracked instance(s) for %s", killed, appID)
	}

	a.closeBuildTargetProcesses(*target, root)
	a.RefreshApps()
	return "ok"
}

// 笏笏 Main 笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏笏

func (a *App) shutdown(ctx context.Context) {
	a.emitLog("Shutdown requested", "info")

	// Respect user preference: only stop children if explicitly enabled.
	a.mu.Lock()
	stopChildren := a.stopChildrenOnExit
	a.mu.Unlock()

	if !stopChildren {
		a.emitLog("Shutdown: not terminating other apps (stopChildrenOnExit=false)", "info")
		return
	}

	a.emitLog("Shutdown: stopping processes", "info")

	root := a.resolveWorkspaceRoot()
	targets := knownBuildTargets()
	for _, t := range targets {
		a.closeBuildTargetProcesses(t, root)
	}

	// Stop G-Earth explicitly
	gPath := a.gEarthExePath()
	a.stopProcessByExePath(gPath)
	a.stopProcessByExeName(gPath)

	// Kill any remaining tracked processes
	a.mu.Lock()
	keys := make([]string, 0, len(a.processes))
	procs := make([]*os.Process, 0, len(a.processes))
	for k, p := range a.processes {
		keys = append(keys, k)
		procs = append(procs, p)
	}
	a.mu.Unlock()

	for i, p := range procs {
		if p == nil {
			continue
		}
		if err := p.Kill(); err == nil {
			a.emitLog(fmt.Sprintf("  killed tracked process %s", keys[i]), "info")
		} else {
			a.emitLog(fmt.Sprintf("  warn: could not kill tracked process %s: %v", keys[i], err), "error")
		}
	}

	a.mu.Lock()
	a.processes = map[string]*os.Process{}
	a.mu.Unlock()

	// Final fallback on Windows: taskkill known names
	if runtime.GOOS == "windows" {
		tk1 := exec.Command("taskkill", "/F", "/IM", "G-Earth.exe")
		hideWindow(tk1)
		_ = tk1.Run()
		for _, t := range targets {
			for _, rel := range append([]string{t.OutExe}, t.CleanPaths...) {
				if strings.TrimSpace(rel) == "" {
					continue
				}
				name := filepath.Base(rel)
				if name == "" {
					continue
				}
				tk2 := exec.Command("taskkill", "/F", "/IM", name)
				hideWindow(tk2)
				_ = tk2.Run()
			}
		}
	}

	a.emitLog("Shutdown complete", "info")
}

// KillAllTasks forcibly stops all known/ tracked child processes immediately
// and clears the tracked process list. Returns a status string for UI display.
func (a *App) KillAllTasks() string {
	a.emitLog("KillAllTasks invoked", "info")

	root := a.resolveWorkspaceRoot()
	targets := knownBuildTargets()

	for _, t := range targets {
		a.closeBuildTargetProcesses(t, root)
	}

	// Stop G-Earth explicitly
	gPath := a.gEarthExePath()
	a.stopProcessByExePath(gPath)
	a.stopProcessByExeName(gPath)

	// Kill any remaining tracked processes
	a.mu.Lock()
	keys := make([]string, 0, len(a.processes))
	procs := make([]*os.Process, 0, len(a.processes))
	for k, p := range a.processes {
		keys = append(keys, k)
		procs = append(procs, p)
	}
	a.mu.Unlock()

	killed := 0
	for i, p := range procs {
		if p == nil {
			continue
		}
		if err := p.Kill(); err == nil {
			killed++
			a.emitLog(fmt.Sprintf("  killed tracked process %s", keys[i]), "info")
		} else {
			a.emitLog(fmt.Sprintf("  warn: could not kill tracked process %s: %v", keys[i], err), "error")
		}
	}

	a.mu.Lock()
	a.processes = map[string]*os.Process{}
	a.mu.Unlock()

	// Final fallback on Windows: taskkill known names
	if runtime.GOOS == "windows" {
		tk1 := exec.Command("taskkill", "/F", "/IM", "G-Earth.exe")
		hideWindow(tk1)
		_ = tk1.Run()
		for _, t := range targets {
			for _, rel := range append([]string{t.OutExe}, t.CleanPaths...) {
				if strings.TrimSpace(rel) == "" {
					continue
				}
				name := filepath.Base(rel)
				if name == "" {
					continue
				}
				tk2 := exec.Command("taskkill", "/F", "/IM", name)
				hideWindow(tk2)
				_ = tk2.Run()
			}
		}
	}

	return fmt.Sprintf("killed %d tracked process(es)", killed)
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			f, _ := os.OpenFile("crash.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if f != nil {
				now := time.Now().Format("2006-01-02 15:04:05")
				stack := make([]byte, 4096)
				stack = stack[:runtime.Stack(stack, false)]
				_, _ = f.WriteString(fmt.Sprintf("[%s] PANIC: %v\n%s\n", now, r, stack))
				f.Close()
			}
			time.Sleep(time.Second)
		}
	}()
	app := NewApp()

	err := wails.Run(&options.App{
		Title:             "App Launcher",
		Width:             680,
		Height:            600,
		MinWidth:          560,
		MinHeight:         460,
		DisableResize:     false,
		StartHidden:       false,
		HideWindowOnClose: false,
		OnStartup:         app.startup,
		OnShutdown:        app.shutdown,
		Bind:              []interface{}{app},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 14, G: 18, B: 25, A: 1},
	})

	if err != nil {
		panic(err)
	}
}
