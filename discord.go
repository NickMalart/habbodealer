package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// sendDiscordWebhookForGame posts a nicely formatted embed about a completed
// game to the configured Discord webhook (read from DISCORD_WEBHOOK_URL).
func (a *App) sendDiscordWebhookForGame(entry GameHistoryEntry) {
	// Hardcoded webhook URL (provided by user)
	webhookURL := "https://discordapp.com/api/webhooks/1496681436592214016/QTGLb6qYMv0-61hVc3m9s7mBgvMc-E0LKpQTxd1bSow9N_GqOjQMyw9njq8KcsM8Jhi6"
	a.AddLogMsg("[DISCORD] using hardcoded webhook URL")

	// Validate webhook URL contains a numeric webhook ID (snowflake).
	if u, perr := url.Parse(webhookURL); perr == nil {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		for i, p := range parts {
			if p == "webhooks" && i+2 < len(parts) {
				webhookID := parts[i+1]
				token := parts[i+2]
				isDigits := true
				for _, r := range webhookID {
					if r < '0' || r > '9' {
						isDigits = false
						break
					}
				}
				maskedID := webhookID
				if len(maskedID) > 8 {
					maskedID = maskedID[:4] + "..." + maskedID[len(maskedID)-4:]
				}
				maskedToken := token
				if len(maskedToken) > 8 {
					maskedToken = maskedToken[:4] + "..." + maskedToken[len(maskedToken)-4:]
				} else {
					maskedToken = "****"
				}
				if !isDigits {
					a.AddLogMsg(fmt.Sprintf("[DISCORD] webhook id invalid: %q token=%q", maskedID, maskedToken))
					a.AddLogMsg("[DISCORD] webhook id must be numeric snowflake; check DISCORD_WEBHOOK_URL")
					return
				}
				a.AddLogMsg(fmt.Sprintf("[DISCORD] using webhook id=%s token=%s", maskedID, maskedToken))
				break
			}
		}
	} else {
		a.AddLogMsg(fmt.Sprintf("[DISCORD] failed to parse webhook URL: %v", perr))
	}

	formatItems := func(items []TradeItem) string {
		if len(items) == 0 {
			return "None"
		}
		parts := make([]string, 0, len(items))
		for _, it := range items {
			parts = append(parts, fmt.Sprintf("%s x%d", it.Name, it.Quantity))
		}
		s := strings.Join(parts, ", ")
		if len(s) > 900 {
			s = s[:900] + "…"
		}
		return s
	}

	formatNotes := func(notes []string) string {
		if len(notes) == 0 {
			return "None"
		}
		s := strings.Join(notes, "\n")
		if len(s) > 900 {
			s = s[:900] + "…"
		}
		return s
	}

	formatField := func(v string) string {
		if strings.TrimSpace(v) == "" {
			return "None"
		}
		if len(v) > 900 {
			return v[:900] + "…"
		}
		return v
	}

	embed := map[string]interface{}{
		"title":       fmt.Sprintf("%s — %s", entry.Game, entry.Status),
		"description": fmt.Sprintf("Player: %s", entry.PlayerName),
		"color":       3447003,
		"fields": []map[string]interface{}{
			{"name": "Winner", "value": formatField(entry.Winner), "inline": true},
			{"name": "Outcome", "value": formatField(entry.Status), "inline": true},
			{"name": "Choice", "value": formatField(entry.Choice), "inline": true},
			{"name": "Player Result", "value": formatField(entry.PlayerResult), "inline": true},
			{"name": "Dealer Result", "value": formatField(entry.DealerResult), "inline": true},
			{"name": "Payout Multiplier", "value": strconv.Itoa(entry.PayoutMultiplier), "inline": true},
			{"name": "Player Shout", "value": formatField(entry.ChoiceShout), "inline": false},
			{"name": "Bet Items", "value": formatItems(entry.BetItems), "inline": false},
			{"name": "Payout Items", "value": formatItems(entry.PayoutItems), "inline": false},
			{"name": "Notes", "value": formatNotes(entry.Notes), "inline": false},
			{"name": "Started At", "value": entry.StartedAt, "inline": true},
			{"name": "Completed At", "value": entry.CompletedAt, "inline": true},
		},
		"timestamp": entry.CompletedAt,
		"footer":    map[string]interface{}{"text": fmt.Sprintf("Game ID: %s", entry.ID)},
	}

	payload := map[string]interface{}{
		"username": "roll-origins",
		"embeds":   []interface{}{embed},
		"allowed_mentions": map[string][]string{
			"parse": []string{},
		},
	}

	jb, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		a.AddErrorLog("[DISCORD] marshal error", err)
		return
	}

	// Best-effort async payload dump for debugging without delaying webhook POST.
	go func(b []byte) {
		if err := os.WriteFile("payload.json", b, 0600); err != nil {
			a.AddLogMsg(fmt.Sprintf("[DISCORD] payload debug write failed: %v", err))
		}
	}(append([]byte(nil), jb...))

	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(jb))
	if err != nil {
		a.AddErrorLog("[DISCORD] request error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "roll-origins/1.0")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.AddErrorLog("[DISCORD] POST error", err)
		return
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	respBody := strings.TrimSpace(string(bodyBytes))
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if respBody == "" {
			a.AddLogMsg("[DISCORD] webhook sent (no response body)")
		} else {
			a.AddLogMsg(fmt.Sprintf("[DISCORD] webhook sent; body=%q", respBody))
		}
	} else {
		a.AddLogMsg(fmt.Sprintf("[DISCORD] webhook responded: %d body=%q", resp.StatusCode, respBody))
	}
}

// sendDiscordRoundResult posts the game outcome to Discord immediately when the
// round winner is known, without waiting for payout to complete. It builds a
// synthetic "Completed" snapshot so the history entry can remain open for
// payout bookkeeping. Call this only for player-win branches where payout is
// still pending.
func (a *App) sendDiscordRoundResult(winner string, playerResult string, dealerResult string, winnerMsg string) {
	a.gameHistoryMu.Lock()
	idx := a.findCurrentGameHistoryIndexLocked()
	if idx < 0 {
		a.gameHistoryMu.Unlock()
		return
	}
	// Clone and promote to Completed for the Discord-only snapshot.
	snap := a.gameHistory[idx]
	a.gameHistoryMu.Unlock()

	snap.Winner = winner
	if strings.TrimSpace(playerResult) != "" {
		snap.PlayerResult = playerResult
	}
	if strings.TrimSpace(dealerResult) != "" {
		snap.DealerResult = dealerResult
	}
	snap.Status = "Completed"
	snap.CompletedAt = time.Now().Format(time.RFC3339)
	if strings.TrimSpace(winnerMsg) != "" {
		snap.Notes = append(append([]string{}, snap.Notes...), winnerMsg)
	}

	go a.sendDiscordWebhookForGame(snap)

	// Also persist the completed snapshot to the DB immediately for statistics.
	// Issues are tracked in Discord; the DB just needs to know the game was
	// played, who won, and the result.
	snapForDB := snap
	go func() {
		if err := a.persistSingleGameEntryToDB(snapForDB); err != nil {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] round-result stats persist failed: %v", err))
		} else {
			a.AddLogMsg("[GAME_HISTORY][DB] round-result stats persisted ok")
		}
	}()
}
