package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func getDBURLFromLocalConfig() string {
	// Try to read db.local.json from repo root
	p := filepath.Join(".", "db.local.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	if v, ok := m["databaseUrl"].(string); ok {
		return v
	}
	return ""
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [DBURL] [bankerName]", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	dburl := os.Getenv("DBURL")
	if dburl == "" && flag.NArg() > 0 {
		dburl = flag.Arg(0)
	}
	if dburl == "" {
		dburl = getDBURLFromLocalConfig()
	}
	if dburl == "" {
		fmt.Fprintln(os.Stderr, "Database URL required via DBURL env, first arg, or db.local.json")
		os.Exit(2)
	}

	var bankerName string
	if flag.NArg() > 1 {
		bankerName = flag.Arg(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dburl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect error: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if bankerName != "" {
		rows, err := pool.Query(ctx, `SELECT item_name, quantity FROM public.banker_inventory WHERE LOWER(banker_name) = LOWER($1) ORDER BY item_name ASC`, bankerName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "query error: %v\n", err)
			os.Exit(1)
		}
		defer rows.Close()
		fmt.Printf("Banker inventory for '%s':\n", bankerName)
		found := false
		for rows.Next() {
			var item string
			var qty int
			if err := rows.Scan(&item, &qty); err != nil {
				fmt.Fprintf(os.Stderr, "row scan error: %v\n", err)
				continue
			}
			found = true
			fmt.Printf("- %s: %d\n", item, qty)
		}
		if !found {
			fmt.Println("(no rows)")
		}
		return
	}

	// No banker specified: list all bankers and their inventories
	rows, err := pool.Query(ctx, `SELECT banker_name, item_name, quantity FROM public.banker_inventory ORDER BY banker_name ASC, item_name ASC`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query error: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	byBank := make(map[string]map[string]int)
	for rows.Next() {
		var b, item string
		var qty int
		if err := rows.Scan(&b, &item, &qty); err != nil {
			fmt.Fprintf(os.Stderr, "row scan error: %v\n", err)
			continue
		}
		if _, ok := byBank[b]; !ok {
			byBank[b] = make(map[string]int)
		}
		byBank[b][item] = qty
	}

	bankers := make([]string, 0, len(byBank))
	for b := range byBank {
		bankers = append(bankers, b)
	}
	sort.Strings(bankers)

	if len(bankers) == 0 {
		fmt.Println("No banker_inventory rows found.")
		return
	}

	for _, b := range bankers {
		fmt.Printf("Banker: %s\n", b)
		items := byBank[b]
		keys := make([]string, 0, len(items))
		for k := range items {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("- %s: %d\n", k, items[k])
		}
		fmt.Println()
	}
}
