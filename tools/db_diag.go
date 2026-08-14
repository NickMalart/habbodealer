package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	conn := "postgresql://neondb_owner:npg_bV04zdgaxDHm@ep-autumn-math-a7fklxxr-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, conn)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	// Inspect schemas for relevant tables and show sample rows.
	introspect := []string{
		`SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'trade_entries' ORDER BY ordinal_position;`,
		`SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'trade_sessions' ORDER BY ordinal_position;`,
		`SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'game_history_entries' ORDER BY ordinal_position;`,
	}

	for _, q := range introspect {
		fmt.Println("--- schema ---")
		rows, err := pool.Query(ctx, q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "introspection failed: %v\n", err)
			continue
		}
		for rows.Next() {
			var name, dtype string
			if err := rows.Scan(&name, &dtype); err != nil {
				fmt.Fprintf(os.Stderr, "scan failed: %v\n", err)
				break
			}
			fmt.Printf("%s\t%s\n", name, dtype)
		}
		rows.Close()
		fmt.Println()
	}

	samples := []struct{
		title string
		q string
	}{
		{"Trades with payload mentioning bvanmker", `SELECT id, session_id, owner_key, occurred_at, payload FROM public.trade_entries WHERE payload::text ILIKE '%bvanmker%' ORDER BY occurred_at DESC LIMIT 200;`},
		{"Game history for bvanmker", `SELECT id, owner_key, player_name, started_at, status FROM public.game_history_entries WHERE LOWER(player_name) = 'bvanmker' ORDER BY started_at DESC LIMIT 200;`},
		{"Recent trade sessions", `SELECT id, owner_key, status, created_at FROM public.trade_sessions ORDER BY created_at DESC LIMIT 50;`},
	}

	for _, s := range samples {
		fmt.Println("---", s.title, "---")
		rows, err := pool.Query(ctx, s.q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sample query failed: %v\n", err)
			continue
		}

		// Quick substring searches in payload and player_name
		checks := []struct{title, q string}{
			{"Count trade_entries with payload ILIKE '%bvan%'", `SELECT COUNT(*) FROM public.trade_entries WHERE payload::text ILIKE '%bvan%';`},
			{"Count trade_entries with payload ILIKE '%van%'", `SELECT COUNT(*) FROM public.trade_entries WHERE payload::text ILIKE '%van%';`},
			{"Count game_history player_name ILIKE '%bvan%'", `SELECT COUNT(*) FROM public.game_history_entries WHERE player_name ILIKE '%bvan%';`},
		}

		for _, c := range checks {
			fmt.Println("---", c.title, "---")
			var cnt int64
			if err := pool.QueryRow(ctx, c.q).Scan(&cnt); err != nil {
				fmt.Fprintf(os.Stderr, "check failed: %v\n", err)
				continue
			}
			fmt.Println(cnt)
			fmt.Println()
		}
		cols := rows.FieldDescriptions()
		for i, f := range cols {
			if i>0 { fmt.Print("\t") }
			fmt.Print(string(f.Name))
		}
		fmt.Println()
		for rows.Next() {
			vals, err := rows.Values()
			if err!=nil { fmt.Fprintf(os.Stderr, "row values error: %v\n", err); break }
			for i, v := range vals {
				if i>0 { fmt.Print("\t") }
				if v==nil { fmt.Print("NULL") } else { fmt.Print(v) }
			}
			fmt.Println()
		}
		rows.Close()
		fmt.Println()
	}

	// Targeted diagnostics for auto-payout flow for player 'bvanmker'
	player := "bvanmker"
	targeted := []struct{title, q string}{
		{"Auto payouts for player", fmt.Sprintf(`SELECT id, player_name, item_name, quantity, status, COALESCE(player_trade_id,0) AS player_trade_id, COALESCE(banker_trade_id,0) AS banker_trade_id FROM public.auto_payouts WHERE lower(player_name)=lower('%s') ORDER BY created_at DESC LIMIT 100;`, player)},
		{"Banker trades for player / trade id", fmt.Sprintf(`SELECT id, player_name, status, player_trade_id, owner_key, created_at FROM public.banker_trades WHERE (player_trade_id > 0 AND player_trade_id IN (SELECT COALESCE(player_trade_id,0) FROM public.auto_payouts WHERE lower(player_name)=lower('%s'))) OR lower(player_name)=lower('%s') ORDER BY created_at DESC LIMIT 100;`, player, player)},
		{"Recent game history for player", fmt.Sprintf(`SELECT id, player_name, game, status, winner, started_at, owner_key FROM public.game_history_entries WHERE lower(player_name)=lower('%s') ORDER BY started_at DESC LIMIT 100;`, player)},
	}

	for _, t := range targeted {
		fmt.Println("---", t.title, "---")
		rows, err := pool.Query(ctx, t.q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "targeted query failed: %v\n", err)
			continue
		}
		cols := rows.FieldDescriptions()
		for i, f := range cols {
			if i>0 { fmt.Print("\t") }
			fmt.Print(string(f.Name))
		}
		fmt.Println()
		for rows.Next() {
			vals, err := rows.Values()
			if err!=nil { fmt.Fprintf(os.Stderr, "row values error: %v\n", err); break }
			for i, v := range vals {
				if i>0 { fmt.Print("\t") }
				if v==nil { fmt.Print("NULL") } else { fmt.Print(v) }
			}
			fmt.Println()
		}
		rows.Close()
		fmt.Println()
	}

	// Distinct owner_key checks per table (if column exists)
	fmt.Println("--- Distinct owner_key in auto_payouts (if column exists) ---")
	rows, err := pool.Query(ctx, "SELECT column_name FROM information_schema.columns WHERE table_name='auto_payouts' AND column_name='owner_key'")
	if err == nil {
		has := false
		for rows.Next() { has = true }
		rows.Close()
		if has {
			r, _ := pool.Query(ctx, "SELECT DISTINCT owner_key FROM public.auto_payouts ORDER BY owner_key")
			for r.Next() {
				var ok string
				_ = r.Scan(&ok)
				fmt.Println(ok)
			}
			r.Close()
		} else {
			fmt.Println("(column not present)")
		}
	}

	fmt.Println("--- Distinct owner_key in banker_trades ---")
	r2, err := pool.Query(ctx, "SELECT DISTINCT owner_key FROM public.banker_trades ORDER BY owner_key")
	if err == nil {
		for r2.Next() {
			var ok string
			_ = r2.Scan(&ok)
			fmt.Println(ok)
		}
		r2.Close()
	} else {
		fmt.Fprintf(os.Stderr, "banker_trades owner_key query failed: %v\n", err)
	}

	fmt.Println("--- Distinct owner_key in game_history_entries ---")
	r3, err := pool.Query(ctx, "SELECT DISTINCT owner_key FROM public.game_history_entries ORDER BY owner_key")
	if err == nil {
		for r3.Next() {
			var ok string
			_ = r3.Scan(&ok)
			fmt.Println(ok)
		}
		r3.Close()
	} else {
		fmt.Fprintf(os.Stderr, "game_history_entries owner_key query failed: %v\n", err)
	}

	// Additional safe SELECTs requested: specific checks for banker_trades and auto_payouts
	fmt.Println() 
	fmt.Println("--- Safe SELECT: banker_trades for player 'bvanmker' ---")
	btRows, err := pool.Query(ctx, "SELECT id, player_name, status, player_trade_id, owner_key, created_at FROM public.banker_trades WHERE lower(player_name)=lower('bvanmker') ORDER BY created_at DESC LIMIT 500")
	if err == nil {
		for btRows.Next() {
			var id int64
			var player, status, owner string
			var playerTradeID sql.NullInt64
			var created time.Time
			if err := btRows.Scan(&id, &player, &status, &playerTradeID, &owner, &created); err == nil {
				fmt.Printf("%d\t%s\t%s\t%v\t%s\t%s\n", id, player, status, playerTradeID.Int64, owner, created)
			}
		}
		btRows.Close()
	} else {
		fmt.Fprintf(os.Stderr, "banker_trades player query failed: %v\n", err)
	}

	fmt.Println()
	fmt.Println("--- Safe SELECT: banker_trades with status 'paying' ---")
	payingRows, err := pool.Query(ctx, "SELECT id, player_name, status, player_trade_id, owner_key, created_at FROM public.banker_trades WHERE lower(status)=lower('paying') ORDER BY created_at DESC LIMIT 500")
	if err == nil {
		for payingRows.Next() {
			var id int64
			var player, status, owner string
			var playerTradeID sql.NullInt64
			var created time.Time
			if err := payingRows.Scan(&id, &player, &status, &playerTradeID, &owner, &created); err == nil {
				fmt.Printf("%d\t%s\t%s\t%v\t%s\t%s\n", id, player, status, playerTradeID.Int64, owner, created)
			}
		}
		payingRows.Close()
	} else {
		fmt.Fprintf(os.Stderr, "banker_trades paying query failed: %v\n", err)
	}

	fmt.Println()
	fmt.Println("--- Safe SELECT: auto_payouts related to player or banker trades ---")
	apRows, err := pool.Query(ctx, "SELECT id, player_name, item_name, quantity, status, created_at, COALESCE(player_trade_id,0), COALESCE(banker_trade_id,0) FROM public.auto_payouts WHERE lower(player_name)=lower('bvanmker') OR player_trade_id IN (SELECT id FROM public.banker_trades WHERE lower(player_name)=lower('bvanmker')) ORDER BY created_at DESC LIMIT 500")
	if err == nil {
		for apRows.Next() {
			var id int64
			var player, item, status string
			var qty int
			var created time.Time
			var pTrade, bTrade int64
			if err := apRows.Scan(&id, &player, &item, &qty, &status, &created, &pTrade, &bTrade); err == nil {
				fmt.Printf("%d\t%s\t%s\t%d\t%s\t%v\t%v\n", id, player, item, qty, status, pTrade, bTrade)
			}
		}
		apRows.Close()
	} else {
		fmt.Fprintf(os.Stderr, "auto_payouts related query failed: %v\n", err)
	}
}
