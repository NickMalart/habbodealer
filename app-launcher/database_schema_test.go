package main

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestEnsureWorkspaceSchemaCreatesAllAppTablesAndIsIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := ensureWorkspaceSchema(ctx, db); err != nil {
		t.Fatalf("first schema bootstrap: %v", err)
	}
	if err := ensureWorkspaceSchema(ctx, db); err != nil {
		t.Fatalf("second schema bootstrap: %v", err)
	}

	wantTables := []string{
		"schema_migrations",
		"game_history_entries", "game_history_items", "trade_ledger", "dealer_shouts", "stocked_items",
		"auto_payouts", "banker_trades", "banker_inventory", "banned_players", "auto_payout_settings",
		"blocked_players", "ui_settings", "raffle_sessions", "raffle_participants",
		"trade_sessions", "trade_entries", "trade_entry_items", "room_rights",
	}
	for _, table := range wantTables {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil {
			t.Fatalf("check table %q: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %q was not created exactly once (count=%d)", table, count)
		}
	}

	for table, columns := range map[string][]string{
		"stocked_items":     {"owner_key"},
		"banker_inventory":  {"updated_at"},
		"auto_payouts":      {"notified"},
		"banker_trades":     {"risk_status", "updated_at"},
		"raffle_sessions":   {"winner_proof_file"},
		"trade_entry_items": {"raw_data"},
	} {
		rows, err := db.QueryContext(ctx, "PRAGMA table_info(\""+table+"\")")
		if err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		found := make(map[string]bool)
		for rows.Next() {
			var cid int
			var name, dataType string
			var notNull, primaryKey int
			var defaultValue any
			if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
				rows.Close()
				t.Fatalf("scan %s columns: %v", table, err)
			}
			for _, column := range columns {
				if name == column {
					found[column] = true
				}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("iterate %s columns: %v", table, err)
		}
		rows.Close()
		for _, column := range columns {
			if !found[column] {
				t.Errorf("column %s.%s was not created", table, column)
			}
		}
	}
}
