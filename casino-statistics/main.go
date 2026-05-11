package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

type GameHistoryEntry struct {
	ID             int64          `json:"id"`
	Game           string         `json:"game"`
	PlayerName     string         `json:"playerName"`
	Winner         string         `json:"winner"`
	Status         string         `json:"status"`
	StartedAt      string         `json:"startedAt"`
	CompletedAt    string         `json:"completedAt"`
	UpdatedAt      string         `json:"updatedAt"`
	Issue          bool           `json:"issue"`
}

type CasinoStats struct {
	TotalRounds     int                 `json:"totalRounds"`
	PlayerWins      int                 `json:"playerWins"`
	DealerWins      int                 `json:"dealerWins"`
	PlayerWinRate   float64             `json:"playerWinRate"`
	DealerWinRate   float64             `json:"dealerWinRate"`
	ByGame          map[string]GameStat `json:"byGame"`
	Items           []ItemStat          `json:"items"`
	Players         []PlayerStat        `json:"players"`
}

type GameStat struct {
	Game          string  `json:"game"`
	TotalRounds   int     `json:"totalRounds"`
	PlayerWins    int     `json:"playerWins"`
	DealerWins    int     `json:"dealerWins"`
	PlayerWinRate float64 `json:"playerWinRate"`
	DealerWinRate float64 `json:"dealerWinRate"`
}

type ItemStat struct {
	Name    string `json:"name"`
	WonQty  int    `json:"wonQty"`
	LostQty int    `json:"lostQty"`
	NetQty  int    `json:"netQty"`
	Games   int    `json:"games"`
}

type PlayerStat struct {
	PlayerName     string  `json:"playerName"`
	TotalRounds    int     `json:"totalRounds"`
	PlayerWins     int     `json:"playerWins"`
	DealerWins     int     `json:"dealerWins"`
	WinRate        float64 `json:"winRate"`
	NetItems       int     `json:"netItems"` // Dealer Profit (Bets - Payouts)
	BetItemsIn     int     `json:"betItemsIn"`
	PayoutItemsOut int     `json:"payoutItemsOut"`
}

type PlayerDetails struct {
	PlayerName     string           `json:"playerName"`
	TotalRounds    int              `json:"totalRounds"`
	PlayerWins     int              `json:"playerWins"`
	DealerWins     int              `json:"dealerWins"`
	WinRate        float64          `json:"winRate"`
	LossRate       float64          `json:"lossRate"`
	BetItemsIn     int              `json:"betItemsIn"`
	PayoutItemsOut int              `json:"payoutItemsOut"`
	NetProfit      int              `json:"netProfit"` // Payouts - Bets
	ByGame         []PlayerGameStat `json:"byGame"`
	ByItem         []PlayerItemStat `json:"byItem"`
}

type PlayerGameStat struct {
	Game        string  `json:"game"`
	TotalRounds int     `json:"totalRounds"`
	Wins        int     `json:"wins"`
	Losses      int     `json:"losses"`
	WinRate     float64 `json:"winRate"`
}

type PlayerItemStat struct {
	ItemName  string `json:"itemName"`
	BetIn     int    `json:"betIn"`
	PayoutOut int    `json:"payoutOut"`
	Net       int    `json:"net"`
}

type App struct {
	ctx      context.Context
	db       *pgxpool.Pool
	ownerKey string
	mu       sync.Mutex
}

func NewApp() *App {
	return &App{
		ownerKey: "roll-origins", // Default, can be overridden by config
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.initDatabase()
}

func (a *App) initDatabase() {
	dbURL := "postgresql://neondb_owner:npg_Jx8ERGzK6eog@ep-small-thunder-a7ceewoj-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ownerKey := "roll-origins"

	// Try to load from db.local.json if it exists (one level up)
	if data, err := os.ReadFile("../db.local.json"); err == nil {
		var config struct {
			DatabaseURL string `json:"databaseUrl"`
			OwnerKey    string `json:"ownerKey"`
		}
		if err := json.Unmarshal(data, &config); err == nil {
			if config.DatabaseURL != "" {
				dbURL = config.DatabaseURL
			}
			if config.OwnerKey != "" {
				ownerKey = config.OwnerKey
			}
		}
	}

	a.ownerKey = ownerKey
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Printf("Unable to parse DATABASE_URL: %v", err)
		return
	}

	// Use simple protocol to avoid "prepared statement name is already in use" errors 
	// which commonly happen with PostgreSQL proxies like Neon or PgBouncer.
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		log.Printf("Unable to connect to database: %v", err)
		return
	}
	a.db = pool

	// Create blocked_players table if it doesn't exist
	_, err = a.db.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS blocked_players (
			id SERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL,
			player_name TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			UNIQUE(owner_key, player_name)
		)
	`)
	if err != nil {
		log.Printf("Failed to ensure blocked_players table: %v", err)
	}
}

func (a *App) shutdown(ctx context.Context) {
	if a.db != nil {
		a.db.Close()
	}
}

func (a *App) GetStats() (CasinoStats, error) {
	if a.db == nil {
		return CasinoStats{}, fmt.Errorf("database not connected")
	}

	// Fetch blocklist first for exclusion in memory for the main loop
	blocklist, _ := a.GetBlockedPlayers()
	blockedMap := make(map[string]bool)
	for _, p := range blocklist {
		blockedMap[strings.ToLower(strings.TrimSpace(p))] = true
	}

	rows, err := a.db.Query(a.ctx, 
		`SELECT game, winner, status, issue, choice, player_name FROM game_history_entries WHERE owner_key = $1`, 
		a.ownerKey)
	if err != nil {
		return CasinoStats{}, err
	}
	defer rows.Close()

	stats := CasinoStats{
		ByGame: make(map[string]GameStat),
	}

	for rows.Next() {
		var game, winner, status, choice, playerName string
		var issue bool
		if err := rows.Scan(&game, &winner, &status, &issue, &choice, &playerName); err != nil {
			continue
		}

		// Skip blocked players
		if blockedMap[strings.ToLower(strings.TrimSpace(playerName))] {
			continue
		}

		// Basic filtering similar to casino_stats.go
		if issue || strings.ToLower(status) != "completed" {
			continue
		}

		gameKey := normalizeGameName(game)
		if gameKey == "" {
			if strings.EqualFold(game, "Waiting For Choice") {
				continue
			}
			gameKey = "Other (" + game + ")"
		}

		// Split UO7 into U7 and O7
		if gameKey == "UO7" {
			choice = strings.ToLower(strings.TrimSpace(choice))
			if choice == "7" {
				// User requested to remove "7"
				continue
			} else if strings.Contains(choice, "under") || choice == "u" || choice == "2-6" || choice == "low" {
				gameKey = "U7"
			} else if strings.Contains(choice, "over") || choice == "o" || choice == "8-12" || choice == "high" {
				gameKey = "O7"
			}
		}

		if gameKey == "Bandit" {
			// User requested to remove "Bandit"
			continue
		}

		gs := stats.ByGame[gameKey]
		gs.Game = gameKey
		gs.TotalRounds++
		stats.TotalRounds++

		winner = strings.ToLower(strings.TrimSpace(winner))
		if winner == "dealer" {
			gs.DealerWins++
			stats.DealerWins++
		} else if winner != "" {
			// Assume anything else is a player win if it's not empty/dealer
			gs.PlayerWins++
			stats.PlayerWins++
		}

		stats.ByGame[gameKey] = gs
	}

	// Calculate rates
	if stats.TotalRounds > 0 {
		stats.PlayerWinRate = float64(stats.PlayerWins) / float64(stats.TotalRounds) * 100
		stats.DealerWinRate = float64(stats.DealerWins) / float64(stats.TotalRounds) * 100
	}

	for k, gs := range stats.ByGame {
		if gs.TotalRounds > 0 {
			gs.PlayerWinRate = float64(gs.PlayerWins) / float64(gs.TotalRounds) * 100
			gs.DealerWinRate = float64(gs.DealerWins) / float64(gs.TotalRounds) * 100
		}
		stats.ByGame[k] = gs
	}

	// Fetch item stats (excluding blocked players)
	itemRows, err := a.db.Query(a.ctx, `
		SELECT
			i.item_name,
			SUM(CASE WHEN i.item_type = 'bet'    THEN i.quantity ELSE 0 END) AS won_qty,
			SUM(CASE WHEN i.item_type = 'payout' THEN i.quantity ELSE 0 END) AS lost_qty,
			COUNT(DISTINCT e.id) AS games
		FROM game_history_entries e
		JOIN game_history_items i ON i.entry_id = e.id AND i.owner_key = e.owner_key
		WHERE e.owner_key = $1 AND e.status = 'Completed' AND e.issue = false
		  AND LOWER(TRIM(e.player_name)) NOT IN (SELECT LOWER(TRIM(player_name)) FROM blocked_players WHERE owner_key = $1)
		GROUP BY i.item_name
		ORDER BY (SUM(CASE WHEN i.item_type = 'bet' THEN i.quantity ELSE 0 END) -
		          SUM(CASE WHEN i.item_type = 'payout' THEN i.quantity ELSE 0 END)) DESC
	`, a.ownerKey)
	if err == nil {
		defer itemRows.Close()
		for itemRows.Next() {
			var i ItemStat
			if err := itemRows.Scan(&i.Name, &i.WonQty, &i.LostQty, &i.Games); err == nil {
				i.NetQty = i.WonQty - i.LostQty
				stats.Items = append(stats.Items, i)
			}
		}
	}

	// Fetch player stats (excluding blocked players)
	playerRows, err := a.db.Query(a.ctx, `
		WITH player_item_totals AS (
			SELECT 
				entry_id,
				SUM(CASE WHEN item_type = 'bet' THEN quantity ELSE 0 END) as bets,
				SUM(CASE WHEN item_type = 'payout' THEN quantity ELSE 0 END) as payouts
			FROM game_history_items
			WHERE owner_key = $1
			GROUP BY entry_id
		)
		SELECT
			e.player_name,
			COUNT(e.id) FILTER (WHERE e.status = 'Completed' AND e.issue = false) AS completed_rounds,
			SUM(CASE WHEN e.status = 'Completed' AND e.issue = false AND LOWER(TRIM(e.winner)) = LOWER(TRIM(e.player_name)) THEN 1 ELSE 0 END) AS player_wins,
			SUM(CASE WHEN e.status = 'Completed' AND e.issue = false AND LOWER(TRIM(e.winner)) = 'dealer' THEN 1 ELSE 0 END) AS dealer_wins,
			COALESCE(SUM(pit.bets), 0) AS total_bet_in,
			COALESCE(SUM(pit.payouts), 0) AS total_payout_out,
			COUNT(e.id) AS total_rounds
		FROM game_history_entries e
		LEFT JOIN player_item_totals pit ON pit.entry_id = e.id
		WHERE e.owner_key = $1
		  AND LOWER(TRIM(e.player_name)) NOT IN (SELECT LOWER(TRIM(player_name)) FROM blocked_players WHERE owner_key = $1)
		GROUP BY e.player_name
		ORDER BY completed_rounds DESC
		LIMIT 100
	`, a.ownerKey)
	if err == nil {
		defer playerRows.Close()
		for playerRows.Next() {
			var p PlayerStat
			var completedRounds int
			if err := playerRows.Scan(&p.PlayerName, &completedRounds, &p.PlayerWins, &p.DealerWins, &p.BetItemsIn, &p.PayoutItemsOut, &p.TotalRounds); err == nil {
				if completedRounds > 0 {
					p.WinRate = float64(p.PlayerWins) / float64(completedRounds) * 100
				}
				p.NetItems = p.BetItemsIn - p.PayoutItemsOut // Dealer Profit
				stats.Players = append(stats.Players, p)
			}
		}
	}

	return stats, nil
}

func (a *App) GetPlayerDetails(playerName string) (PlayerDetails, error) {
	if a.db == nil {
		return PlayerDetails{}, fmt.Errorf("database not connected")
	}

	details := PlayerDetails{
		PlayerName: playerName,
		ByGame:     []PlayerGameStat{},
		ByItem:     []PlayerItemStat{},
	}

	// Basic stats
	err := a.db.QueryRow(a.ctx, `
		WITH player_item_totals AS (
			SELECT 
				entry_id,
				SUM(CASE WHEN item_type = 'bet' THEN quantity ELSE 0 END) as bets,
				SUM(CASE WHEN item_type = 'payout' THEN quantity ELSE 0 END) as payouts
			FROM game_history_items
			WHERE owner_key = $1
			GROUP BY entry_id
		)
		SELECT
			COUNT(e.id) AS total_rounds,
			SUM(CASE WHEN e.status = 'Completed' AND e.issue = false AND LOWER(TRIM(e.winner)) = LOWER(TRIM(e.player_name)) THEN 1 ELSE 0 END) AS player_wins,
			SUM(CASE WHEN e.status = 'Completed' AND e.issue = false AND LOWER(TRIM(e.winner)) = 'dealer' THEN 1 ELSE 0 END) AS dealer_wins,
			COALESCE(SUM(pit.bets), 0) AS total_bet_in,
			COALESCE(SUM(pit.payouts), 0) AS total_payout_out
		FROM game_history_entries e
		LEFT JOIN player_item_totals pit ON pit.entry_id = e.id
		WHERE e.owner_key = $1 AND LOWER(TRIM(e.player_name)) = LOWER(TRIM($2))
	`, a.ownerKey, playerName).Scan(&details.TotalRounds, &details.PlayerWins, &details.DealerWins, &details.BetItemsIn, &details.PayoutItemsOut)

	if err != nil {
		return details, err
	}

	if details.TotalRounds > 0 {
		// Use completed rounds for win rate calculation if possible
		var completedCount int
		a.db.QueryRow(a.ctx, "SELECT COUNT(*) FROM game_history_entries WHERE owner_key = $1 AND LOWER(TRIM(player_name)) = LOWER(TRIM($2)) AND status = 'Completed' AND issue = false", a.ownerKey, playerName).Scan(&completedCount)
		
		if completedCount > 0 {
			details.WinRate = float64(details.PlayerWins) / float64(completedCount) * 100
			details.LossRate = float64(details.DealerWins) / float64(completedCount) * 100
		}
	}
	details.NetProfit = details.PayoutItemsOut - details.BetItemsIn

	// By Game breakdown
	gameRows, err := a.db.Query(a.ctx, `
		SELECT
			game,
			COUNT(id) AS total_rounds,
			SUM(CASE WHEN e.status = 'Completed' AND e.issue = false AND LOWER(TRIM(winner)) = LOWER(TRIM(player_name)) THEN 1 ELSE 0 END) AS wins,
			SUM(CASE WHEN e.status = 'Completed' AND e.issue = false AND LOWER(TRIM(winner)) = 'dealer' THEN 1 ELSE 0 END) AS losses
		FROM game_history_entries e
		WHERE owner_key = $1 AND LOWER(TRIM(player_name)) = LOWER(TRIM($2))
		GROUP BY game
		ORDER BY total_rounds DESC
	`, a.ownerKey, playerName)
	if err == nil {
		defer gameRows.Close()
		for gameRows.Next() {
			var g PlayerGameStat
			var rawGame string
			if err := gameRows.Scan(&rawGame, &g.TotalRounds, &g.Wins, &g.Losses); err == nil {
				g.Game = normalizeGameName(rawGame)
				if g.Game == "" {
					g.Game = "Other (" + rawGame + ")"
				}
				if g.TotalRounds > 0 {
					g.WinRate = float64(g.Wins) / float64(g.TotalRounds) * 100
				}
				details.ByGame = append(details.ByGame, g)
			}
		}
	}

	// By Item breakdown (include all status/issues to match physical reality)
	itemRows, err := a.db.Query(a.ctx, `
		SELECT
			i.item_name,
			SUM(CASE WHEN i.item_type = 'bet' THEN i.quantity ELSE 0 END) AS bet_in,
			SUM(CASE WHEN i.item_type = 'payout' THEN i.quantity ELSE 0 END) AS payout_out
		FROM game_history_items i
		JOIN game_history_entries e ON e.id = i.entry_id AND e.owner_key = i.owner_key
		WHERE e.owner_key = $1 AND LOWER(TRIM(e.player_name)) = LOWER(TRIM($2))
		GROUP BY i.item_name
		ORDER BY SUM(i.quantity) DESC
	`, a.ownerKey, playerName)
	if err == nil {
		defer itemRows.Close()
		for itemRows.Next() {
			var i PlayerItemStat
			if err := itemRows.Scan(&i.ItemName, &i.BetIn, &i.PayoutOut); err == nil {
				i.Net = i.PayoutOut - i.BetIn
				details.ByItem = append(details.ByItem, i)
			}
		}
	}

	return details, nil
}

func (a *App) GetBlockedPlayers() ([]string, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not connected")
	}
	rows, err := a.db.Query(a.ctx, `SELECT player_name FROM blocked_players WHERE owner_key = $1 ORDER BY player_name ASC`, a.ownerKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			players = append(players, p)
		}
	}
	return players, nil
}

func (a *App) BlockPlayer(name string) error {
	if a.db == nil {
		return fmt.Errorf("database not connected")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	_, err := a.db.Exec(a.ctx, `INSERT INTO blocked_players (owner_key, player_name) VALUES ($1, $2) ON CONFLICT DO NOTHING`, a.ownerKey, name)
	return err
}

func (a *App) UnblockPlayer(name string) error {
	if a.db == nil {
		return fmt.Errorf("database not connected")
	}
	_, err := a.db.Exec(a.ctx, `DELETE FROM blocked_players WHERE owner_key = $1 AND LOWER(TRIM(player_name)) = LOWER(TRIM($2))`, a.ownerKey, name)
	return err
}

func normalizeGameName(game string) string {
	g := strings.TrimSpace(strings.ToLower(game))
	// Prefer explicit Double Trouble mapping before broad 'tri' checks
	if strings.Contains(g, "double") || g == "dt" || strings.Contains(g, "doubletrouble") {
		return "DT"
	}
	// Tri variants: treat all tri variants as a single "Tri" bucket for stats
	if strings.Contains(g, "tri") {
		return "Tri"
	}
	switch g {
	case "poker", "pkr":
		return "Poker"
	case "21", "blackjack", "black jack":
		return "21"
	case "13", "thirteen":
		return "13"
	case "pu", "pu3", "pairup", "pair up":
		return "PU"
	case "h18":
		return "H18"
	case "uo7", "uo":
		return "UO7"
	case "bandit", "onearmbandit", "oab":
		return "Bandit"
	default:
		return ""
	}
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Casino Statistics",
		Width:  1200,
		Height: 800,
		MinWidth: 1024,
		MinHeight: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
