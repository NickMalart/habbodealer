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

	// Show all distinct owner_keys in the DB
	fmt.Println("=== owner_keys in game_history_entries ===")
	rows, err := db.Query(ctx, `SELECT owner_key, COUNT(*) as cnt FROM game_history_entries GROUP BY owner_key`)
	if err != nil {
		log.Fatalf("query failed: %v", err)
	}
	found := false
	for rows.Next() {
		var ownerKey string
		var cnt int64
		_ = rows.Scan(&ownerKey, &cnt)
		fmt.Printf("  owner_key=%q  entries=%d\n", ownerKey, cnt)
		found = true
	}
	rows.Close()
	if !found {
		fmt.Println("  (no rows - table is empty)")
	}

	// Show all distinct owner_keys in items
	fmt.Println("\n=== owner_keys in game_history_items ===")
	rows2, err := db.Query(ctx, `SELECT owner_key, COUNT(*) as cnt FROM game_history_items GROUP BY owner_key`)
	if err != nil {
		log.Fatalf("query failed: %v", err)
	}
	found = false
	for rows2.Next() {
		var ownerKey string
		var cnt int64
		_ = rows2.Scan(&ownerKey, &cnt)
		fmt.Printf("  owner_key=%q  items=%d\n", ownerKey, cnt)
		found = true
	}
	rows2.Close()
	if !found {
		fmt.Println("  (no rows - table is empty)")
	}

	// Show recent entries regardless of owner
	fmt.Println("\n=== last 5 entries (any owner) ===")
	rows3, err := db.Query(ctx, `SELECT id, owner_key, player_name, game, status, updated_db_at FROM game_history_entries ORDER BY updated_db_at DESC LIMIT 5`)
	if err != nil {
		log.Fatalf("query failed: %v", err)
	}
	found = false
	for rows3.Next() {
		var id, ownerKey, player, game, status string
		var updatedAt time.Time
		_ = rows3.Scan(&id, &ownerKey, &player, &game, &status, &updatedAt)
		fmt.Printf("  id=%s owner=%q player=%q game=%s status=%s at=%s\n", id, ownerKey, player, game, status, updatedAt.Format(time.RFC3339))
		found = true
	}
	rows3.Close()
	if !found {
		fmt.Println("  (no rows)")
	}
}
