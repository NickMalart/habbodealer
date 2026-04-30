package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// RaffleContribution is a single recorded contribution (who, what, tickets, when)
type RaffleContribution struct {
	Player  string      `json:"player"`
	Items   []TradeItem `json:"items"`
	Tickets int         `json:"tickets"`
	At      string      `json:"at"`
}

type raffleState struct {
	Tickets       map[string]int       `json:"tickets"`
	Contributions []RaffleContribution `json:"contributions"`
	Active        bool                 `json:"active"`
	Name          string               `json:"name,omitempty"`
	PrizeName     string               `json:"prizeName,omitempty"`
	PrizeCount    int                  `json:"prizeCount,omitempty"`
	StartAt       string               `json:"startAt,omitempty"`
	EndAt         string               `json:"endAt,omitempty"`
}

type RaffleManager struct {
	mu            sync.Mutex
	tickets       map[string]int
	contributions []RaffleContribution
	path          string
	ctx           context.Context
	// raffle metadata / lifecycle
	Active     bool
	Name       string
	PrizeName  string
	PrizeCount int
	StartAt    string
	EndAt      string
	// archive of all raffles stored in the single JSON file at `path`
	history      []raffleState
	currentIndex int // index into history for the active/selected raffle, -1 if none
}

// Raffle is the package-global raffle manager instance (may be nil)
var Raffle *RaffleManager

// NewRaffleManager loads state from path (if present) and returns a manager.
func NewRaffleManager(path string, ctx context.Context) *RaffleManager {
	r := &RaffleManager{
		tickets:       make(map[string]int),
		contributions: make([]RaffleContribution, 0),
		path:          path,
		ctx:           ctx,
	}
	r.currentIndex = -1
	_ = r.load()
	return r
}

func (r *RaffleManager) load() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.path == "" {
		return nil
	}
	b, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			r.history = []raffleState{}
			r.currentIndex = -1
			return nil
		}
		return err
	}

	// Try to unmarshal as an array of raffles (preferred)
	var hist []raffleState
	if err := json.Unmarshal(b, &hist); err == nil && len(hist) > 0 {
		r.history = hist
		// choose the last active raffle if present, otherwise the last entry
		idx := -1
		for i := len(r.history) - 1; i >= 0; i-- {
			if r.history[i].Active {
				idx = i
				break
			}
		}
		if idx == -1 && len(r.history) > 0 {
			idx = len(r.history) - 1
		}
		r.currentIndex = idx
		if idx != -1 {
			s := r.history[idx]
			if s.Tickets != nil {
				r.tickets = s.Tickets
			} else {
				r.tickets = map[string]int{}
			}
			r.contributions = s.Contributions
			r.Active = s.Active
			r.Name = s.Name
			r.PrizeName = s.PrizeName
			r.PrizeCount = s.PrizeCount
			r.StartAt = s.StartAt
			r.EndAt = s.EndAt
		}
		return nil
	}

	// Fallback: try single raffleState for backward compatibility
	var single raffleState
	if err := json.Unmarshal(b, &single); err == nil {
		r.history = []raffleState{single}
		r.currentIndex = 0
		if single.Tickets != nil {
			r.tickets = single.Tickets
		} else {
			r.tickets = map[string]int{}
		}
		r.contributions = single.Contributions
		r.Active = single.Active
		r.Name = single.Name
		r.PrizeName = single.PrizeName
		r.PrizeCount = single.PrizeCount
		r.StartAt = single.StartAt
		r.EndAt = single.EndAt
		return nil
	}

	// Unparseable file; treat as empty archive to avoid data loss
	r.history = []raffleState{}
	r.currentIndex = -1
	return nil
}

func (r *RaffleManager) save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.path == "" {
		return nil
	}

	// Ensure history entry reflects current in-memory state
	cur := raffleState{
		Tickets:       r.tickets,
		Contributions: r.contributions,
		Active:        r.Active,
		Name:          r.Name,
		PrizeName:     r.PrizeName,
		PrizeCount:    r.PrizeCount,
		StartAt:       r.StartAt,
		EndAt:         r.EndAt,
	}

	if r.currentIndex < 0 || r.currentIndex >= len(r.history) {
		r.history = append(r.history, cur)
		r.currentIndex = len(r.history) - 1
	} else {
		r.history[r.currentIndex] = cur
	}

	b, err := json.MarshalIndent(r.history, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func (r *RaffleManager) computeTickets(items []TradeItem) int {
	coinValues := map[string]int{
		"cf_1_coin_bronze": 1,
		"cf_5_coin_silver": 5,
		"cf_10_coin_gold":  10,
		"cf_20_moneybag":   20,
		"cf_50_goldbar":    50,
	}

	totalCoins := 0
	nonCoinUnits := 0
	for _, it := range items {
		if it.Quantity <= 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(it.Name))
		if v, ok := coinValues[name]; ok {
			totalCoins += v * it.Quantity
		} else {
			nonCoinUnits += it.Quantity
		}
	}

	tickets := 0
	if totalCoins > 0 {
		tickets = totalCoins + (totalCoins / 5) // +1 ticket per 5 coins
	}
	tickets += nonCoinUnits // keep 1 ticket per non-coin unit

	return tickets
}

// RecordContribution records the player's contribution, returns tickets allocated.
func (r *RaffleManager) RecordContribution(player string, items []TradeItem) (int, error) {
	if player == "" {
		player = "Unknown"
	}
	player = normalizeUsername(player)

	// Only record when raffle is active
	r.mu.Lock()
	active := r.Active
	r.mu.Unlock()
	if !active {
		return 0, nil
	}

	tickets := r.computeTickets(items)
	if tickets <= 0 {
		return 0, nil
	}

	contrib := RaffleContribution{
		Player:  player,
		Items:   items,
		Tickets: tickets,
		At:      time.Now().Format(time.RFC3339),
	}

	r.mu.Lock()
	if r.tickets == nil {
		r.tickets = map[string]int{}
	}
	r.tickets[player] += tickets
	r.contributions = append(r.contributions, contrib)
	r.mu.Unlock()

	if err := r.save(); err != nil {
		return tickets, err
	}

	// Emit frontend event with the update
	if r.ctx != nil {
		payload, _ := json.Marshal(map[string]interface{}{
			"player":       player,
			"ticketsTotal": r.tickets[player],
			"contribution": contrib,
		})
		runtime.EventsEmit(r.ctx, "raffleUpdate", string(payload))
	}

	return tickets, nil
}

func (r *RaffleManager) GetTickets(player string) int {
	player = normalizeUsername(player)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tickets[player]
}

func (r *RaffleManager) AllTickets() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int, len(r.tickets))
	for k, v := range r.tickets {
		out[k] = v
	}
	return out
}

func (r *RaffleManager) ListContributions() []RaffleContribution {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RaffleContribution, len(r.contributions))
	copy(out, r.contributions)
	return out
}

// DrawWinner selects a weighted random winner (seed optional: pass 0 to use time).
func (r *RaffleManager) DrawWinner(seed int64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	total := 0
	for _, v := range r.tickets {
		total += v
	}
	if total == 0 {
		return "", fmt.Errorf("no tickets")
	}

	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rnd := rand.New(rand.NewSource(seed))
	target := rnd.Intn(total) + 1

	cum := 0
	for player, v := range r.tickets {
		cum += v
		if target <= cum {
			return player, nil
		}
	}
	return "", fmt.Errorf("draw failed")
}

func (r *RaffleManager) Reset() error {
	r.mu.Lock()
	// Start a new empty raffle entry (preserve history)
	newState := raffleState{
		Tickets:       map[string]int{},
		Contributions: []RaffleContribution{},
		Active:        false,
		Name:          "",
		PrizeName:     "",
		PrizeCount:    0,
		StartAt:       "",
		EndAt:         "",
	}
	r.history = append(r.history, newState)
	r.currentIndex = len(r.history) - 1
	r.tickets = newState.Tickets
	r.contributions = newState.Contributions
	r.Active = newState.Active
	r.Name = newState.Name
	r.PrizeName = newState.PrizeName
	r.PrizeCount = newState.PrizeCount
	r.StartAt = newState.StartAt
	r.EndAt = newState.EndAt
	r.mu.Unlock()
	if r.ctx != nil {
		payload, _ := json.Marshal(map[string]interface{}{"newRaffle": true})
		runtime.EventsEmit(r.ctx, "raffleUpdate", string(payload))
	}
	return r.save()
}

// Start begins a new raffle and appends it to the single-file archive.
func (r *RaffleManager) Start(name, prizeName string, prizeCount int) error {
	r.mu.Lock()
	r.tickets = map[string]int{}
	r.contributions = []RaffleContribution{}
	r.Active = true
	r.Name = strings.TrimSpace(name)
	r.PrizeName = strings.TrimSpace(prizeName)
	r.PrizeCount = prizeCount
	r.StartAt = time.Now().Format(time.RFC3339)
	r.EndAt = ""
	// append explicitly to history and set currentIndex
	s := raffleState{
		Tickets:       r.tickets,
		Contributions: r.contributions,
		Active:        r.Active,
		Name:          r.Name,
		PrizeName:     r.PrizeName,
		PrizeCount:    r.PrizeCount,
		StartAt:       r.StartAt,
		EndAt:         r.EndAt,
	}
	r.history = append(r.history, s)
	r.currentIndex = len(r.history) - 1
	r.mu.Unlock()

	if err := r.save(); err != nil {
		return err
	}
	if r.ctx != nil {
		payload, _ := json.Marshal(map[string]interface{}{
			"active":     r.Active,
			"name":       r.Name,
			"prizeName":  r.PrizeName,
			"prizeCount": r.PrizeCount,
			"startAt":    r.StartAt,
		})
		runtime.EventsEmit(r.ctx, "raffleUpdate", string(payload))
	}
	return nil
}

// Stop ends the active raffle (marks end time and persists into archive).
func (r *RaffleManager) Stop() error {
	r.mu.Lock()
	r.Active = false
	r.EndAt = time.Now().Format(time.RFC3339)
	r.mu.Unlock()

	if err := r.save(); err != nil {
		return err
	}
	if r.ctx != nil {
		payload, _ := json.Marshal(map[string]interface{}{
			"active":  r.Active,
			"endAt":   r.EndAt,
			"name":    r.Name,
			"prize":   r.PrizeName,
			"prizeCt": r.PrizeCount,
		})
		runtime.EventsEmit(r.ctx, "raffleUpdate", string(payload))
	}
	return nil
}

func (r *RaffleManager) IsActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Active
}

// Resume re-activates the currently selected raffle (does not alter history order).
func (r *RaffleManager) Resume() error {
	r.mu.Lock()
	if r.tickets == nil {
		r.tickets = map[string]int{}
	}
	r.Active = true
	if r.StartAt == "" {
		r.StartAt = time.Now().Format(time.RFC3339)
	}
	r.mu.Unlock()

	if err := r.save(); err != nil {
		return err
	}
	if r.ctx != nil {
		payload, _ := json.Marshal(map[string]interface{}{
			"active":     r.Active,
			"name":       r.Name,
			"prizeName":  r.PrizeName,
			"prizeCount": r.PrizeCount,
			"startAt":    r.StartAt,
		})
		runtime.EventsEmit(r.ctx, "raffleUpdate", string(payload))
	}
	return nil
}

// RaffleSummary is a compact metadata view for listing archived raffles.
type RaffleSummary struct {
	Index              int    `json:"index"`
	Name               string `json:"name"`
	PrizeName          string `json:"prizeName"`
	PrizeCount         int    `json:"prizeCount"`
	StartAt            string `json:"startAt"`
	EndAt              string `json:"endAt"`
	Active             bool   `json:"active"`
	TicketsTotal       int    `json:"ticketsTotal"`
	ContributionsCount int    `json:"contributionsCount"`
}

// ListArchivedRaffles returns a slice of summaries for all raffles in the archive.
func (r *RaffleManager) ListArchivedRaffles() []RaffleSummary {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RaffleSummary, 0, len(r.history))
	for i, s := range r.history {
		total := 0
		for _, v := range s.Tickets {
			total += v
		}
		out = append(out, RaffleSummary{
			Index:              i,
			Name:               s.Name,
			PrizeName:          s.PrizeName,
			PrizeCount:         s.PrizeCount,
			StartAt:            s.StartAt,
			EndAt:              s.EndAt,
			Active:             s.Active,
			TicketsTotal:       total,
			ContributionsCount: len(s.Contributions),
		})
	}
	return out
}

// LoadArchivedRaffle selects the raffle at index and loads it into memory as current.
func (r *RaffleManager) LoadArchivedRaffle(index int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index < 0 || index >= len(r.history) {
		return fmt.Errorf("index out of range")
	}
	s := r.history[index]
	r.currentIndex = index
	if s.Tickets != nil {
		r.tickets = s.Tickets
	} else {
		r.tickets = map[string]int{}
	}
	r.contributions = s.Contributions
	r.Active = s.Active
	r.Name = s.Name
	r.PrizeName = s.PrizeName
	r.PrizeCount = s.PrizeCount
	r.StartAt = s.StartAt
	r.EndAt = s.EndAt

	// persist selection
	return r.save()
}

// DeleteArchivedRaffle removes the raffle at index from the archive and persists.
func (r *RaffleManager) DeleteArchivedRaffle(index int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index < 0 || index >= len(r.history) {
		return fmt.Errorf("index out of range")
	}
	r.history = append(r.history[:index], r.history[index+1:]...)
	// adjust currentIndex
	if r.currentIndex == index {
		r.currentIndex = -1
		r.tickets = map[string]int{}
		r.contributions = []RaffleContribution{}
		r.Active = false
		r.Name = ""
		r.PrizeName = ""
		r.PrizeCount = 0
		r.StartAt = ""
		r.EndAt = ""
	} else if r.currentIndex > index {
		r.currentIndex--
	}
	return r.save()
}
