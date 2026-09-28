package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"database/sql"
	_ "modernc.org/sqlite"
)

func main() {
	conn := "postgresql://neondb_owner:npg_cPwtQn4ZGh7J@ep-crimson-frost-b4a01mk-pooler.c-6.us-east-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	ctx := context.Background()
	conn = strings.TrimPrefix(conn, "file:")
	pool, err := sql.Open("sqlite", conn)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	player := "bvanmker"
	targetOwner := "roll-origins"

	fmt.Println("Beginning backup + update transaction...")
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// Create backup table if not exists
	_, err = tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS tmp_banker_trades_backup AS SELECT * FROM banker_trades WHERE false;")
	if err != nil {
		log.Fatalf("create backup table: %v", err)
	}

	// Insert matching rows into backup
	res, err := tx.ExecContext(ctx, "INSERT INTO tmp_banker_trades_backup SELECT * FROM banker_trades WHERE lower(player_name)=lower(?)", player)
	if err != nil {
		log.Fatalf("backup insert failed: %v", err)
	}
	fmt.Printf("Backed up rows: %v\n", res.RowsAffected())

	// Show rows before update
	fmt.Println("Rows before update:")
	r, err := tx.QueryContext(ctx, "SELECT id, player_name, status, owner_key FROM banker_trades WHERE lower(player_name)=lower(?) ORDER BY created_at DESC", player)
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
	res2, err := tx.ExecContext(ctx, "UPDATE banker_trades SET owner_key=? WHERE lower(player_name)=lower(?)", targetOwner, player)
	if err != nil {
		log.Fatalf("update failed: %v", err)
	}
	fmt.Printf("Updated rows: %v\n", res2.RowsAffected())

	// Show rows after update
	fmt.Println("Rows after update:")
	r2, err := tx.QueryContext(ctx, "SELECT id, player_name, status, owner_key FROM banker_trades WHERE lower(player_name)=lower(?) ORDER BY created_at DESC", player)
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
	if err := tx.Commit(); err != nil {
		log.Fatalf("commit failed: %v", err)
	}

	fmt.Println("Transaction committed.")
	// small sleep to let neon reflect
	time.Sleep(500 * time.Millisecond)

	// Final verification outside tx
	rows, err := pool.QueryContext(ctx, "SELECT id, player_name, status, owner_key FROM banker_trades WHERE lower(player_name)=lower(?) ORDER BY created_at DESC", player)
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
