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

func TestAutoPayoutSchemaStatementsIncludeRequiredColumns(t *testing.T) {
	statements := autoPayoutSchemaStatements()
	joined := strings.Join(statements, "\n")

	required := []string{
		"player_name",
		"item_name",
		"quantity",
		"status",
		"created_at",
		"player_trade_id",
		"banker_trade_id",
		"notified",
	}

	for _, column := range required {
		if !strings.Contains(joined, column) {
			t.Fatalf("autoPayoutSchemaStatements() missing %q in statements: %s", column, joined)
		}
	}
}

func TestLegacyBanAndSettingsMigrationsArePresent(t *testing.T) {
	joined := strings.Join(bannedPlayersSchemaStatements(), "\n") + "\n" + strings.Join(autoPayoutSettingsSchemaStatements(), "\n")

	for _, snippet := range []string{
		"ban_key",
		"username",
		"idx_banned_players_ban_key",
		"setting_key",
		"idx_auto_payout_settings_key",
		"ctid",
	} {
		if !strings.Contains(joined, snippet) {
			t.Fatalf("legacy schema migration missing %q in migration statements: %s", snippet, joined)
		}
	}
}

func TestCanonicalDealerShoutAndBankerInventoryStatementsArePresent(t *testing.T) {
	joined := strings.Join(dealerShoutSchemaStatements(), "\n") + "\n" + strings.Join(bankerInventorySchemaStatements(), "\n")

	for _, snippet := range []string{
		"public.dealer_shouts",
		"target_player",
		"shout_type",
		"public.banker_inventory",
		"banker_name",
		"item_name",
		"quantity",
		"updated_at",
	} {
		if !strings.Contains(joined, snippet) {
			t.Fatalf("canonical dealer/banker schema missing %q in migration statements: %s", snippet, joined)
		}
	}
}
