//go:build ignore
// +build ignore

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type TradeItem struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	RawData  string `json:"rawData,omitempty"`
}

type LiveDealerStatusPayload struct {
	LastSeenAt         string      `json:"lastSeenAt"`
	DealerOpen         bool        `json:"dealerOpen"`
	TradeOpen          bool        `json:"tradeOpen"`
	GameActive         bool        `json:"gameActive"`
	SnapshotReady      bool        `json:"snapshotReady"`
	DealerName         string      `json:"dealerName"`
	RoomName           string      `json:"roomName"`
	MaxUniqueItems     int         `json:"maxUniqueItems"`
	MaxQuantityPerItem int         `json:"maxQuantityPerItem"`
	RiskEnabled        bool        `json:"riskEnabled,omitempty"`
	Snapshot           []TradeItem `json:"snapshot,omitempty"`
}

func main() {
	// flags/env
	dbURL := os.Getenv("DB_URL")
	banker := os.Getenv("BANKER_NAME")
	owner := os.Getenv("DB_OWNER")
	liveURL := os.Getenv("LIVE_SYNC_URL")
	if liveURL == "" {
		liveURL = "http://rollorigins.club/api/live-dealer"
	}
	auth := os.Getenv("AUTH_TOKEN")
	if auth == "" {
		auth = "Bearer s3cUr3-r4nd0m_v4lu3-6f2b8a"
	}
	times := 5
	interval := 2

	// optional CLI flags to override env
	flag.IntVar(&times, "n", times, "number of posts")
	flag.IntVar(&interval, "i", interval, "interval seconds")
	flag.Parse()

	if dbURL == "" {
		log.Fatal("DB_URL environment variable required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer conn.Close(context.Background())

	// pick banker if not provided
	if strings.TrimSpace(banker) == "" {
		var b string
		err = conn.QueryRow(context.Background(), `SELECT DISTINCT banker_name FROM banker_inventory LIMIT 1`).Scan(&b)
		if err != nil {
			log.Fatalf("failed to find any banker_name in banker_inventory and BANKER_NAME not set: %v", err)
		}
		banker = b
		log.Printf("auto-selected banker: %s", banker)
	}

	// build snapshot from DB: join to stocked_items and require is_active = TRUE
	var rows pgx.Rows
	if owner != "" {
		rows, err = conn.Query(context.Background(), `
			SELECT bi.item_name, bi.quantity
			FROM banker_inventory bi
			JOIN public.stocked_items si
			  ON LOWER(si.raw_name) = LOWER(bi.item_name) AND si.owner_key = $2
			WHERE bi.banker_name = $1 AND si.is_active = TRUE
		`, banker, owner)
	} else {
		rows, err = conn.Query(context.Background(), `
			SELECT bi.item_name, bi.quantity
			FROM banker_inventory bi
			JOIN public.stocked_items si
			  ON LOWER(si.raw_name) = LOWER(bi.item_name)
			WHERE bi.banker_name = $1 AND si.is_active = TRUE
		`, banker)
	}
	if err != nil {
		log.Fatalf("db query error: %v", err)
	}
	defer rows.Close()

	snapshot := make([]TradeItem, 0)
	for rows.Next() {
		var name string
		var qty int
		if err := rows.Scan(&name, &qty); err != nil {
			log.Printf("row scan: %v", err)
			continue
		}
		snapshot = append(snapshot, TradeItem{Name: name, Quantity: qty})
	}

	if len(snapshot) == 0 {
		log.Fatalf("no active stocked items found in banker_inventory for banker %q", banker)
	}
	log.Printf("loaded %d snapshot items from DB", len(snapshot))

	payload := LiveDealerStatusPayload{
		DealerOpen:         true,
		TradeOpen:          false,
		GameActive:         false,
		SnapshotReady:      true,
		DealerName:         banker,
		RoomName:           "",
		MaxUniqueItems:     5,
		MaxQuantityPerItem: 50,
		Snapshot:           snapshot,
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for i := 0; i < times; i++ {
		payload.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
		jb, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", liveURL, bytes.NewReader(jb))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", auth)

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[%d] POST error: %v", i+1, err)
		} else {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Printf("[%d] %s %d: %s", i+1, liveURL, resp.StatusCode, strings.TrimSpace(string(b)))
		}

		if i < times-1 {
			time.Sleep(time.Duration(interval) * time.Second)
		}
	}

	fmt.Println("done")
}
