package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	add := flag.Bool("add", false, "add missing column if absent")
	flag.Parse()

	conn := os.Getenv("DBURL")
	if conn == "" {
		conn = flag.Arg(0)
	}
	if conn == "" {
		fmt.Fprintln(os.Stderr, "DB URL required via DBURL env or first arg")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect error: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	fmt.Println("Connected; inspecting columns for public.auto_payouts")
	rows, err := pool.Query(ctx, "SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema='public' AND table_name='auto_payouts' ORDER BY ordinal_position")
	if err != nil {
		fmt.Fprintf(os.Stderr, "query error: %v\n", err)
		os.Exit(2)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var name, dtype, nullable string
		if err := rows.Scan(&name, &dtype, &nullable); err != nil {
			fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
			os.Exit(3)
		}
		fmt.Printf(" - %s %s %s\n", name, dtype, nullable)
		if name == "player_trade_id" {
			found = true
		}
	}

	if !found {
		fmt.Println("player_trade_id not found")
		if *add {
			fmt.Println("adding player_trade_id column...")
			if _, err := pool.Exec(ctx, "ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS player_trade_id INTEGER NULL;"); err != nil {
				fmt.Fprintf(os.Stderr, "alter error: %v\n", err)
				os.Exit(4)
			}
			fmt.Println("ALTER TABLE executed.")
			rows2, err := pool.Query(ctx, "SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema='public' AND table_name='auto_payouts' ORDER BY ordinal_position")
			if err != nil {
				fmt.Fprintf(os.Stderr, "query2 err: %v\n", err)
				os.Exit(5)
			}
			defer rows2.Close()
			for rows2.Next() {
				var name, dtype, nullable string
				rows2.Scan(&name, &dtype, &nullable)
				fmt.Printf(" - %s %s %s\n", name, dtype, nullable)
			}
		} else {
			fmt.Println("Run with -add to add the column")
		}
	} else {
		fmt.Println("player_trade_id exists.")
	}

	fmt.Println("\nSample rows (most recent 10):")
	sampleRows, err := pool.Query(ctx, "SELECT id, player_name, item_name, quantity, status, created_at, player_trade_id FROM public.auto_payouts ORDER BY created_at DESC LIMIT 10")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sample query err: %v\n", err)
		os.Exit(6)
	}
	defer sampleRows.Close()
	for sampleRows.Next() {
		var id, playerName, itemName, status string
		var quantity int
		var created_at time.Time
		var player_trade_id sql.NullInt64
		if err := sampleRows.Scan(&id, &playerName, &itemName, &quantity, &status, &created_at, &player_trade_id); err != nil {
			fmt.Fprintf(os.Stderr, "sample scan err: %v\n", err)
			os.Exit(7)
		}
		pt := "NULL"
		if player_trade_id.Valid {
			pt = fmt.Sprintf("%d", player_trade_id.Int64)
		}
		fmt.Printf("%s | %s | %s | %d | %s | %s | %s\n", id, playerName, itemName, quantity, status, created_at.Format(time.RFC3339), pt)
	}
}
