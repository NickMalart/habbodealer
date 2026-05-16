package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5"
)

type TradeItem struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	RawData  string `json:"rawData"`
}

func main() {
	dbURL := "postgresql://neondb_owner:npg_Jx8ERGzK6eog@ep-small-thunder-a7ceewoj-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	owner := "roll-origins"

	conn, err := pgx.Connect(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer conn.Close(context.Background())

	players := []string{"SATOSHl", "VolatileRhys", "LuckyShark", "BigBetter", "HabboKing"}
	items := []string{"HC Sofa", "Throne", "Dino Egg", "Blue Dragon", "Gold Bar"}

	rand.Seed(time.Now().UnixNano())

	fmt.Printf("Generating 50 fake ledger lines for owner %q...\n", owner)

	for i := 0; i < 50; i++ {
		player := players[rand.Intn(len(players))]
		tradeType := "IN"
		if rand.Float32() > 0.6 {
			tradeType = "OUT"
		}

		itemName := items[rand.Intn(len(items))]
		qty := rand.Intn(5) + 1
		if itemName == "HC Sofa" {
			qty = rand.Intn(20) + 1
		}

		tradeItems := []TradeItem{
			{Name: itemName, Quantity: qty},
		}
		itemsJSON, _ := json.Marshal(tradeItems)

		// Random time in the last 24 hours
		createdAt := time.Now().Add(-time.Duration(rand.Intn(24)) * time.Hour).Add(-time.Duration(rand.Intn(60)) * time.Minute)

		_, err := conn.Exec(context.Background(), `
			INSERT INTO trade_ledger (owner_key, partner_name, trade_type, total_quantity, items, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, owner, player, tradeType, qty, itemsJSON, createdAt)

		if err != nil {
			fmt.Printf("Failed to insert row %d: %v\n", i, err)
		} else {
			if i%10 == 0 {
				fmt.Printf("Inserted %d rows...\n", i)
			}
		}
	}

	fmt.Println("Successfully generated 50 fake ledger lines.")
}
