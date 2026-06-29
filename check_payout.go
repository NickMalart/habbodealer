package main

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
)

func main() {
	connStr := "postgresql://neondb_owner:npg_l8r4nExKaNGP@ep-rapid-night-a7u01fue-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require"
	conn, err := pgx.Connect(context.Background(), connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(context.Background(), "SELECT id, banker_trade_id, status FROM auto_payouts WHERE banker_trade_id = 962")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		found = true
		var id, btid int
		var status string
		rows.Scan(&id, &btid, &status)
		fmt.Printf("auto_payout id: %d, banker_trade_id: %d, status: %s\n", id, btid, status)
	}
	if !found {
		fmt.Println("No auto_payouts found for banker_trade_id 962")
	}
}
