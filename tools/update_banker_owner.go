package main

import (
	"context"
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

	player := "bvanmker"
	targetOwner := "roll-origins"

	fmt.Println("Beginning backup + update transaction...")
	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Create backup table if not exists
	_, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS tmp_banker_trades_backup AS SELECT * FROM public.banker_trades WHERE false;")
	if err != nil {
		log.Fatalf("create backup table: %v", err)
	}

	// Insert matching rows into backup
	res, err := tx.Exec(ctx, "INSERT INTO tmp_banker_trades_backup SELECT * FROM public.banker_trades WHERE lower(player_name)=lower($1)", player)
	if err != nil {
		log.Fatalf("backup insert failed: %v", err)
	}
	fmt.Printf("Backed up rows: %v\n", res.RowsAffected())

	// Show rows before update
	fmt.Println("Rows before update:")
	r, err := tx.Query(ctx, "SELECT id, player_name, status, owner_key FROM public.banker_trades WHERE lower(player_name)=lower($1) ORDER BY created_at DESC", player)
	if err != nil {
		log.Fatalf("select before update failed: %v", err)
	}
	for r.Next() {
		var id int64
		var playerName, status, ownerKey string
		if err := r.Scan(&id, &playerName, &status, &ownerKey); err != nil {
			log.Fatalf("scan before: %v", err)
		}
		fmt.Printf("%d\t%s\t%s\t%s\n", id, playerName, status, ownerKey)
	}
	r.Close()

	// Perform update
	res2, err := tx.Exec(ctx, "UPDATE public.banker_trades SET owner_key=$1 WHERE lower(player_name)=lower($2)", targetOwner, player)
	if err != nil {
		log.Fatalf("update failed: %v", err)
	}
	fmt.Printf("Updated rows: %v\n", res2.RowsAffected())

	// Show rows after update
	fmt.Println("Rows after update:")
	r2, err := tx.Query(ctx, "SELECT id, player_name, status, owner_key FROM public.banker_trades WHERE lower(player_name)=lower($1) ORDER BY created_at DESC", player)
	if err != nil {
		log.Fatalf("select after update failed: %v", err)
	}
	for r2.Next() {
		var id int64
		var playerName, status, ownerKey string
		if err := r2.Scan(&id, &playerName, &status, &ownerKey); err != nil {
			log.Fatalf("scan after: %v", err)
		}
		fmt.Printf("%d\t%s\t%s\t%s\n", id, playerName, status, ownerKey)
	}
	r2.Close()

	// Commit
	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit failed: %v", err)
	}

	fmt.Println("Transaction committed.")
	// small sleep to let neon reflect
	time.Sleep(500 * time.Millisecond)

	// Final verification outside tx
	rows, err := pool.Query(ctx, "SELECT id, player_name, status, owner_key FROM public.banker_trades WHERE lower(player_name)=lower($1) ORDER BY created_at DESC", player)
	if err != nil {
		log.Fatalf("final select failed: %v", err)
	}
	for rows.Next() {
		var id int64
		var playerName, status, ownerKey string
		if err := rows.Scan(&id, &playerName, &status, &ownerKey); err != nil {
			log.Fatalf("scan final: %v", err)
		}
		fmt.Printf("%d\t%s\t%s\t%s\n", id, playerName, status, ownerKey)
	}
	rows.Close()

	fmt.Println("Done.")
	os.Exit(0)
}
