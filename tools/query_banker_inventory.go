package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	connStr := "postgresql://neondb_owner:npg_S9jFTYzdQx3l@ep-aged-king-a77p1t8b-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect: %v\n", err)
		os.Exit(2)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `SELECT banker_name, item_name, quantity, updated_at FROM public.banker_inventory ORDER BY banker_name, item_name`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query failed: %v\n", err)
		os.Exit(2)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var bankerName, itemName string
		var qty int
		var updated time.Time
		if err := rows.Scan(&bankerName, &itemName, &qty, &updated); err != nil {
			fmt.Fprintf(os.Stderr, "scan failed: %v\n", err)
			continue
		}
		fmt.Printf("%s | %s | %d | %s\n", bankerName, itemName, qty, updated.Format(time.RFC3339))
		found = true
	}
	if !found {
		fmt.Println("(no rows)")
	}
}
