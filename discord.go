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
	// Prefer environment-configured webhook; fall back to hardcoded value for
	// backwards compatibility. Set `DISCORD_WEBHOOK_URL` to override.
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if strings.TrimSpace(webhookURL) == "" {
		webhookURL = "https://discord.com/api/webhooks/1496681436592214016/QTGLb6qYMv0-61hVc3m9s7mBgvMc-E0LKpQTxd1bSow9N_GqOjQMyw9njq8KcsM8Jhi6"
		a.AddLogMsg("[DISCORD] using built-in webhook URL (env DISCORD_WEBHOOK_URL not set)")
	} else {
		a.AddLogMsg("[DISCORD] using webhook URL from DISCORD_WEBHOOK_URL")
	}

	// Optional secondary webhook for Issue-only posts (env override: DISCORD_ISSUE_WEBHOOK_URL)
	issueWebhookURL := os.Getenv("DISCORD_ISSUE_WEBHOOK_URL")
	if strings.TrimSpace(issueWebhookURL) == "" {
		issueWebhookURL = "https://discord.com/api/webhooks/1502209413065343086/lV-mzQvSRCqc-HkjKZWXOrmX0McP1HU47_fBjthixU2IdO0Bh18j-FBkIjGCDDjgAbo4"
	}

	// Optional dedicated Bandit webhook for game/payout/issue posts (env override: DISCORD_BANDIT_WEBHOOK_URL)
	banditWebhookURL := os.Getenv("DISCORD_BANDIT_WEBHOOK_URL")
	if strings.TrimSpace(banditWebhookURL) == "" {
		banditWebhookURL = "https://discordapp.com/api/webhooks/1502995064811425885/1_18izV6OFWGOeF-PxjFfLMtqna3vZv2Suo1DezK7BEBfaNc5bDAQfAHt4_aFTipQsEM"
	}

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

	// Prepare risk display values
	riskRound := "No"
	if entry.RiskSession {
		riskRound = "Yes"
	}
	riskedStr := strconv.Itoa(entry.RiskPending)
	bankStr := strconv.Itoa(entry.RiskBank)

	// Determine embed color and title prefix. Issue takes precedence.
	embedColor := 3447003
	titlePrefix := ""
	winnerName := strings.TrimSpace(entry.Winner)
	playerName := strings.TrimSpace(entry.PlayerName)
	if entry.Issue || strings.EqualFold(entry.Status, "Issue") {
		embedColor = 15158332
		titlePrefix = "⚠️ "
	} else if strings.EqualFold(winnerName, playerName) {
		embedColor = 16753920
		titlePrefix = "💸 "
	} else if strings.EqualFold(winnerName, "Dealer") || strings.EqualFold(winnerName, strings.TrimSpace(a.getCurrentDealerName())) {
		embedColor = 5763719
		titlePrefix = "💰 "
	}

	fields := []map[string]interface{}{
		{"name": "Winner", "value": formatField(entry.Winner), "inline": true},
		{"name": "Outcome", "value": formatField(entry.Status), "inline": true},
		{"name": "Choice", "value": formatField(entry.Choice), "inline": true},
		{"name": "Player Result", "value": formatField(entry.PlayerResult), "inline": true},
		{"name": "Dealer Result", "value": formatField(entry.DealerResult), "inline": true},
		{"name": "Payout Multiplier", "value": fmt.Sprintf("%.2fx", entry.PayoutMultiplier), "inline": true},
		{"name": "Risk Round", "value": formatField(riskRound), "inline": true},
		{"name": "Risked", "value": formatField(riskedStr), "inline": true},
		{"name": "Bank After", "value": formatField(bankStr), "inline": true},
		{"name": "Risk Decision", "value": formatField(entry.RiskDecision), "inline": true},
		{"name": "Player Shout", "value": formatField(entry.ChoiceShout), "inline": false},
		{"name": "Bet Items", "value": formatItems(entry.BetItems), "inline": false},
		{"name": "Payout Items", "value": formatItems(entry.PayoutItems), "inline": false},
		{"name": "Notes", "value": formatNotes(entry.Notes), "inline": false},
		{"name": "Started At", "value": entry.StartedAt, "inline": true},
		{"name": "Completed At", "value": entry.CompletedAt, "inline": true},
	}
	if strings.TrimSpace(entry.IssueReason) != "" {
		fields = append(fields, map[string]interface{}{"name": "Issue Reason", "value": formatField(entry.IssueReason), "inline": false})
	}

	// Include structured issue metadata when present
	if strings.TrimSpace(entry.IssueType) != "" {
		fields = append(fields, map[string]interface{}{"name": "Issue Type", "value": formatField(entry.IssueType), "inline": true})
	}
	if entry.IssueOwed > 0 {
		fields = append(fields, map[string]interface{}{"name": "Owed Amount", "value": strconv.Itoa(entry.IssueOwed), "inline": true})
	}
	if strings.TrimSpace(entry.IssueOwedItems) != "" {
		fields = append(fields, map[string]interface{}{"name": "Owed Items", "value": formatField(entry.IssueOwedItems), "inline": false})
	}

	embed := map[string]interface{}{
		"title":       fmt.Sprintf("%s%s — %s", titlePrefix, entry.Game, entry.Status),
		"description": fmt.Sprintf("Player: %s", entry.PlayerName),
		"color":       embedColor,
		"fields":      fields,
		"timestamp":   entry.CompletedAt,
		"footer":      map[string]interface{}{"text": fmt.Sprintf("Game ID: %s", entry.ID)},
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

	isIssue := entry.Issue || strings.EqualFold(entry.Status, "Issue")
	isBandit := strings.EqualFold(entry.Game, "Bandit")
	targetURL := webhookURL
	channelName := "history"
	if isBandit {
		targetURL = banditWebhookURL
		channelName = "bandit"
		a.AddLogMsg("[DISCORD] game is Bandit; routing to dedicated bandit webhook")
	} else if isIssue {
		targetURL = issueWebhookURL
		channelName = "issue"
		a.AddLogMsg("[DISCORD] game is an issue; routing exclusively to issues channel")
	}

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(jb))
	if err != nil {
		a.AddErrorLog(fmt.Sprintf("[DISCORD] %s webhook request error", channelName), err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "roll-origins/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.AddErrorLog(fmt.Sprintf("[DISCORD] %s webhook POST error", channelName), err)
		return
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	respBody := strings.TrimSpace(string(bodyBytes))
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if respBody == "" {
			a.AddLogMsg(fmt.Sprintf("[DISCORD] %s webhook sent (no response body)", channelName))
		} else {
			a.AddLogMsg(fmt.Sprintf("[DISCORD] %s webhook sent; body=%q", channelName, respBody))
		}
	} else {
		a.AddLogMsg(fmt.Sprintf("[DISCORD] %s webhook responded: %d body=%q", channelName, resp.StatusCode, respBody))
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

// sendDiscordWebhookForPayout posts a focused embed about a completed payout
// separate from the game result. Payout embeds intentionally avoid including
// game-specific fields (choice/results) and instead surface the payout items,
// payout decision (Keep/Risk), and any owed/itemized issue metadata.
func (a *App) sendDiscordWebhookForPayout(entry GameHistoryEntry) {
	// Payout webhook URL (env override: DISCORD_PAYOUT_WEBHOOK_URL)
	payoutWebhookURL := os.Getenv("DISCORD_PAYOUT_WEBHOOK_URL")
	if strings.TrimSpace(payoutWebhookURL) == "" {
		payoutWebhookURL = "https://discord.com/api/webhooks/1502425167031173211/IZF_TGQnk_rXeR5kgzpFJLQzAU6a6NVe05MTQavYn3kt2QQOQNpw7d7QxeKkZuVJscZP"
	}

	// Ensure we have an issues webhook available in this scope (env override)
	issueWebhookURL := os.Getenv("DISCORD_ISSUE_WEBHOOK_URL")
	if strings.TrimSpace(issueWebhookURL) == "" {
		issueWebhookURL = "https://discord.com/api/webhooks/1502209413065343086/lV-mzQvSRCqc-HkjKZWXOrmX0McP1HU47_fBjthixU2IdO0Bh18j-FBkIjGCDDjgAbo4"
	}

	// Bandit webhook for all bandit-related events
	banditWebhookURL := os.Getenv("DISCORD_BANDIT_WEBHOOK_URL")
	if strings.TrimSpace(banditWebhookURL) == "" {
		banditWebhookURL = "https://discordapp.com/api/webhooks/1502995064811425885/1_18izV6OFWGOeF-PxjFfLMtqna3vZv2Suo1DezK7BEBfaNc5bDAQfAHt4_aFTipQsEM"
	}

	// Decide destination: successful payouts go to payout channel, issues go to issues channel.
	targetWebhookURL := payoutWebhookURL
	if strings.EqualFold(entry.Game, "Bandit") {
		targetWebhookURL = banditWebhookURL
		a.AddLogMsg("[DISCORD] sending bandit payout (success or issue) to dedicated bandit webhook")
	} else if entry.Issue || strings.EqualFold(entry.Status, "Issue") {
		targetWebhookURL = issueWebhookURL
		a.AddLogMsg("[DISCORD] sending payout Issue only to configured issues webhook")
	} else {
		a.AddLogMsg("[DISCORD] sending payout webhook to configured payout channel")
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

	// Title / color selection: Issues take precedence.
	embedColor := 16753920
	titlePrefix := "💸 "
	if entry.Issue || strings.EqualFold(entry.Status, "Issue") {
		embedColor = 15158332
		titlePrefix = "⚠️ "
	}

	fields := []map[string]interface{}{
		{"name": "Payout To", "value": formatField(entry.PlayerName), "inline": true},
		{"name": "Payout Decision", "value": formatField(entry.RiskDecision), "inline": true},
		{"name": "Payout Items", "value": formatItems(entry.PayoutItems), "inline": false},
	}

	if strings.TrimSpace(entry.IssueType) != "" {
		fields = append(fields, map[string]interface{}{"name": "Issue Type", "value": formatField(entry.IssueType), "inline": true})
	}
	if entry.IssueOwed > 0 {
		fields = append(fields, map[string]interface{}{"name": "Owed Amount", "value": strconv.Itoa(entry.IssueOwed), "inline": true})
	}
	if strings.TrimSpace(entry.IssueOwedItems) != "" {
		fields = append(fields, map[string]interface{}{"name": "Owed Items", "value": formatField(entry.IssueOwedItems), "inline": false})
	}

	if len(entry.Notes) > 0 {
		fields = append(fields, map[string]interface{}{"name": "Notes", "value": formatNotes(entry.Notes), "inline": false})
	}

	// Use CompletedAt if present, otherwise timestamp now for the embed
	timestamp := entry.CompletedAt
	if strings.TrimSpace(timestamp) == "" {
		timestamp = time.Now().Format(time.RFC3339)
	}

	titleStatus := "Completed"
	if entry.Issue || strings.EqualFold(entry.Status, "Issue") {
		titleStatus = "Issue"
	}
	embed := map[string]interface{}{
		"title":       fmt.Sprintf("%sPayout — %s", titlePrefix, titleStatus),
		"description": fmt.Sprintf("Payout for: %s", entry.PlayerName),
		"color":       embedColor,
		"fields":      fields,
		"timestamp":   timestamp,
		"footer":      map[string]interface{}{"text": fmt.Sprintf("Game ID: %s", entry.ID)},
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
		a.AddErrorLog("[DISCORD] payout marshal error", err)
		return
	}

	// Write a separate debug payload file for payout events
	go func(b []byte) {
		if err := os.WriteFile("payload_payout.json", b, 0600); err != nil {
			a.AddLogMsg(fmt.Sprintf("[DISCORD] payout payload write failed: %v", err))
		}
	}(append([]byte(nil), jb...))

	req, err := http.NewRequest("POST", targetWebhookURL, bytes.NewReader(jb))
	if err != nil {
		a.AddErrorLog("[DISCORD] payout request error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "roll-origins/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.AddErrorLog("[DISCORD] payout POST error", err)
		return
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	respBody := strings.TrimSpace(string(bodyBytes))
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if respBody == "" {
			a.AddLogMsg("[DISCORD] payout webhook sent (no response body)")
		} else {
			a.AddLogMsg(fmt.Sprintf("[DISCORD] payout webhook sent; body=%q", respBody))
		}
	} else {
		a.AddLogMsg(fmt.Sprintf("[DISCORD] payout webhook responded: %d body=%q", resp.StatusCode, respBody))
	}

	// When this is an Issue we already sent to the issues webhook above;
	// do not duplicate posts to the payout channel.
}
