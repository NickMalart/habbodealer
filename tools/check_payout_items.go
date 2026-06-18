package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	connStr := "postgresql://neondb_owner:npg_l8r4nExKaNGP@ep-rapid-night-a7u01fue-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("pool create failed: %v", err)
	}
	defer db.Close()

	fmt.Println("=== Checking game_history_items for Payout Pending entries ===")
	query := `
		SELECT e.player_name, i.item_name, i.quantity, e.id
		FROM game_history_entries e
		JOIN game_history_items i ON e.id = i.entry_id AND e.owner_key = i.owner_key
		WHERE e.status = 'Payout Pending' AND i.item_type = 'payout'
	`
	rows, err := db.Query(ctx, query)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name, item, id string
		var qty int
		rows.Scan(&name, &item, &qty, &id)
		fmt.Printf("Payout: player=%s item=%s qty=%d (EntryID=%s)\n", name, item, qty, id)
	}
}
