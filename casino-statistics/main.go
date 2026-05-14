package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

type TradeItem struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	RawData  string `json:"rawData"`
}

type GameHistoryEntry struct {
	ID               string      `json:"id"`
	PlayerName       string      `json:"playerName"`
	StartedAt        string      `json:"startedAt"`
	UpdatedAt        string      `json:"updatedAt"`
	CompletedAt      string      `json:"completedAt,omitempty"`
	Game             string      `json:"game"`
	Winner           string      `json:"winner"`
	Status           string      `json:"status"`
	Issue            bool        `json:"issue"`
	IssueReason      string      `json:"issueReason"`
	PlayerResult     string      `json:"playerResult"`
	DealerResult     string      `json:"dealerResult"`
	BetItems         []TradeItem `json:"betItems"`
	PayoutItems      []TradeItem `json:"payoutItems"`
	Choice           string      `json:"choice,omitempty"`
	ChoiceShout      string      `json:"choiceShout,omitempty"`
	PayoutMultiplier float64     `json:"payoutMultiplier,omitempty"`
}

type GameStats struct {
	Game          string  `json:"game"`
	TotalRounds   int     `json:"totalRounds"`
	PlayerWins    int     `json:"playerWins"`
	DealerWins    int     `json:"dealerWins"`
	PlayerWinRate float64 `json:"playerWinRate"`
	DealerWinRate float64 `json:"dealerWinRate"`
}

type CasinoStats struct {
	Overall GameStats            `json:"overall"`
	ByGame  map[string]GameStats `json:"byGame"`
}

type App struct {
	ctx      context.Context
	db       *pgxpool.Pool
	ownerKey string
	dbStatus string
	mu       sync.Mutex
	initWg   sync.WaitGroup
}

func NewApp() *App {
	a := &App{
		dbStatus: "Disconnected",
	}
	a.initWg.Add(1)
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	runtime.WindowShow(ctx)
	a.initDB()
}

func (a *App) GetDbStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dbStatus
}

func (a *App) initDB() {
	defer a.initWg.Done()

	a.mu.Lock()
	a.dbStatus = "Initializing..."
	a.mu.Unlock()

	// Try to find db.local.json by looking in CWD and then walking up
	found := false
	absConfigPath := ""

	// Check current dir and up to 4 levels up
	checkPath := "db.local.json"
	for i := 0; i < 5; i++ {
		abs, _ := filepath.Abs(checkPath)
		log.Printf("[DB_INIT] Checking for config at: %s", abs)
		if _, err := os.Stat(abs); err == nil {
			absConfigPath = abs
			found = true
			break
		}
		checkPath = filepath.Join("..", checkPath)
	}

	if !found {
		msg := "Failed to find db.local.json in current or parent directories"
		log.Printf("[DB_INIT] %s", msg)
		a.mu.Lock()
		a.dbStatus = "Config not found"
		a.mu.Unlock()
		return
	}

	log.Printf("[DB_INIT] Using config at: %s", absConfigPath)

	data, err := os.ReadFile(absConfigPath)
	if err != nil {
		log.Printf("[DB_INIT] Failed to read db.local.json: %v", err)
		a.mu.Lock()
		a.dbStatus = "Read error"
		a.mu.Unlock()
		return
	}

	var cfg struct {
		DatabaseURL string `json:"databaseUrl"`
		OwnerKey    string `json:"ownerKey"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("[DB_INIT] Failed to unmarshal db.local.json: %v", err)
		a.mu.Lock()
		a.dbStatus = "Parse error"
		a.mu.Unlock()
		return
	}

	a.ownerKey = cfg.OwnerKey
	log.Printf("[DB_INIT] Connecting to database... (ownerKey: %s)", a.ownerKey)
	
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Printf("[DB_INIT] Failed to create connection pool: %v", err)
		a.mu.Lock()
		a.dbStatus = "Connection pool error"
		a.mu.Unlock()
		return
	}
	
	// Test connection
	if err := pool.Ping(context.Background()); err != nil {
		log.Printf("[DB_INIT] Failed to ping database: %v", err)
		a.mu.Lock()
		a.dbStatus = "Connection failed (Ping)"
		a.mu.Unlock()
		return
	}

	a.db = pool

	// Create blocked_players table if not exists
	_, err = a.db.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS blocked_players (
			player_name TEXT PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		log.Printf("[DB_INIT] Failed to create blocked_players table: %v", err)
	}

	// Create ui_settings table if not exists
	_, err = a.db.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS ui_settings (
			key TEXT PRIMARY KEY,
			value JSONB NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		log.Printf("[DB_INIT] Failed to create ui_settings table: %v", err)
	}

	a.mu.Lock()
	a.dbStatus = "Connected"
	a.mu.Unlock()
}

func (a *App) GetSettings(key string) string {
	a.initWg.Wait()
	if a.db == nil {
		return "{}"
	}
	var val string
	err := a.db.QueryRow(context.Background(), "SELECT value FROM ui_settings WHERE key = $1", key).Scan(&val)
	if err != nil {
		return "{}"
	}
	return val
}

func (a *App) SaveSettings(key string, valueJSON string) {
	a.initWg.Wait()
	if a.db == nil {
		return
	}
	_, err := a.db.Exec(context.Background(), `
		INSERT INTO ui_settings (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()
	`, key, valueJSON)
	if err != nil {
		log.Printf("[SETTINGS] Failed to save %s: %v", key, err)
	}
}

func (a *App) GetBlockedPlayers() []string {
	a.initWg.Wait()

	if a.db == nil {
		return []string{}
	}
	rows, err := a.db.Query(context.Background(), "SELECT player_name FROM blocked_players ORDER BY player_name")
	if err != nil {
		return []string{}
	}
	defer rows.Close()

	var players []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			players = append(players, name)
		}
	}
	return players
}

func (a *App) ToggleBlockPlayer(name string) {
	a.initWg.Wait()

	if a.db == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	var exists bool
	err := a.db.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM blocked_players WHERE player_name = $1)", name).Scan(&exists)
	if err != nil {
		return
	}

	if exists {
		log.Printf("[BLOCK] Unblocking player: %s", name)
		_, _ = a.db.Exec(context.Background(), "DELETE FROM blocked_players WHERE player_name = $1", name)
	} else {
		log.Printf("[BLOCK] Blocking player: %s", name)
		_, _ = a.db.Exec(context.Background(), "INSERT INTO blocked_players (player_name) VALUES ($1)", name)
	}
}

type PlayerStats struct {
	Name          string  `json:"name"`
	TotalRounds   int     `json:"totalRounds"`
	PlayerWins    int     `json:"playerWins"`
	DealerWins    int     `json:"dealerWins"`
	PlayerWinRate float64 `json:"playerWinRate"`
	DealerWinRate float64 `json:"dealerWinRate"`
	DealerEdge    float64 `json:"dealerEdge"`
}

func (a *App) GetOwnerKey() string {
	a.initWg.Wait()
	return a.ownerKey
}

func (a *App) GetPlayerStats(startDate, endDate string) []PlayerStats {
	a.initWg.Wait()
	if a.db == nil {
		log.Printf("[PLAYER_STATS] DB not connected")
		return []PlayerStats{}
	}

	query := `
		SELECT player_name, winner, status, issue, completed_at
		FROM game_history_entries
		WHERE owner_key = $1
	`
	args := []interface{}{a.ownerKey}
	if startDate != "" {
		query += fmt.Sprintf(" AND completed_at >= $%d", len(args)+1)
		args = append(args, startDate)
	}
	if endDate != "" {
		query += fmt.Sprintf(" AND completed_at <= $%d", len(args)+1)
		args = append(args, endDate)
	}

	rows, err := a.db.Query(context.Background(), query, args...)
	if err != nil {
		log.Printf("[PLAYER_STATS] Query failed: %v", err)
		return []PlayerStats{}
	}
	defer rows.Close()

	playerMap := make(map[string]*PlayerStats)
	rowCount := 0
	skippedCount := 0
	for rows.Next() {
		rowCount++
		var name, winner, status, completedAt string
		var issue bool
		if err := rows.Scan(&name, &winner, &status, &issue, &completedAt); err != nil {
			log.Printf("[PLAYER_STATS] Scan error at row %d: %v", rowCount, err)
			continue
		}

		statusClean := strings.ToLower(status)
		if statusClean != "completed" || issue {
			skippedCount++
			continue
		}

		winnerClean := strings.TrimSpace(strings.ToLower(winner))
		playerClean := strings.TrimSpace(strings.ToLower(name))
		
		isPlayerWin := winnerClean == playerClean
		isDealerWin := winnerClean == "dealer"

		if !isPlayerWin && !isDealerWin {
			if winnerClean == "player" {
				isPlayerWin = true
			} else {
				skippedCount++
				continue
			}
		}

		ps, ok := playerMap[name]
		if !ok {
			ps = &PlayerStats{Name: name}
			playerMap[name] = ps
		}

		ps.TotalRounds++
		if isPlayerWin {
			ps.PlayerWins++
		} else {
			ps.DealerWins++
		}
	}

	log.Printf("[PLAYER_STATS] Owner=%s, Range=%s to %s, Rows=%d, Skipped=%d, Players=%d", a.ownerKey, startDate, endDate, rowCount, skippedCount, len(playerMap))

	result := []PlayerStats{}
	for _, ps := range playerMap {
		if ps.TotalRounds > 0 {
			ps.PlayerWinRate = float64(ps.PlayerWins) / float64(ps.TotalRounds) * 100
			ps.DealerWinRate = float64(ps.DealerWins) / float64(ps.TotalRounds) * 100
			ps.DealerEdge = ps.DealerWinRate - ps.PlayerWinRate
		}
		result = append(result, *ps)
	}

	return result
}

func (a *App) GetPlayers() []string {
	a.initWg.Wait()

	if a.db == nil {
		return []string{}
	}
	rows, err := a.db.Query(context.Background(), "SELECT DISTINCT player_name FROM game_history_entries WHERE owner_key = $1 ORDER BY player_name", a.ownerKey)
	if err != nil {
		return []string{}
	}
	defer rows.Close()

	var players []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			players = append(players, name)
		}
	}
	return players
}

func normalizeGameName(game string, choice string) string {
	g := strings.TrimSpace(strings.ToLower(game))
	
	// MidHouse Split
	if strings.Contains(g, "mid") || strings.Contains(g, "house") || g == "mh" {
		c := strings.TrimSpace(strings.ToLower(choice))
		if strings.Contains(c, "u10") {
			return "U10"
		}
		if strings.Contains(c, "o11") {
			return "O11"
		}
		return "MidHouse"
	}

	// Broadly catch Under/Over games
	if strings.Contains(g, "uo") || strings.Contains(g, "under") || strings.Contains(g, "over") {
		c := strings.TrimSpace(strings.ToLower(choice))
		
		// Check for specific words first to avoid 'uo_over' matching 'u'
		if strings.Contains(c, "over") {
			return "O7"
		}
		if strings.Contains(c, "under") {
			return "U7"
		}
		
		// Then check for single letters
		if c == "o" {
			return "O7"
		}
		if c == "u" {
			return "U7"
		}
		
		if c == "7" || strings.Contains(c, "7") {
			return "7"
		}
		// Fallback to U7 if choice is unclear
		return "U7"
	}
	
	if strings.Contains(g, "double") || g == "dt" || strings.Contains(g, "doubletrouble") {
		return "DT"
	}
	if strings.Contains(g, "tri") {
		return "Tri"
	}
	if strings.Contains(g, "pair") || strings.Contains(g, "pu") {
		return "PU"
	}

	switch g {
	case "poker", "pkr":
		return "Poker"
	case "21", "blackjack", "black jack":
		return "21"
	case "13", "thirteen":
		return "13"
	case "6", "six":
		return "6"
	case "h18":
		return "H18"
	case "bandit", "onearmbandit", "oab":
		return "Bandit"
	default:
		return "Other"
	}
}

var BuildTime = "2026-05-14T10:15:00"

func (a *App) GetStats(startDate, endDate string) CasinoStats {
	a.initWg.Wait()
	log.Printf("[STATS] GetStats called (Version: %s)", BuildTime)

	if a.db == nil {
		log.Printf("[STATS] DB not connected, returning empty stats")
		return CasinoStats{}
	}

	blocked := make(map[string]bool)
	for _, p := range a.GetBlockedPlayers() {
		blocked[strings.ToLower(p)] = true
	}

	query := `
		SELECT player_name, game, winner, choice, completed_at, status, issue
		FROM game_history_entries
		WHERE owner_key = $1
	`
	args := []interface{}{a.ownerKey}
	// Note: We'll filter status and issue in Go for now to see EVERYTHING in the logs
	if startDate != "" {
		query += fmt.Sprintf(" AND completed_at >= $%d", len(args)+1)
		args = append(args, startDate)
	}
	if endDate != "" {
		query += fmt.Sprintf(" AND completed_at <= $%d", len(args)+1)
		args = append(args, endDate)
	}

	rows, err := a.db.Query(context.Background(), query, args...)
	if err != nil {
		log.Printf("[STATS] Query failed: %v", err)
		return CasinoStats{}
	}
	defer rows.Close()

	stats := CasinoStats{
		ByGame: make(map[string]GameStats),
	}

	rowCount := 0
	gameCount := make(map[string]int)
	for rows.Next() {
		rowCount++
		var playerName, game, winner, choice, completedAt, status string
		var issue bool
		if err := rows.Scan(&playerName, &game, &winner, &choice, &completedAt, &status, &issue); err != nil {
			log.Printf("[STATS] Scan error at row %d: %v", rowCount, err)
			continue
		}

		if blocked[strings.ToLower(playerName)] {
			continue
		}

		// Filter here in Go
		if strings.ToLower(status) != "completed" || issue {
			continue
		}

		normGame := normalizeGameName(game, choice)
		gameCount[normGame]++
		
		winnerClean := strings.TrimSpace(strings.ToLower(winner))
		playerClean := strings.TrimSpace(strings.ToLower(playerName))
		
		isPlayerWin := winnerClean == playerClean
		isDealerWin := winnerClean == "dealer"

		if !isPlayerWin && !isDealerWin {
			// Some games might have winner as 'Player' or other strings, try to catch 'player' as well
			if winnerClean == "player" {
				isPlayerWin = true
			} else {
				continue
			}
		}

		// Update Overall
		stats.Overall.TotalRounds++
		if isPlayerWin {
			stats.Overall.PlayerWins++
		} else {
			stats.Overall.DealerWins++
		}

		// Update ByGame
		gs := stats.ByGame[normGame]
		gs.Game = normGame
		gs.TotalRounds++
		if isPlayerWin {
			gs.PlayerWins++
		} else {
			gs.DealerWins++
		}
		stats.ByGame[normGame] = gs
	}

	log.Printf("[STATS] Processed %d rows (Owner: %s). Game counts: %v", rowCount, a.ownerKey, gameCount)

	// Compute win rates
	if stats.Overall.TotalRounds > 0 {
		stats.Overall.PlayerWinRate = float64(stats.Overall.PlayerWins) / float64(stats.Overall.TotalRounds) * 100
		stats.Overall.DealerWinRate = float64(stats.Overall.DealerWins) / float64(stats.Overall.TotalRounds) * 100
	}
	for k, gs := range stats.ByGame {
		if gs.TotalRounds > 0 {
			gs.PlayerWinRate = float64(gs.PlayerWins) / float64(gs.TotalRounds) * 100
			gs.DealerWinRate = float64(gs.DealerWins) / float64(gs.TotalRounds) * 100
		}
		stats.ByGame[k] = gs
	}

	return stats
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Casino Statistics",
		Width:  800,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 44, A: 1},
	})

	if err != nil {
		log.Fatal(err)
	}
}
