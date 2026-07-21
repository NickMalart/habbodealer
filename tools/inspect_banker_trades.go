package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	connStr := "postgresql://neondb_owner:npg_bV04zdgaxDHm@ep-autumn-math-a7fklxxr-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer conn.Close(ctx)

	fmt.Println("--- Schema for banker_trades ---")
	rows, err := conn.Query(ctx, `
		SELECT column_name, data_type 
		FROM information_schema.columns 
		WHERE table_name = 'banker_trades'
	`)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var colName, dataType string
		if err := rows.Scan(&colName, &dataType); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Column: %s, Type: %s\n", colName, dataType)
	}

	fmt.Println("\n--- Recent banker_trades entries ---")
	rows2, err := conn.Query(ctx, `SELECT * FROM banker_trades ORDER BY id DESC LIMIT 5`)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	defer rows2.Close()

	cols := rows2.FieldDescriptions()
	for rows2.Next() {
		vals, err := rows2.Values()
		if err != nil {
			log.Fatal(err)
		}
		for i, v := range vals {
			fmt.Printf("%s: %v | ", cols[i].Name, v)
		}
		fmt.Println()
	}
}
