package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
	Qty      int    `json:"qty,omitempty"`
	RawData  string `json:"rawData,omitempty"`
	RawName  string `json:"raw_name,omitempty"`
}

type DiceRoll struct {
	DiceID int `json:"diceId"`
	Value  int `json:"value"`
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
	Rolls            []DiceRoll  `json:"rolls,omitempty"`
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

type LedgerItemStats struct {
	Name     string `json:"name"`
	TotalIn  int    `json:"totalIn"`
	TotalOut int    `json:"totalOut"`
	Net      int    `json:"net"`
}

type DiscordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type DiscordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Color       int                 `json:"color"`
	Fields      []DiscordEmbedField `json:"fields"`
	Timestamp   string              `json:"timestamp"`
	Footer      struct {
		Text string `json:"text"`
	} `json:"footer"`
}

type DiscordWebhookPayload struct {
	Username  string         `json:"username"`
	AvatarURL string         `json:"avatar_url"`
	Embeds    []DiscordEmbed `json:"embeds"`
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
			id SERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL,
			player_name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (owner_key, player_name)
		)
	`)
	if err != nil {
		log.Printf("[DB_INIT] Failed to create blocked_players table: %v", err)
	} else {
		// Create lowercase index if it doesn't exist
		_, _ = a.db.Exec(context.Background(), "CREATE UNIQUE INDEX IF NOT EXISTS idx_blocked_players_lower_name ON blocked_players (LOWER(player_name))")

		// Normalize existing names to lowercase
		_, err = a.db.Exec(context.Background(), "UPDATE blocked_players SET player_name = LOWER(player_name) WHERE player_name != LOWER(player_name)")
		if err != nil {
			log.Printf("[DB_INIT] Failed to normalize blocked_players: %v", err)
		}
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
	// Filter by owner key to match the actual schema
	rows, err := a.db.Query(context.Background(), "SELECT player_name FROM blocked_players WHERE owner_key = $1 ORDER BY player_name", a.ownerKey)
	if err != nil {
		log.Printf("[BLOCK] Failed to query blocked players: %v", err)
		return []string{}
	}
	defer rows.Close()

	var players []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			players = append(players, strings.ToLower(strings.TrimSpace(name)))
		}
	}
	return players
}

func (a *App) ToggleBlockPlayer(name string) {
	a.initWg.Wait()

	if a.db == nil {
		log.Printf("[BLOCK] DB is nil, cannot toggle block for %s", name)
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		log.Printf("[BLOCK] Attempted to toggle block for empty name")
		return
	}

	log.Printf("[BLOCK] Toggle request for: '%s' (owner: %s)", name, a.ownerKey)

	var exists bool
	// Check existence for THIS owner
	err := a.db.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM blocked_players WHERE owner_key = $1 AND LOWER(player_name) = $2)", a.ownerKey, name).Scan(&exists)
	if err != nil {
		log.Printf("[BLOCK] Error checking existence for %s: %v", name, err)
		return
	}

	log.Printf("[BLOCK] Player '%s' exists in blocked list for %s: %v", name, a.ownerKey, exists)

	if exists {
		log.Printf("[BLOCK] Unblocking player: %s", name)
		tag, err := a.db.Exec(context.Background(), "DELETE FROM blocked_players WHERE owner_key = $1 AND LOWER(player_name) = $2", a.ownerKey, name)
		if err != nil {
			log.Printf("[BLOCK] Failed to unblock player %s: %v", name, err)
		} else {
			log.Printf("[BLOCK] Unblock successful for %s. Rows affected: %d", name, tag.RowsAffected())
		}
	} else {
		log.Printf("[BLOCK] Blocking player: %s", name)
		// The actual table has a unique index on (owner_key, player_name)
		tag, err := a.db.Exec(context.Background(), "INSERT INTO blocked_players (owner_key, player_name) VALUES ($1, $2) ON CONFLICT (owner_key, player_name) DO NOTHING", a.ownerKey, name)
		if err != nil {
			log.Printf("[BLOCK] Failed to block player %s: %v", name, err)
		} else {
			log.Printf("[BLOCK] Block successful for %s. Rows affected: %d", name, tag.RowsAffected())
		}
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
		WHERE 1=1
	`
	args := []interface{}{}
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

		trimmedName := strings.TrimSpace(name)
		ps, ok := playerMap[trimmedName]
		if !ok {
			ps = &PlayerStats{Name: trimmedName}
			playerMap[trimmedName] = ps
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

func (a *App) GetPlayerGameStats(playerName, startDate, endDate string) []GameStats {
	a.initWg.Wait()
	if a.db == nil {
		return []GameStats{}
	}

	query := `
		SELECT game, winner, status, issue, completed_at
		FROM game_history_entries
		WHERE 1=1 AND TRIM(player_name) = $1
	`
	args := []interface{}{strings.TrimSpace(playerName)}
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
		log.Printf("[PLAYER_GAME_STATS] Query failed: %v", err)
		return []GameStats{}
	}
	defer rows.Close()

	gameMap := make(map[string]*GameStats)
	for rows.Next() {
		var game, winner, status, completedAt string
		var issue bool
		if err := rows.Scan(&game, &winner, &status, &issue, &completedAt); err != nil {
			continue
		}

		if strings.ToLower(status) != "completed" || issue {
			continue
		}

		normGame := normalizeGameName(game, "") // choice info missing in this query, but normalize should handle it

		winnerClean := strings.TrimSpace(strings.ToLower(winner))
		playerClean := strings.TrimSpace(strings.ToLower(playerName))

		isPlayerWin := winnerClean == playerClean
		isDealerWin := winnerClean == "dealer"

		if !isPlayerWin && !isDealerWin {
			if winnerClean == "player" {
				isPlayerWin = true
			} else {
				continue
			}
		}

		gs, ok := gameMap[normGame]
		if !ok {
			gs = &GameStats{Game: normGame}
			gameMap[normGame] = gs
		}

		gs.TotalRounds++
		if isPlayerWin {
			gs.PlayerWins++
		} else {
			gs.DealerWins++
		}
	}

	result := []GameStats{}
	for _, gs := range gameMap {
		if gs.TotalRounds > 0 {
			gs.PlayerWinRate = float64(gs.PlayerWins) / float64(gs.TotalRounds) * 100
			gs.DealerWinRate = float64(gs.DealerWins) / float64(gs.TotalRounds) * 100
		}
		result = append(result, *gs)
	}

	return result
}

func (a *App) GetPlayers() []string {
	a.initWg.Wait()

	if a.db == nil {
		return []string{}
	}
	rows, err := a.db.Query(context.Background(), "SELECT DISTINCT player_name FROM game_history_entries ORDER BY player_name")
	if err != nil {
		return []string{}
	}
	defer rows.Close()

	var players []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			players = append(players, strings.TrimSpace(name))
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
			return "MidHouse U10"
		}
		if strings.Contains(c, "o11") {
			return "MidHouse O11"
		}
		return "MidHouse"
	}

	// Broadly catch Under/Over games
	if strings.Contains(g, "uo") || strings.Contains(g, "under") || strings.Contains(g, "over") {
		c := strings.TrimSpace(strings.ToLower(choice))

		if strings.Contains(c, "over") || c == "o" {
			return "O7"
		}
		if strings.Contains(c, "under") || c == "u" {
			return "U7"
		}
		if c == "7" || strings.Contains(c, "7") {
			return "7"
		}

		// If it's just "uo" or "uo7" without choice, return UO
		if g == "uo" || g == "uo7" {
			return "UO"
		}
		return "U7"
	}

	if strings.Contains(g, "double") || g == "dt" || strings.Contains(g, "doubletrouble") {
		return "DT"
	}
	if strings.Contains(g, "tri") {
		c := strings.TrimSpace(strings.ToLower(choice))
		if strings.Contains(c, "high") || strings.Contains(c, "trih") {
			return "TriH"
		}
		if strings.Contains(c, "low") || strings.Contains(c, "tril") {
			return "TriL"
		}
		return "TriL"
	}
	if strings.Contains(g, "pair") || strings.Contains(g, "pu") {
		return "Pair Up"
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
		// Try to capitalize first letter if it's a short word
		if len(g) > 0 {
			return strings.Title(g)
		}
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
		WHERE 1=1
	`
	args := []interface{}{}
	// Note: We'll filter status and issue in Go for now to see EVERYTHING in the logs
	if startDate != "" {
		query += fmt.Sprintf(" AND NULLIF(completed_at, '')::timestamptz >= $%d::timestamptz", len(args)+1)
		args = append(args, startDate)
	}
	if endDate != "" {
		query += fmt.Sprintf(" AND NULLIF(completed_at, '')::timestamptz <= $%d::timestamptz", len(args)+1)
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

		if blocked[strings.ToLower(strings.TrimSpace(playerName))] {
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

func (a *App) GetLedgerStats(startDate, endDate string) []LedgerItemStats {
	a.initWg.Wait()
	if a.db == nil {
		return []LedgerItemStats{}
	}

	blocked := make(map[string]bool)
	for _, p := range a.GetBlockedPlayers() {
		blocked[strings.ToLower(p)] = true
	}

	query := `
		SELECT partner_name, trade_type, items
		FROM trade_ledger
		WHERE 1=1
	`
	args := []interface{}{}
	if startDate != "" {
		query += fmt.Sprintf(" AND created_at >= $%d", len(args)+1)
		args = append(args, startDate)
	}
	if endDate != "" {
		query += fmt.Sprintf(" AND created_at <= $%d", len(args)+1)
		args = append(args, endDate)
	}

	rows, err := a.db.Query(context.Background(), query, args...)
	if err != nil {
		log.Printf("[LEDGER_STATS] Query failed: %v", err)
		return []LedgerItemStats{}
	}
	defer rows.Close()

	itemMap := make(map[string]*LedgerItemStats)
	for rows.Next() {
		var partnerName, tradeType string
		var itemsJSON []byte
		if err := rows.Scan(&partnerName, &tradeType, &itemsJSON); err != nil {
			continue
		}

		if blocked[strings.ToLower(strings.TrimSpace(partnerName))] {
			continue
		}

		var items []TradeItem
		if err := json.Unmarshal(itemsJSON, &items); err != nil {
			continue
		}

		for _, it := range items {
			name := strings.TrimSpace(it.Name)
			if name == "" && strings.TrimSpace(it.RawName) != "" {
				name = strings.TrimSpace(it.RawName)
			}
			if name == "" {
				continue
			}
			qty := it.Quantity
			if qty == 0 {
				qty = it.Qty
			}
			ls, ok := itemMap[name]
			if !ok {
				ls = &LedgerItemStats{Name: name}
				itemMap[name] = ls
			}

			if tradeType == "IN" {
				ls.TotalIn += qty
			} else if tradeType == "OUT" {
				ls.TotalOut += qty
			}
		}
	}

	result := make([]LedgerItemStats, 0, len(itemMap))
	for _, ls := range itemMap {
		ls.Net = ls.TotalIn - ls.TotalOut
		result = append(result, *ls)
	}

	// Sort by Name
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

func (a *App) GetPlayerLedgerStats(playerName, startDate, endDate string) []LedgerItemStats {
	a.initWg.Wait()
	if a.db == nil {
		return []LedgerItemStats{}
	}

	query := `
		SELECT trade_type, items
		FROM trade_ledger
		WHERE 1=1 AND TRIM(LOWER(partner_name)) = $1
	`
	args := []interface{}{strings.TrimSpace(strings.ToLower(playerName))}
	if startDate != "" {
		query += fmt.Sprintf(" AND created_at >= $%d", len(args)+1)
		args = append(args, startDate)
	}
	if endDate != "" {
		query += fmt.Sprintf(" AND created_at <= $%d", len(args)+1)
		args = append(args, endDate)
	}

	rows, err := a.db.Query(context.Background(), query, args...)
	if err != nil {
		log.Printf("[PLAYER_LEDGER_STATS] Query failed: %v", err)
		return []LedgerItemStats{}
	}
	defer rows.Close()

	itemMap := make(map[string]*LedgerItemStats)
	for rows.Next() {
		var tradeType string
		var itemsJSON []byte
		if err := rows.Scan(&tradeType, &itemsJSON); err != nil {
			continue
		}

		var items []TradeItem
		if err := json.Unmarshal(itemsJSON, &items); err != nil {
			continue
		}

		for _, it := range items {
			name := strings.TrimSpace(it.Name)
			if name == "" && strings.TrimSpace(it.RawName) != "" {
				name = strings.TrimSpace(it.RawName)
			}
			if name == "" {
				continue
			}
			qty := it.Quantity
			if qty == 0 {
				qty = it.Qty
			}
			ls, ok := itemMap[name]
			if !ok {
				ls = &LedgerItemStats{Name: name}
				itemMap[name] = ls
			}

			if tradeType == "IN" {
				ls.TotalIn += qty
			} else if tradeType == "OUT" {
				ls.TotalOut += qty
			}
		}
	}

	result := make([]LedgerItemStats, 0, len(itemMap))
	for _, ls := range itemMap {
		ls.Net = ls.TotalIn - ls.TotalOut
		result = append(result, *ls)
	}

	// Sort by Net descending (most profitable for casino first)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Net != result[j].Net {
			return result[i].Net > result[j].Net
		}
		return result[i].Name < result[j].Name
	})

	return result
}

func (a *App) PostStatsToDiscord() string {
	a.initWg.Wait()
	if a.db == nil {
		return "Error: Database not connected"
	}

	// 1. Setup Time Ranges
	loc := time.FixedZone("GMT+10", 10*60*60)
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Format(time.RFC3339)
	todayEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, loc).Format(time.RFC3339)

	// Weekly start (Monday)
	daysSinceMonday := int(now.Weekday()) - 1
	if daysSinceMonday < 0 {
		daysSinceMonday = 6 // Sunday
	}
	weekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -daysSinceMonday).Format(time.RFC3339)

	// Fetch Blocked List for filtering
	blocked := make(map[string]bool)
	for _, p := range a.GetBlockedPlayers() {
		blocked[strings.ToLower(strings.TrimSpace(p))] = true
	}

	// 2. Fetch Global Stats (GetStats/GetLedgerStats already filter blocked players)
	statsToday := a.GetStats(todayStart, todayEnd)
	ledgerToday := a.GetLedgerStats(todayStart, todayEnd)
	statsWeek := a.GetStats(weekStart, todayEnd)
	ledgerWeek := a.GetLedgerStats(weekStart, todayEnd)
	statsLife := a.GetStats("", "")
	ledgerLife := a.GetLedgerStats("", "")

	// 3. Calculate Biggest Winner/Loser (Today - Net Items)
	rows, _ := a.db.Query(context.Background(), `
		SELECT partner_name, trade_type, items FROM trade_ledger 
		WHERE created_at >= $1 AND created_at <= $2
	`, todayStart, todayEnd)

	type PlayerNet struct {
		Name  string
		Total int
	}
	playerNets := make(map[string]*PlayerNet)

	for rows.Next() {
		var name, ttype string
		var itemsJSON []byte
		rows.Scan(&name, &ttype, &itemsJSON)

		if blocked[strings.ToLower(strings.TrimSpace(name))] {
			continue
		}

		var items []TradeItem
		json.Unmarshal(itemsJSON, &items)

		pn, ok := playerNets[name]
		if !ok {
			pn = &PlayerNet{Name: name}
			playerNets[name] = pn
		}
		for _, it := range items {
			qty := it.Quantity
			if qty == 0 {
				qty = it.Qty
			}
			if ttype == "IN" {
				pn.Total -= qty
			} else {
				pn.Total += qty
			}
		}
	}
	rows.Close()

	var bigWinner, bigLoser *PlayerNet
	for _, pn := range playerNets {
		if bigWinner == nil || pn.Total > bigWinner.Total {
			bigWinner = pn
		}
		if bigLoser == nil || pn.Total < bigLoser.Total {
			bigLoser = pn
		}
	}

	// 4. Calculate Heat Map (Longest Current Streaks)
	srows, _ := a.db.Query(context.Background(), `
		SELECT game, winner, player_name FROM game_history_entries 
		WHERE status = 'Completed' AND issue = false
		ORDER BY created_at DESC LIMIT 100
	`)

	type Streak struct {
		Winner string
		Count  int
		Game   string
	}
	gameStreaks := make(map[string]*Streak)
	gamesToTrack := map[string]bool{"Poker": true, "13": true, "21": true, "TriH": true, "TriL": true}

	for srows.Next() {
		var g, w, p string
		srows.Scan(&g, &w, &p)

		if blocked[strings.ToLower(strings.TrimSpace(p))] {
			continue
		}

		norm := normalizeGameName(g, "")
		if !gamesToTrack[norm] {
			continue
		}

		str, ok := gameStreaks[norm]
		if !ok {
			winner := "Dealer"
			if strings.EqualFold(w, p) {
				winner = "Player"
			}
			gameStreaks[norm] = &Streak{Winner: winner, Count: 1, Game: norm}
		} else if str.Count > 0 {
			winner := "Dealer"
			if strings.EqualFold(w, p) {
				winner = "Player"
			}
			if str.Winner == winner {
				str.Count++
			} else {
				str.Count = -1
			}
		}
	}
	srows.Close()

	var bestDealerStreak, bestPlayerStreak *Streak
	for _, s := range gameStreaks {
		if s.Winner == "Dealer" && (bestDealerStreak == nil || s.Count > bestDealerStreak.Count) {
			bestDealerStreak = s
		}
		if s.Winner == "Player" && (bestPlayerStreak == nil || s.Count > bestPlayerStreak.Count) {
			bestPlayerStreak = s
		}
	}

	// 5. Lifetime Economy
	var firstDate time.Time
	a.db.QueryRow(context.Background(), "SELECT MIN(created_at) FROM trade_ledger").Scan(&firstDate)
	daysActive := int(now.Sub(firstDate).Hours()/24) + 1

	totalItemsIn := 0
	lifeNetItems := 0
	for _, it := range ledgerLife {
		totalItemsIn += it.TotalIn
		lifeNetItems += it.Net
	}
	dailyAvgIn := float64(totalItemsIn) / float64(daysActive)

	// 6. Build Discord Embed
	webhookURL := "https://discord.com/api/webhooks/1518982954716627094/2_SZHySpMk9b6mrEqRm1N5ndLDF6XVAWRNMLIPJry-cz5Y4ihN7OyaxXZypqsXTy0lp-"
	embed := DiscordEmbed{
		Title:     fmt.Sprintf("🎰 CASINO PERFORMANCE [%s]", now.Format("Monday, Jan 2 @ 3:04 PM")),
		Color:     0xf1c40f, // Gold
		Timestamp: now.Format(time.RFC3339),
	}
	embed.Footer.Text = "Casino Performance Bot • Vertical Report"

	truncateField := func(v string) string {
		if len(v) > 1000 {
			return v[:1000] + "…"
		}
		return v
	}

	// SECTION 1: TODAYS STATS
	dateStr := time.Now().UTC().Format("2006-01-02")
	var gbOutToday int
	a.db.QueryRow(context.Background(), "SELECT gb_out FROM public.gb_daily_stats WHERE date_str = $1", dateStr).Scan(&gbOutToday)

	dealerLoss := statsToday.Overall.TotalRounds - statsToday.Overall.DealerWins
	todayStats := fmt.Sprintf("Total Rounds: %d\nDealer Wins: %d\nDealer Loss: %d\nHouse Edge: %+.1f%%\nGold Bars Paid Out: %d / 200",
		statsToday.Overall.TotalRounds,
		statsToday.Overall.DealerWins,
		dealerLoss,
		statsToday.Overall.DealerWinRate-statsToday.Overall.PlayerWinRate,
		gbOutToday)
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "📅 Todays Stats", Value: truncateField(todayStats), Inline: false})
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "\u200b", Value: "▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬", Inline: false})

	// SECTION 2: TOP GAMES TODAY (Top 2)
	var topGames []string
	sortedGames := make([]GameStats, 0, len(statsToday.ByGame))
	for _, g := range statsToday.ByGame {
		sortedGames = append(sortedGames, g)
	}
	sort.Slice(sortedGames, func(i, j int) bool { return sortedGames[i].TotalRounds > sortedGames[j].TotalRounds })
	for i, g := range sortedGames {
		if i >= 2 {
			break
		}
		topGames = append(topGames, fmt.Sprintf("**%s**: %+.1f%% Edge", g.Game, g.DealerWinRate-g.PlayerWinRate))
	}
	if len(topGames) == 0 {
		topGames = append(topGames, "No games today.")
	}
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "🎮 Top Games Today", Value: truncateField(strings.Join(topGames, "\n")), Inline: false})
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "\u200b", Value: "▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬", Inline: false})

	// SECTION 3: TODAYES NET
	var todayNet []string
	sort.Slice(ledgerToday, func(i, j int) bool { return ledgerToday[i].Net > ledgerToday[j].Net })
	for _, it := range ledgerToday {
		sign := "📈"
		if it.Net < 0 {
			sign = "📉"
		}
		todayNet = append(todayNet, fmt.Sprintf("%s %s: %d", sign, it.Name, it.Net))
	}
	if len(todayNet) == 0 {
		todayNet = append(todayNet, "No items today.")
	}
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "💰 Todayes Net", Value: truncateField(strings.Join(todayNet, "\n")), Inline: false})
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "\u200b", Value: "▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬", Inline: false})

	// SECTION 4: TOP PLAYERS TODAY
	whaleInfo := "Winner: None\nLoser: None"
	if bigWinner != nil || bigLoser != nil {
		whaleInfo = ""
		if bigWinner != nil && bigWinner.Total > 0 {
			whaleInfo += fmt.Sprintf("🏆 **Winner:** `%s` (%+d items)\n", bigWinner.Name, bigWinner.Total)
		}
		if bigLoser != nil && bigLoser.Total < 0 {
			whaleInfo += fmt.Sprintf("💀 **Loser:** `%s` (%d items)", bigLoser.Name, bigLoser.Total)
		}
	}
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "🐋 Top Players Today", Value: truncateField(whaleInfo), Inline: false})
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "\u200b", Value: "▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬", Inline: false})

	// SECTION 5: HEAT MAP
	heatInfo := ""
	if bestDealerStreak != nil && bestDealerStreak.Count > 1 {
		heatInfo += fmt.Sprintf("🔥 **Dealer Streak:** %d wins on %s\n", bestDealerStreak.Count, bestDealerStreak.Game)
	} else {
		heatInfo += "🔥 **Dealer Streak:** None\n"
	}
	if bestPlayerStreak != nil && bestPlayerStreak.Count > 1 {
		heatInfo += fmt.Sprintf("🧊 **Player Streak:** %d wins on %s", bestPlayerStreak.Count, bestPlayerStreak.Game)
	} else {
		heatInfo += "🧊 **Player Streak:** None"
	}
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "📈 Heat Map", Value: truncateField(heatInfo), Inline: false})
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "\u200b", Value: "▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬", Inline: false})

	// SECTION 5.5: WEEKLY STATS
	dealerLossWeek := statsWeek.Overall.TotalRounds - statsWeek.Overall.DealerWins
	weekStats := fmt.Sprintf("Total Rounds: %d\nDealer Wins: %d\nDealer Loss: %d\nHouse Edge: %+.1f%%",
		statsWeek.Overall.TotalRounds,
		statsWeek.Overall.DealerWins,
		dealerLossWeek,
		statsWeek.Overall.DealerWinRate-statsWeek.Overall.PlayerWinRate)
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "📅 Weekly Stats", Value: truncateField(weekStats), Inline: false})

	var weekNet []string
	sort.Slice(ledgerWeek, func(i, j int) bool { return ledgerWeek[i].Net > ledgerWeek[j].Net })
	for i, it := range ledgerWeek {
		if i >= 10 {
			break
		}
		sign := "📈"
		if it.Net < 0 {
			sign = "📉"
		}
		weekNet = append(weekNet, fmt.Sprintf("%s %s: %d", sign, it.Name, it.Net))
	}
	if len(weekNet) == 0 {
		weekNet = append(weekNet, "No items this week.")
	}
	if len(ledgerWeek) > 10 {
		weekNet = append(weekNet, fmt.Sprintf("... and %d more types", len(ledgerWeek)-10))
	}
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "💰 Weekly Net", Value: truncateField(strings.Join(weekNet, "\n")), Inline: false})
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "\u200b", Value: "▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬", Inline: false})

	// SECTION 6: LIFETIME SUMMARY
	lifeItems := []string{}
	sort.Slice(ledgerLife, func(i, j int) bool { return ledgerLife[i].Net > ledgerLife[j].Net })
	for _, it := range ledgerLife {
		lifeItems = append(lifeItems, fmt.Sprintf("🔸 %s: %d", it.Name, it.Net))
	}
	lifeSummary := fmt.Sprintf("Daily Avg: %.1f items in/day\nTotal Rounds: %d\nLife Net: **%d** items\n\n%s",
		dailyAvgIn, statsLife.Overall.TotalRounds, lifeNetItems, strings.Join(lifeItems, "\n"))
	embed.Fields = append(embed.Fields, DiscordEmbedField{Name: "👑 Lifetime Summary", Value: truncateField(lifeSummary), Inline: false})

	payload := DiscordWebhookPayload{
		Username:  "Casino Performance Bot",
		AvatarURL: "https://i.imgur.com/W7S6S8k.png",
		Embeds:    []DiscordEmbed{embed},
	}

	payloadBytes, _ := json.Marshal(payload)
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Sprintf("Error posting to Discord: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Sprintf("Discord Error: %s - %s", resp.Status, string(body))
	}

	return "Vertical Performance Report posted to Discord!"
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
