package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	connStr := "postgresql://neondb_owner:npg_l8r4nExKaNGP@ep-rapid-night-a7u01fue-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("pool create failed: %v", err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow(ctx, "SELECT count(*) FROM public.auto_payouts").Scan(&count)
	if err != nil {
		fmt.Printf("Query public.auto_payouts failed: %v\n", err)
	} else {
		fmt.Printf("Total rows in public.auto_payouts: %d\n", count)
	}

	rows, err := db.Query(ctx, "SELECT id, player_name, item_name, quantity, status FROM public.auto_payouts")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, name, item, status string
			var qty int
			rows.Scan(&id, &name, &item, &qty, &status)
			fmt.Printf("Row: id=%s name=%s item=%s qty=%d status=%s\n", id, name, item, qty, status)
		}
	}
}
