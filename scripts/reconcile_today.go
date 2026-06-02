package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := "postgresql://neondb_owner:npg_S9jFTYzdQx3l@ep-aged-king-a77p1t8b-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx := context.Background()
	pool, _ := pgxpool.New(ctx, dbURL)
	defer pool.Close()

	now := time.Now()
	todayStart := now.Format("2006-01-02") + "T00:00:00"
	todayEnd := now.Format("2006-01-02") + "T23:59:59"

	fmt.Println("--- GAME HISTORY FOR TODAY ---")
	grows, _ := pool.Query(ctx, `
		SELECT player_name, game, winner, status, notes, completed_at 
		FROM game_history_entries 
		WHERE completed_at >= $1 AND completed_at <= $2
		ORDER BY completed_at ASC
	`, todayStart, todayEnd)
	
	for grows.Next() {
		var p, g, w, s, notes, cat string
		grows.Scan(&p, &g, &w, &s, &notes, &cat)
		fmt.Printf("[%s] %s | %s | Winner: %s | Status: %s | Notes: %s\n", cat, p, g, w, s, notes)
	}
	grows.Close()

	fmt.Println("\n--- TRADE LEDGER FOR TODAY ---")
	trows, _ := pool.Query(ctx, `
		SELECT partner_name, trade_type, items, created_at 
		FROM trade_ledger 
		WHERE created_at >= $1 AND created_at <= $2
		ORDER BY created_at ASC
	`, todayStart, todayEnd)
	
	for trows.Next() {
		var p, tt, itemsJSON string
		var cat time.Time
		trows.Scan(&p, &tt, &itemsJSON, &cat)
		fmt.Printf("[%s] %s | %s | Items: %s\n", cat.Format(time.RFC3339), p, tt, itemsJSON)
	}
	trows.Close()
}
