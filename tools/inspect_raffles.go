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

	fmt.Println("=== Recent Raffle Sessions ===")
	rows, err := db.Query(ctx, `SELECT id, raffle_name, prize_name, prize_qty, winner_name FROM raffle_sessions ORDER BY id DESC`)
	if err != nil {
		log.Fatalf("query failed: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, prize, winner string
		var qty int
		_ = rows.Scan(&id, &name, &prize, &qty, &winner)
		fmt.Printf("  ID: %d | Name: %q | Prize: %q | Qty: %d | Winner: %q\n", id, name, prize, qty, winner)
	}
}
