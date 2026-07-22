package main

import "strings"
import "testing"

func TestBankerTradeSchemaStatementsIncludeRequiredColumns(t *testing.T) {
	statements := bankerTradeSchemaStatements()
	joined := strings.Join(statements, "\n")

	required := []string{
		"player_name",
		"bet_items",
		"banker_name",
		"player_trade_id",
		"player_chat_id",
		"owner_key",
		"risk_bank",
		"bet_amount",
		"risk_status",
	}

	for _, column := range required {
		if !strings.Contains(joined, column) {
			t.Fatalf("bankerTradeSchemaStatements() missing %q in statements: %s", column, joined)
		}
	}
}
