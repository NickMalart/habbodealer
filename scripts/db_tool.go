package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type BankerBetItem struct {
	RawName string `json:"raw_name"`
	Qty     int    `json:"qty"`
}

func main() {
	show := flag.Bool("show", false, "show recent banker_trades and auto_payouts")
	makeLatest := flag.Bool("make-latest", false, "create auto_payout(s) from latest banker_trade")
	flag.Parse()

	conn := os.Getenv("DBURL")
	if conn == "" {
		if flag.NArg() > 0 {
			conn = flag.Arg(0)
		}
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

	if *show {
		fmt.Println("== recent banker_trades ==")
		r, err := pool.Query(ctx, "SELECT id, player_name, COALESCE(player_trade_id,0), status, created_at, bet_items FROM public.banker_trades ORDER BY created_at DESC LIMIT 20")
		if err != nil {
			fmt.Fprintf(os.Stderr, "query banker_trades error: %v\n", err)
		} else {
			for r.Next() {
				var id int
				var player string
				var playerTrade sql.NullInt64
				var status string
				var created time.Time
				var betItems []byte
				if err := r.Scan(&id, &player, &playerTrade, &status, &created, &betItems); err == nil {
					pt := "NULL"
					if playerTrade.Valid {
						pt = fmt.Sprintf("%d", playerTrade.Int64)
					}
					fmt.Printf("%d | %s | player_trade_id=%s | %s | %s | bet_items=%s\n", id, player, pt, status, created.Format(time.RFC3339), string(betItems))
				}
			}
			r.Close()
		}

		fmt.Println("\n== recent auto_payouts ==")
		var cnt int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM public.auto_payouts").Scan(&cnt); err == nil {
			fmt.Printf("auto_payouts count=%d\n", cnt)
		} else {
			fmt.Printf("auto_payouts count query error: %v\n", err)
		}
		r2, err := pool.Query(ctx, "SELECT id, player_name, COALESCE(player_trade_id,0), COALESCE(banker_trade_id,0), item_name, quantity, status, created_at FROM public.auto_payouts ORDER BY created_at DESC LIMIT 50")
		if err != nil {
			fmt.Fprintf(os.Stderr, "query auto_payouts error: %v\n", err)
		} else {
			for r2.Next() {
				var id, player, item, status, created string
				var playerTrade, bankerTrade int64
				var qty int
				if err := r2.Scan(&id, &player, &playerTrade, &bankerTrade, &item, &qty, &status, &created); err == nil {
					fmt.Printf("%s | %s | player_trade_id=%d | banker_trade_id=%d | %s x%d | %s | %s\n", id, player, playerTrade, bankerTrade, item, qty, status, created)
				} else {
					fmt.Printf("auto_payouts row scan error: %v\n", err)
				}
			}
			r2.Close()
		}
	}

	if *makeLatest {
		// Find latest banker_trade not completed
		row := pool.QueryRow(ctx, "SELECT id, player_name, COALESCE(player_trade_id,0), COALESCE(player_chat_id,0), bet_items FROM public.banker_trades WHERE status != 'completed' ORDER BY created_at DESC LIMIT 1")
		var btID int
		var player string
		var playerTrade int64
		var playerChat int64
		var betItems []byte
		if err := row.Scan(&btID, &player, &playerTrade, &playerChat, &betItems); err != nil {
			fmt.Fprintf(os.Stderr, "no banker_trade found to make payouts from: %v\n", err)
			os.Exit(3)
		}
		fmt.Printf("Using banker_trade id=%d player=%s player_trade_id=%d\n", btID, player, playerTrade)

		var items []BankerBetItem
		if err := json.Unmarshal(betItems, &items); err != nil {
			fmt.Fprintf(os.Stderr, "failed to unmarshal bet_items: %v\nraw: %s\n", err, string(betItems))
			os.Exit(4)
		}

		if len(items) == 0 {
			fmt.Println("No bet items parsed from banker_trade; nothing to insert.")
			os.Exit(0)
		}

		for _, it := range items {
			pid := fmt.Sprintf("%d", time.Now().UnixNano())
			created := time.Now().Format("2006-01-02 15:04:05")
			var tradeParam interface{}
			if playerTrade > 0 {
				tradeParam = playerTrade
			} else {
				tradeParam = nil
			}
			_, err := pool.Exec(ctx, "INSERT INTO public.auto_payouts (id, player_name, item_name, quantity, status, created_at, player_trade_id, banker_trade_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)", pid, player, it.RawName, it.Qty, "Pending", created, tradeParam, btID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "INSERT error: %v\n", err)
			} else {
				fmt.Printf("Inserted auto_payout id=%s player=%s item=%s qty=%d player_trade_id=%v banker_trade_id=%d\n", pid, player, it.RawName, it.Qty, tradeParam, btID)
			}
			// small pause for unique ids
			time.Sleep(15 * time.Millisecond)
		}
	}
}
