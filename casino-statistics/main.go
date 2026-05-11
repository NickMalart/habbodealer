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
}

type GameStat struct {
	Game          string  `json:"game"`
	TotalRounds   int     `json:"totalRounds"`
	PlayerWins    int     `json:"playerWins"`
	DealerWins    int     `json:"dealerWins"`
	PlayerWinRate float64 `json:"playerWinRate"`
	DealerWinRate float64 `json:"dealerWinRate"`
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

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		log.Printf("Unable to connect to database: %v", err)
		return
	}
	a.db = pool
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

	rows, err := a.db.Query(a.ctx, 
		`SELECT game, winner, status, issue, choice FROM game_history_entries WHERE owner_key = $1`, 
		a.ownerKey)
	if err != nil {
		return CasinoStats{}, err
	}
	defer rows.Close()

	stats := CasinoStats{
		ByGame: make(map[string]GameStat),
	}

	for rows.Next() {
		var game, winner, status, choice string
		var issue bool
		if err := rows.Scan(&game, &winner, &status, &issue, &choice); err != nil {
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

	return stats, nil
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
		Width:  800,
		Height: 600,
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
