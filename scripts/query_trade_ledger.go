package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func printRows(ctx context.Context, pool *pgxpool.Pool, q string, args ...interface{}) error {
	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var owner, partner, tradeType string
		var totalQty int
		var items []byte
		var created time.Time
		if err := rows.Scan(&id, &owner, &partner, &tradeType, &totalQty, &items, &created); err != nil {
			fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
			continue
		}
		// pretty items
		var pretty json.RawMessage
		_ = json.Unmarshal(items, &pretty)
		itemsStr := string(items)
		fmt.Printf("%d | owner=%s | partner=%s | type=%s | qty=%d | items=%s | created=%s\n", id, owner, partner, tradeType, totalQty, itemsStr, created.Format(time.RFC3339))
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run scripts/query_trade_ledger.go <db-url> [partner]")
		os.Exit(2)
	}
	dbURL := os.Args[1]
	partner := ""
	if len(os.Args) >= 3 {
		partner = os.Args[2]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect error: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	fmt.Println("--- Recent trade_ledger rows for partner (if provided) ---")
	if partner != "" {
		pattern := "%" + partner + "%"
		q := `SELECT id, owner_key, partner_name, trade_type, total_quantity, items, created_at FROM public.trade_ledger WHERE partner_name ILIKE $1 ORDER BY created_at DESC LIMIT 50`
		if err := printRows(ctx, pool, q, pattern); err != nil {
			fmt.Fprintf(os.Stderr, "query error: %v\n", err)
		}
	} else {
		fmt.Println("(no partner provided)")
	}

	fmt.Println("\n--- Last 20 trade_ledger rows overall ---")
	q2 := `SELECT id, owner_key, partner_name, trade_type, total_quantity, items, created_at FROM public.trade_ledger ORDER BY created_at DESC LIMIT 20`
	if err := printRows(ctx, pool, q2); err != nil {
		fmt.Fprintf(os.Stderr, "query error: %v\n", err)
	}
}
