//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DBConfig struct {
	DatabaseURL string `json:"databaseUrl"`
	OwnerKey    string `json:"ownerKey"`
}

func main() {
	data, err := os.ReadFile("../../db.local.json")
	if err != nil {
		data, err = os.ReadFile("db.local.json")
	}
	if err != nil {
		log.Fatalf("could not read db.local.json: %v", err)
	}
	var cfg DBConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("could not parse db.local.json: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("pool create failed: %v", err)
	}
	defer db.Close()

	owner := cfg.OwnerKey
	now := time.Now().Format(time.RFC3339)
	entryID := fmt.Sprintf("sim-%d", time.Now().UnixNano())

	fmt.Printf("Inserting simulated game (id=%s, owner=%q)...\n", entryID, owner)

	notesJSON, _ := json.Marshal([]string{"simulated game"})

	_, err = db.Exec(ctx, `
		INSERT INTO game_history_entries (
			id, owner_key, player_name, started_at, updated_at, completed_at,
			game, winner, status, issue, issue_reason, player_result, dealer_result,
			notes, choice, choice_shout, payout_multiplier, updated_db_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,
			$7,$8,$9,$10,$11,$12,$13,
			$14,$15,$16,$17,NOW()
		)
		ON CONFLICT (id, owner_key) DO UPDATE SET updated_db_at = NOW()
	`,
		entryID, owner, "TestPlayer", now, now, now,
		"dice", "TestPlayer", "completed", false, "", "win", "loss",
		notesJSON, "high", "", 2,
	)
	if err != nil {
		log.Fatalf("INSERT entry failed: %v", err)
	}
	fmt.Println("Entry inserted OK")

	_, err = db.Exec(ctx, `
		INSERT INTO game_history_items (entry_id, owner_key, item_type, item_index, item_name, quantity, raw_data)
		VALUES ($1,$2,'bet',0,'HC',$3,'')
	`, entryID, owner, 5)
	if err != nil {
		log.Fatalf("INSERT item failed: %v", err)
	}
	fmt.Println("Item inserted OK")

	// Read it back
	var id, playerName, game, status string
	var updatedDBAt time.Time
	err = db.QueryRow(ctx, `SELECT id, player_name, game, status, updated_db_at FROM game_history_entries WHERE id=$1 AND owner_key=$2`, entryID, owner).
		Scan(&id, &playerName, &game, &status, &updatedDBAt)
	if err != nil {
		log.Fatalf("read-back failed: %v", err)
	}
	fmt.Printf("\nRead back: id=%s player=%q game=%s status=%s saved_at=%s\n", id, playerName, game, status, updatedDBAt.Format(time.RFC3339))
	fmt.Println("\nSimulation SUCCESS - DB is reachable and writes work fine.")
	fmt.Printf("\nNow check the app logs for '[GAME_HISTORY][DB]' messages to see if the app connected.\n")
	fmt.Printf("owner_key used by this test: %q\n", owner)

	// Clean up sim row
	_, _ = db.Exec(ctx, `DELETE FROM game_history_entries WHERE id=$1 AND owner_key=$2`, entryID, owner)
	fmt.Println("Cleaned up sim row.")
}
