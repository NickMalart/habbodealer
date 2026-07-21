package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	conn := "postgresql://neondb_owner:npg_bV04zdgaxDHm@ep-autumn-math-a7fklxxr-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	
	pool, err := pgxpool.New(ctx, conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect error: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	fmt.Println("Querying pending shouts from public.dealer_shouts...")
	rows, err := pool.Query(ctx, "SELECT id, target_player, message, owner_key, status, created_at FROM public.dealer_shouts WHERE status = 'pending' ORDER BY created_at DESC LIMIT 20")
	if err != nil {
		fmt.Fprintf(os.Stderr, "query error: %v\n", err)
		os.Exit(2)
	}
	defer rows.Close()

	fmt.Printf("%-5s | %-15s | %-15s | %-10s | %-20s | %s\n", "ID", "Player", "Owner", "Status", "Created", "Message")
	fmt.Println("---------------------------------------------------------------------------------------------------------")
	for rows.Next() {
		var id int
		var targetPlayer, message, ownerKey, status string
		var createdAt time.Time
		if err := rows.Scan(&id, &targetPlayer, &message, &ownerKey, &status, &createdAt); err != nil {
			fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
			continue
		}
		fmt.Printf("%-5d | %-15s | %-15s | %-10s | %-20s | %s\n", id, targetPlayer, ownerKey, status, createdAt.Format("2006-01-02 15:04:05"), message)
	}
}
