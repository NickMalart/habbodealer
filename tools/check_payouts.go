package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	connStr := "postgresql://neondb_owner:npg_l8r4nExKaNGP@ep-rapid-night-a7u01fue-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer conn.Close(ctx)

	fmt.Println("--- Recent auto_payouts ---")
	rows, err := conn.Query(ctx, `SELECT id, player_name, item_name, quantity, status, created_at FROM auto_payouts ORDER BY created_at DESC LIMIT 10`)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, playerName, itemName, status, createdAt string
		var quantity int
		if err := rows.Scan(&id, &playerName, &itemName, &quantity, &status, &createdAt); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("[%s] %s: %s x%d (%s) [%s]\n", id, playerName, itemName, quantity, status, createdAt)
	}
}
