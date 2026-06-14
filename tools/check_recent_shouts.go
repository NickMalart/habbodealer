package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	connStr := "postgresql://neondb_owner:npg_S9jFTYzdQx3l@ep-aged-king-a77p1t8b-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer conn.Close(ctx)

	fmt.Println("--- Recent dealer_shouts for DESKTOP-B9TGIUH ---")
	rows, err := conn.Query(ctx, `SELECT id, target_player, message, status, created_at FROM dealer_shouts WHERE owner_key = 'DESKTOP-B9TGIUH' ORDER BY id DESC LIMIT 20`)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var targetPlayer, message, status string
		var createdAt time.Time
		if err := rows.Scan(&id, &targetPlayer, &message, &status, &createdAt); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("[%d] %s -> %s (%s) [%s]\n", id, targetPlayer, message, status, createdAt.Format("15:04:05"))
	}
}
