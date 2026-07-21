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

	fmt.Println("--- Migrating banker_trades table ---")
	
	// Add risk_bank column
	_, err = conn.Exec(ctx, `
		ALTER TABLE banker_trades 
		ADD COLUMN IF NOT EXISTS risk_bank INTEGER DEFAULT 0,
		ADD COLUMN IF NOT EXISTS risk_status TEXT DEFAULT 'idle'
	`)
	if err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	
	fmt.Println("Table banker_trades migrated successfully.")
}
