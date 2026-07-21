package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	connStr := "postgresql://neondb_owner:npg_bV04zdgaxDHm@ep-autumn-math-a7fklxxr-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("pool create failed: %v", err)
	}
	defer db.Close()

	fmt.Println("=== Checking game_history_entries for potential payouts ===")
	rows, err := db.Query(ctx, "SELECT player_name, status, issue_reason FROM game_history_entries WHERE status ILIKE '%payout%' OR issue_reason ILIKE '%payout%' LIMIT 20")
	if err != nil {
		fmt.Printf("Query failed: %v\n", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var name, status, reason string
			rows.Scan(&name, &status, &reason)
			fmt.Printf("Row: player=%s status=%s reason=%s\n", name, status, reason)
		}
	}
}
