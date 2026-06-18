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

	fmt.Println("--- Creating banker_inventory table ---")
	_, err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS banker_inventory (
			banker_name TEXT,
			item_name TEXT,
			quantity INTEGER,
			updated_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			PRIMARY KEY (banker_name, item_name)
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}
	fmt.Println("Table banker_inventory ready.")
}
