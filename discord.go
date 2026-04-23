package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

	embed := map[string]interface{}{
		"title":       fmt.Sprintf("%s — %s", entry.Game, entry.Status),
		"description": fmt.Sprintf("Player: %s", entry.PlayerName),
		"color":       3447003,
		"fields": []map[string]interface{}{
			{"name": "Winner", "value": entry.Winner, "inline": true},
			{"name": "Outcome", "value": entry.Status, "inline": true},
			{"name": "Player Result", "value": entry.PlayerResult, "inline": true},
			{"name": "Dealer Result", "value": entry.DealerResult, "inline": true},
			{"name": "Bet Items", "value": formatItems(entry.BetItems), "inline": false},
			{"name": "Payout Items", "value": formatItems(entry.PayoutItems), "inline": false},
			{"name": "Started At", "value": entry.StartedAt, "inline": true},
			{"name": "Completed At", "value": entry.CompletedAt, "inline": true},
		},
		"timestamp": entry.CompletedAt,
		"footer":    map[string]interface{}{"text": fmt.Sprintf("Game ID: %s", entry.ID)},
	}

	payload := map[string]interface{}{
		"username": "Gamba-Suite",
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

	// Persist the exact payload to payload.json (overwrite each game)
	payloadFile := "payload.json"
	if err := os.WriteFile(payloadFile, jb, 0600); err != nil {
		a.AddLogMsg(fmt.Sprintf("[DISCORD] failed to write payload file: %v", err))
	} else {
		a.AddLogMsg("[DISCORD] payload written to " + payloadFile)
	}

	// Read back the payload file to ensure we post the exact bytes written.
	fileBytes, fileErr := os.ReadFile(payloadFile)
	if fileErr == nil {
		jb = fileBytes
	}

	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(jb))
	if err != nil {
		a.AddErrorLog("[DISCORD] request error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Gamba-Suite/1.0")

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
