package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// casino_stats.go -- cleaned casino statistics built from GameHistoryEntry

type CasinoStats struct {
	GeneratedAt string               `json:"generatedAt"`
	Range       StatsRange           `json:"range"`
	Overall     CasinoStatsSummary   `json:"overall"`
	ByGame      map[string]GameStats `json:"byGame"`
	ByItem      []ItemStats          `json:"byItem"`
	ByPlayer    []PlayerStats        `json:"byPlayer"`
	Issues      IssueStats           `json:"issues"`
	Trends      TrendStats           `json:"trends"`
}

type StatsRange struct {
	Key     string `json:"key"`
	StartAt string `json:"startAt,omitempty"`
	EndAt   string `json:"endAt,omitempty"`
}

type CasinoStatsSummary struct {
	TotalRounds           int            `json:"totalRounds"`
	CompletedRounds       int            `json:"completedRounds"`
	IssueRounds           int            `json:"issueRounds"`
	PlayerWins            int            `json:"playerWins"`
	DealerWins            int            `json:"dealerWins"`
	PlayerWinRate         float64        `json:"playerWinRate"`
	DealerWinRate         float64        `json:"dealerWinRate"`
	IssueRate             float64        `json:"issueRate"`
	TotalBetItemsIn       int            `json:"totalBetItemsIn"`
	TotalPayoutItemsOut   int            `json:"totalPayoutItemsOut"`
	NetItems              int            `json:"netItems"`
	RTPPercent            float64        `json:"rtpPercent"`
	ProfitMarginPercent   float64        `json:"profitMarginPercent"`
	CasinoEdgePercent     float64        `json:"casinoEdgePercent"`
	AverageNetPerRound    float64        `json:"averageNetPerRound"`
	UniquePlayers         int            `json:"uniquePlayers"`
	BestGameByNet         string         `json:"bestGameByNet"`
	WorstGameByNet        string         `json:"worstGameByNet"`
	BestItemByNet         string         `json:"bestItemByNet"`
	WorstItemByNet        string         `json:"worstItemByNet"`
	MostProfitablePlayer  string         `json:"mostProfitablePlayer"`
	LeastProfitablePlayer string         `json:"leastProfitablePlayer"`
	BestSingleWin         int            `json:"bestSingleWin"`
	WorstSingleLoss       int            `json:"worstSingleLoss"`
	BetItemCounts         map[string]int `json:"betItemCounts"`
	PayoutItemCounts      map[string]int `json:"payoutItemCounts"`
	NetItemCounts         map[string]int `json:"netItemCounts"`
}

type GameStats struct {
	Game                string         `json:"game"`
	TotalRounds         int            `json:"totalRounds"`
	CompletedRounds     int            `json:"completedRounds"`
	IssueRounds         int            `json:"issueRounds"`
	PlayerWins          int            `json:"playerWins"`
	DealerWins          int            `json:"dealerWins"`
	PlayerWinRate       float64        `json:"playerWinRate"`
	DealerWinRate       float64        `json:"dealerWinRate"`
	TotalBetItemsIn     int            `json:"totalBetItemsIn"`
	TotalPayoutItemsOut int            `json:"totalPayoutItemsOut"`
	NetItems            int            `json:"netItems"`
	RTPPercent          float64        `json:"rtpPercent"`
	CasinoEdgePercent   float64        `json:"casinoEdgePercent"`
	AverageNetPerRound  float64        `json:"averageNetPerRound"`
	LargestBet          int            `json:"largestBet"`
	LargestPayout       int            `json:"largestPayout"`
	WorstCasinoLoss     int            `json:"worstCasinoLoss"`
	BestCasinoWin       int            `json:"bestCasinoWin"`
	BetItemCounts       map[string]int `json:"betItemCounts"`
	PayoutItemCounts    map[string]int `json:"payoutItemCounts"`
	NetItemCounts       map[string]int `json:"netItemCounts"`
}

type ItemStats struct {
	Name            string         `json:"name"`
	BetIn           int            `json:"betIn"`
	PayoutOut       int            `json:"payoutOut"`
	Net             int            `json:"net"`
	GamesPlayed     int            `json:"gamesPlayed"`
	CasinoWins      int            `json:"casinoWins"`
	CasinoLosses    int            `json:"casinoLosses"`
	CasinoWinRate   float64        `json:"casinoWinRate"`
	Status          string         `json:"status"`
	ByGameBetIn     map[string]int `json:"byGameBetIn"`
	ByGamePayoutOut map[string]int `json:"byGamePayoutOut"`
	ByGameNet       map[string]int `json:"byGameNet"`
}

type PlayerStats struct {
	PlayerName        string             `json:"playerName"`
	TotalRounds       int                `json:"totalRounds"`
	PlayerWins        int                `json:"playerWins"`
	DealerWins        int                `json:"dealerWins"`
	PlayerWinRate     float64            `json:"playerWinRate"`
	BetItemsIn        int                `json:"betItemsIn"`
	PayoutItemsOut    int                `json:"payoutItemsOut"`
	NetAgainstCasino  int                `json:"netAgainstCasino"`
	AverageNetPerGame float64            `json:"averageNetPerGame"`
	IsProfitable      bool               `json:"isProfitable"`
	ByGameRounds      map[string]int     `json:"byGameRounds"`
	ByGameWins        map[string]int     `json:"byGameWins"`
	ByGameLosses      map[string]int     `json:"byGameLosses"`
	ByGameWinRate     map[string]float64 `json:"byGameWinRate"`
}

type DailyStatsPoint struct {
	Day            string  `json:"day"`
	TotalRounds    int     `json:"totalRounds"`
	NetItems       int     `json:"netItems"`
	IssueRounds    int     `json:"issueRounds"`
	BetItemsIn     int     `json:"betItemsIn"`
	PayoutItemsOut int     `json:"payoutItemsOut"`
	RTPPercent     float64 `json:"rtpPercent"`
}

type TrendStats struct {
	Daily []DailyStatsPoint `json:"daily"`
}

type IssueStats struct {
	TotalIssues          int            `json:"totalIssues"`
	ByReason             map[string]int `json:"byReason"`
	GameChoiceTimeouts   int            `json:"gameChoiceTimeouts"`
	PayoutTimeouts       int            `json:"payoutTimeouts"`
	PayoutCancelFlags    int            `json:"payoutCancelFlags"`
	TradeConfirmTimeouts int            `json:"tradeConfirmTimeouts"`
}

func totalTradeItemQuantity(items []TradeItem) int {
	total := 0
	for _, it := range items {
		total += it.Quantity
	}
	return total
}

func tradeItemsToCountMap(items []TradeItem) map[string]int {
	m := map[string]int{}
	for _, it := range items {
		name := strings.TrimSpace(it.Name)
		if name == "" {
			continue
		}
		m[name] += it.Quantity
	}
	return m
}

func normalizeGameName(game string) string {
	g := strings.TrimSpace(strings.ToLower(game))
	if strings.Contains(g, "double") || g == "dt" || strings.Contains(g, "doubletrouble") {
		return "DT"
	}
	if strings.Contains(g, "tri") {
		return "Tri"
	}
	if strings.Contains(g, "pair") || strings.Contains(g, "pu") {
		return "Pair Up"
	}
	if strings.Contains(g, "uo") || strings.Contains(g, "under") || strings.Contains(g, "over") {
		return "UO"
	}
	switch g {
	case "poker", "pkr":
		return "Poker"
	case "21", "blackjack", "black jack":
		return "21"
	case "13", "thirteen":
		return "13"
	case "6", "six":
		return "6"
	case "h18":
		return "H18"
	case "bandit", "onearmbandit", "oab":
		return "Bandit"
	case "midhouse", "mh":
		return "MidHouse"
	default:
		if len(g) > 0 {
			return strings.Title(g)
		}
		return ""
	}
}

func isFinanciallyCountableRound(entry GameHistoryEntry) bool {
	// Relaxed check: if it's completed and has a winner, it's countable for win rates
	if entry.Issue {
		return false
	}
	g := normalizeGameName(entry.Game)
	if g == "" {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(entry.Status))
	if status == "completed" || status == "completed (keep)" || strings.TrimSpace(entry.Winner) != "" {
		return true
	}
	return false
}

func didPlayerWin(entry GameHistoryEntry) bool {
	if strings.TrimSpace(entry.Winner) == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(entry.Winner), strings.TrimSpace(entry.PlayerName))
}

func didDealerWin(entry GameHistoryEntry) bool {
	return strings.EqualFold(strings.TrimSpace(entry.Winner), "dealer")
}

func parseEntryTime(entry GameHistoryEntry) time.Time {
	var s string
	if entry.CompletedAt != "" {
		s = entry.CompletedAt
	} else if entry.UpdatedAt != "" {
		s = entry.UpdatedAt
	} else if entry.StartedAt != "" {
		s = entry.StartedAt
	}
	if s == "" {
		return time.Time{}
	}
	// Try RFC3339 first
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t
	}
	// Fallback to a common format if needed
	return time.Time{}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func itemStatusFromNet(net int) string {
	if net > 0 {
		return "Up"
	}
	if net < 0 {
		return "Down"
	}
	return "Even"
}

func unionItemNames(a map[string]int, b map[string]int) []string {
	namesMap := map[string]struct{}{}
	for k := range a {
		namesMap[k] = struct{}{}
	}
	for k := range b {
		namesMap[k] = struct{}{}
	}
	names := make([]string, 0, len(namesMap))
	for k := range namesMap {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// BuildCasinoStats computes statistics for the provided time range key.
// Supported rangeKey: all_time, today, last_7_days, last_30_days
func (a *App) BuildCasinoStats(rangeKey string) CasinoStats {
	stats := CasinoStats{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Range:       StatsRange{Key: rangeKey},
		Overall: CasinoStatsSummary{
			BetItemCounts:    make(map[string]int),
			PayoutItemCounts: make(map[string]int),
			NetItemCounts:    make(map[string]int),
		},
		ByGame: map[string]GameStats{
			"PU":       {Game: "PU"},
			"O7":       {Game: "O7"},
			"U7":       {Game: "U7"},
			"7":        {Game: "7"},
			"TriL":     {Game: "TriL"},
			"TriH":     {Game: "TriH"},
			"MidHouse": {Game: "MidHouse"},
			"UO":       {Game: "UO"},
			"Pair Up":  {Game: "Pair Up"},
		},
	}

	var start, end time.Time
	if rangeKey == "today" {
		now := time.Now()
		y, m, d := now.Date()
		loc := now.Location()
		start = time.Date(y, m, d, 0, 0, 0, 0, loc)
		end = start.Add(24 * time.Hour)
		stats.Range.StartAt = start.Format(time.RFC3339)
		stats.Range.EndAt = end.Format(time.RFC3339)
	}

	// Use DB if available for accurate stats across all history without OOM
	db, owner := a.getHistoryDB()
	if db != nil {
		// 1. Fetch Game History
		rows, err := db.Query(context.Background(), `
			SELECT
				player_name, started_at, updated_at, completed_at, game, winner, status, issue, notes, choice
			FROM game_history_entries
			WHERE owner_key = $1
		`, owner)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var e GameHistoryEntry
				var notesRaw []byte
				if err := rows.Scan(
					&e.PlayerName, &e.StartedAt, &e.UpdatedAt, &e.CompletedAt, &e.Game, &e.Winner, &e.Status, &e.Issue, &notesRaw, &e.Choice,
				); err != nil {
					continue
				}
				if len(notesRaw) > 0 {
					_ = json.Unmarshal(notesRaw, &e.Notes)
				}

				a.processStatsEntry(&stats, e, rangeKey, start, end)
			}
		}

		// 2. Fetch Item Ledger for "Ahead or Not" tracking
		lrows, err := db.Query(context.Background(), `
			SELECT trade_type, items, created_at
			FROM trade_ledger
			WHERE owner_key = $1
		`, owner)
		if err == nil {
			defer lrows.Close()
			itemStatsMap := make(map[string]*ItemStats)
			for lrows.Next() {
				var ttype string
				var itemsJSON []byte
				var created time.Time
				if err := lrows.Scan(&ttype, &itemsJSON, &created); err != nil {
					continue
				}

				if rangeKey == "today" {
					if created.Before(start) || created.After(end) {
						continue
					}
				}

				var items []TradeItem
				if err := json.Unmarshal(itemsJSON, &items); err != nil {
					continue
				}

				for _, it := range items {
					name := it.Name
					if name == "" {
						name = it.RawName
					}
					if name == "" {
						continue
					}

					qty := it.Quantity
					if qty == 0 {
						qty = it.Qty
					}

					is, ok := itemStatsMap[name]
					if !ok {
						is = &ItemStats{Name: name}
						itemStatsMap[name] = is
					}

					if ttype == "IN" {
						is.BetIn += qty
						stats.Overall.TotalBetItemsIn += qty
						stats.Overall.BetItemCounts[name] += qty
					} else {
						is.PayoutOut += qty
						stats.Overall.TotalPayoutItemsOut += qty
						stats.Overall.PayoutItemCounts[name] += qty
					}
				}
			}

			for _, is := range itemStatsMap {
				is.Net = is.BetIn - is.PayoutOut
				is.Status = itemStatusFromNet(is.Net)
				stats.ByItem = append(stats.ByItem, *is)
				stats.Overall.NetItemCounts[is.Name] = is.Net
			}
			stats.Overall.NetItems = stats.Overall.TotalBetItemsIn - stats.Overall.TotalPayoutItemsOut
		}

	} else {
		// Fallback to in-memory if DB is not available (though it will only be recent history)
		a.gameHistoryMu.Lock()
		for _, entry := range a.gameHistory {
			a.processStatsEntry(&stats, entry, rangeKey, start, end)
		}
		a.gameHistoryMu.Unlock()
	}

	// Compute win rates using only decided outcomes (player or dealer wins).
	totalDecided := stats.Overall.PlayerWins + stats.Overall.DealerWins
	if totalDecided > 0 {
		stats.Overall.PlayerWinRate = float64(stats.Overall.PlayerWins) / float64(totalDecided) * 100.0
		stats.Overall.DealerWinRate = float64(stats.Overall.DealerWins) / float64(totalDecided) * 100.0
		stats.Overall.CasinoEdgePercent = stats.Overall.DealerWinRate - stats.Overall.PlayerWinRate
	}
	for k, gs := range stats.ByGame {
		decided := gs.PlayerWins + gs.DealerWins
		if decided > 0 {
			gs.PlayerWinRate = float64(gs.PlayerWins) / float64(decided) * 100.0
			gs.DealerWinRate = float64(gs.DealerWins) / float64(decided) * 100.0
			gs.CasinoEdgePercent = gs.DealerWinRate - gs.PlayerWinRate
		}
		stats.ByGame[k] = gs
	}

	return stats
}

func (a *App) processStatsEntry(stats *CasinoStats, entry GameHistoryEntry, rangeKey string, start, end time.Time) {
	if !isFinanciallyCountableRound(entry) {
		return
	}
	t := parseEntryTime(entry)
	if rangeKey == "today" {
		if t.IsZero() || t.Before(start) || t.After(end) {
			return
		}
	}

	// Determine normalized game key
	g := normalizeGameName(entry.Game)
	if g == "UO" {
		choice := strings.ToLower(entry.Choice)
		if strings.Contains(choice, "over") {
			g = "O7"
		} else if strings.Contains(choice, "under") {
			g = "U7"
		} else if choice == "7" {
			g = "7"
		} else {
			g = "UO"
		}
	}

	if g == "Tri" {
		choice := strings.ToLower(entry.Choice)
		if strings.Contains(choice, "high") || strings.Contains(choice, "trih") {
			g = "TriH"
		} else if strings.Contains(choice, "low") || strings.Contains(choice, "tril") {
			g = "TriL"
		} else {
			g = "TriL"
		}
	}

	if g == "" {
		g = "Other"
	}

	if _, ok := stats.ByGame[g]; !ok {
		stats.ByGame[g] = GameStats{Game: g}
	}

	// Update overall counters
	stats.Overall.TotalRounds++
	stats.Overall.CompletedRounds++
	if entry.Issue {
		stats.Overall.IssueRounds++
	}
	if didPlayerWin(entry) {
		stats.Overall.PlayerWins++
	} else if didDealerWin(entry) {
		stats.Overall.DealerWins++
	}

	// Update per-game counters
	gs := stats.ByGame[g]
	gs.TotalRounds++
	gs.CompletedRounds++
	if entry.Issue {
		gs.IssueRounds++
	}
	if didPlayerWin(entry) {
		gs.PlayerWins++
	} else if didDealerWin(entry) {
		gs.DealerWins++
	}
	stats.ByGame[g] = gs
}

func (a *App) GetCasinoStatsJSON(rangeKey string) string {
	stats := a.BuildCasinoStats(rangeKey)
	b, err := json.Marshal(stats)
	if err != nil {
		return "{}"
	}
	return string(b)
}
