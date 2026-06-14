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

	fmt.Println("--- Schema for auto_payouts ---")
	rows, _ := conn.Query(ctx, "SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'auto_payouts'")
	for rows.Next() {
		var col, typ string
		rows.Scan(&col, &typ)
		fmt.Printf("Column: %s, Type: %s\n", col, typ)
	}
	rows.Close()
}
