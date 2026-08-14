//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DBConfig struct {
	DatabaseURL string `json:"databaseUrl"`
	OwnerKey    string `json:"ownerKey"`
}

func main() {
	data, err := os.ReadFile("db.local.json")
	if err != nil {
		log.Fatalf("could not read db.local.json: %v", err)
	}
	var cfg DBConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("could not parse db.local.json: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("pool create failed: %v", err)
	}
	defer db.Close()

	fmt.Println("=== blocked_players table ===")
	rows, err := db.Query(ctx, `SELECT player_name, created_at FROM blocked_players ORDER BY created_at DESC`)
	if err != nil {
		fmt.Printf("Query failed (maybe table doesn't exist?): %v\n", err)
	} else {
		defer rows.Close()
		count := 0
		for rows.Next() {
			var name string
			var createdAt time.Time
			if err := rows.Scan(&name, &createdAt); err == nil {
				fmt.Printf("  name=%q created_at=%s\n", name, createdAt.Format(time.RFC3339))
				count++
			}
		}
		fmt.Printf("Total blocked: %d\n", count)
	}

	// Show recent entries for the current owner
	fmt.Printf("\n=== last 10 entries for owner %q ===\n", cfg.OwnerKey)
	rows3, err := db.Query(ctx, `SELECT id, player_name, game, status, updated_db_at FROM game_history_entries WHERE ($1 = '' OR owner_key = $1) ORDER BY updated_db_at DESC LIMIT 10`, cfg.OwnerKey)
	if err != nil {
		log.Fatalf("query failed: %v", err)
	}
	found := false
	for rows3.Next() {
		var id, player, game, status string
		var updatedAt time.Time
		_ = rows3.Scan(&id, &player, &game, &status, &updatedAt)
		fmt.Printf("  id=%s player=%q game=%s status=%s at=%s\n", id, player, game, status, updatedAt.Format(time.RFC3339))
		found = true
	}
	rows3.Close()
	if !found {
		fmt.Println("  (no rows)")
	}
}
