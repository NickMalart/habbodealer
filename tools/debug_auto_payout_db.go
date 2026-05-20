package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	connStr := "postgresql://neondb_owner:npg_Jx8ERGzK6eog@ep-small-thunder-a7ceewoj-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("pool create failed: %v", err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow(ctx, "SELECT count(*) FROM auto_payouts").Scan(&count)
	if err != nil {
		fmt.Printf("Query auto_payouts failed: %v\n", err)
	} else {
		fmt.Printf("Total rows in auto_payouts: %d\n", count)
	}

	fmt.Println("\n--- auto_payouts ---")
	rows, err := db.Query(ctx, "SELECT id, player_name, item_name, quantity, status FROM auto_payouts")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, name, item, status string
			var qty int
			rows.Scan(&id, &name, &item, &qty, &status)
			fmt.Printf("Row: id=%s name=%s item=%s qty=%d status=%s\n", id, name, item, qty, status)
		}
	}

	fmt.Println("\n--- banker_trades (recent) ---")
	rowsBT, err := db.Query(ctx, "SELECT id, player_name, status, banker_name, created_at FROM banker_trades ORDER BY created_at DESC LIMIT 10")
	if err == nil {
		defer rowsBT.Close()
		for rowsBT.Next() {
			var id int
			var name, status, bankerName string
			var createdAt time.Time
			rowsBT.Scan(&id, &name, &status, &bankerName, &createdAt)
			fmt.Printf("BT: id=%d name=%s status=%s banker=%s at=%v\n", id, name, status, bankerName, createdAt)
		}
	}
}
