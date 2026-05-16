package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	data, err := os.ReadFile("db.local.json")
	if err != nil {
		log.Fatal(err)
	}

	var cfg struct {
		DatabaseURL string `json:"databaseUrl"`
		OwnerKey    string `json:"ownerKey"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatal(err)
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	fmt.Printf("Checking tables for owner: %s\n\n", cfg.OwnerKey)

	// Check game_history_items
	var gameItemCount int
	err = pool.QueryRow(context.Background(), "SELECT count(*) FROM game_history_items WHERE owner_key = $1", cfg.OwnerKey).Scan(&gameItemCount)
	if err != nil {
		fmt.Printf("Error checking game_history_items: %v\n", err)
	} else {
		fmt.Printf("Total items in game_history_items: %d\n", gameItemCount)
		if gameItemCount > 0 {
			rows, _ := pool.Query(context.Background(), "SELECT item_name, item_type, quantity FROM game_history_items WHERE owner_key = $1 LIMIT 5", cfg.OwnerKey)
			fmt.Println("Sample items from game_history_items:")
			for rows.Next() {
				var name, itype string
				var qty int
				rows.Scan(&name, &itype, &qty)
				fmt.Printf(" - %s (%s) x%d\n", name, itype, qty)
			}
			rows.Close()
		}
	}

	fmt.Println()

	// Check trade_entry_items
	var tradeItemCount int
	err = pool.QueryRow(context.Background(), "SELECT count(*) FROM trade_entry_items WHERE owner_key = $1", cfg.OwnerKey).Scan(&tradeItemCount)
	if err != nil {
		fmt.Printf("Error checking trade_entry_items: %v\n", err)
	} else {
		fmt.Printf("Total items in trade_entry_items: %d\n", tradeItemCount)
		if tradeItemCount > 0 {
			rows, _ := pool.Query(context.Background(), "SELECT item_name, quantity FROM trade_entry_items WHERE owner_key = $1 LIMIT 5", cfg.OwnerKey)
			fmt.Println("Sample items from trade_entry_items:")
			for rows.Next() {
				var name string
				var qty int
				rows.Scan(&name, &qty)
				fmt.Printf(" - %s x%d\n", name, qty)
			}
			rows.Close()
		}
	}
}
