package main

import (
	"context"
	"fmt"
	"time"

	"database/sql"
	_ "modernc.org/sqlite"
)

func main() {
	dbURL := "./database.sqlite"
	ctx := context.Background()
	pool, err := sql.Open("sqlite", dbURL)
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		return
	}
	defer pool.Close()

	loc := time.FixedZone("GMT+10", 10*60*60)
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Format(time.RFC3339)
	todayEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, loc).Format(time.RFC3339)

	fmt.Println("--- GAME HISTORY FOR TODAY ---")
	grows, _ := pool.QueryContext(ctx, `
		SELECT player_name, game, winner, status, notes, completed_at 
		FROM game_history_entries 
		WHERE completed_at >= ? AND completed_at <= ?
		ORDER BY completed_at ASC
	`, todayStart, todayEnd)

	for grows.Next() {
		var p, g, w, s, notes, cat string
		grows.Scan(&p, &g, &w, &s, &notes, &cat)
		fmt.Printf("[%s] %s | %s | Winner: %s | Status: %s | Notes: %s\n", cat, p, g, w, s, notes)
	}
	grows.Close()

	fmt.Println("\n--- TRADE LEDGER FOR TODAY ---")
	trows, _ := pool.QueryContext(ctx, `
		SELECT partner_name, trade_type, items, created_at 
		FROM trade_ledger 
		WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at ASC
	`, todayStart, todayEnd)

	for trows.Next() {
		var p, tt, itemsJSON, cat string
		trows.Scan(&p, &tt, &itemsJSON, &cat)
		fmt.Printf("[%s] %s | %s | Items: %s\n", cat, p, tt, itemsJSON)
	}
	trows.Close()
}
