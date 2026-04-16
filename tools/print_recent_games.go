package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TradeItem struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	RawData  string `json:"rawData"`
}

type GameHistoryEntry struct {
	ID           string      `json:"id"`
	PlayerName   string      `json:"playerName"`
	StartedAt    string      `json:"startedAt"`
	UpdatedAt    string      `json:"updatedAt"`
	CompletedAt  string      `json:"completedAt,omitempty"`
	Game         string      `json:"game"`
	Winner       string      `json:"winner"`
	Status       string      `json:"status"`
	Issue        bool        `json:"issue"`
	IssueReason  string      `json:"issueReason"`
	PlayerResult string      `json:"playerResult"`
	DealerResult string      `json:"dealerResult"`
	BetItems     []TradeItem `json:"betItems"`
	PayoutItems  []TradeItem `json:"payoutItems"`
	Notes        []string    `json:"notes"`
}

type LiveGameSummary struct {
	ID          string      `json:"id"`
	Game        string      `json:"game"`
	Winner      string      `json:"winner"`
	Outcome     string      `json:"outcome"`
	StartedAt   string      `json:"startedAt,omitempty"`
	CompletedAt string      `json:"completedAt,omitempty"`
	BetItems    []TradeItem `json:"betItems,omitempty"`
	PayoutItems []TradeItem `json:"payoutItems,omitempty"`
}

func main() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get user config dir: %v\n", err)
		os.Exit(1)
	}
	configPath := filepath.Join(configDir, "Gamba-Suite")
	_ = os.MkdirAll(configPath, 0700)
	path := filepath.Join(configPath, "game_history.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "game history file not found: %s\n", path)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "failed to read game history file: %v\n", err)
		os.Exit(1)
	}

	var entries []GameHistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		fmt.Fprintf(os.Stderr, "failed to unmarshal game history: %v\n", err)
		os.Exit(1)
	}

	dealerName := strings.TrimSpace(os.Getenv("LIVE_SYNC_DEALER_NAME"))
	if dealerName == "" {
		dealerName = strings.TrimSpace(os.Getenv("USERNAME"))
	}
	if dealerName == "" {
		dealerName = "Dealer"
	}
	roomName := strings.TrimSpace(os.Getenv("LIVE_SYNC_ROOM_NAME"))

	n := 5
	games := make([]LiveGameSummary, 0, n)
	count := 0
	for i := 0; i < len(entries) && count < n; i++ {
		entry := entries[i]
		if strings.TrimSpace(entry.CompletedAt) == "" {
			continue
		}
		winner := strings.TrimSpace(entry.Winner)
		publicWinner := "Unknown"
		if winner != "" {
			if strings.EqualFold(winner, dealerName) {
				publicWinner = "Dealer"
			} else {
				publicWinner = "Player"
			}
		}

		gs := LiveGameSummary{
			ID:          entry.ID,
			Game:        entry.Game,
			Winner:      publicWinner,
			Outcome:     entry.Status,
			StartedAt:   entry.StartedAt,
			CompletedAt: entry.CompletedAt,
			BetItems:    entry.BetItems,
			PayoutItems: entry.PayoutItems,
		}
		games = append(games, gs)
		count++
	}

	payload := struct {
		LastSeenAt string            `json:"lastSeenAt"`
		DealerName string            `json:"dealerName"`
		RoomName   string            `json:"roomName"`
		Games      []LiveGameSummary `json:"games"`
	}{
		LastSeenAt: time.Now().UTC().Format(time.RFC3339),
		DealerName: dealerName,
		RoomName:   roomName,
		Games:      games,
	}

	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal payload: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}
