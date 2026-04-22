package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type PokerHandCategory int

const (
	PokerNothing PokerHandCategory = iota
	PokerOnePair
	PokerTwoPair
	PokerThreeOfAKind
	PokerLowStraight
	PokerHighStraight
	PokerFullHouse
	PokerFourOfAKind
	PokerFiveOfAKind
)

type PokerWinner int

const (
	PokerWinnerDealer PokerWinner = iota
	PokerWinnerPlayer
)

type PokerHandResult struct {
	Category  PokerHandCategory
	Tiebreaks []int
}

func defaultPokerDisplayConfig() *PokerDisplayConfig {
	return &PokerDisplayConfig{
		FiveOfAKind:  "Five of a kind: %s",
		FourOfAKind:  "Four of a kind: %s",
		FullHouse:    "Full House: %s",
		HighStraight: "High Str8",
		LowStraight:  "Low Str8",
		ThreeOfAKind: "Three of a kind: %s",
		TwoPair:      "Two Pair: %ss",
		OnePair:      "One Pair: %ss",
		Nothing:      "Nothing",
	}
}

func formatPokerHandResult(config *PokerDisplayConfig, result PokerHandResult) string {
	if config == nil {
		config = defaultPokerDisplayConfig()
	}

	switch result.Category {
	case PokerFiveOfAKind:
		return fmt.Sprintf(config.FiveOfAKind, strconv.Itoa(firstTiebreak(result)))
	case PokerFourOfAKind:
		return fmt.Sprintf(config.FourOfAKind, strconv.Itoa(firstTiebreak(result)))
	case PokerFullHouse:
		return fmt.Sprintf(config.FullHouse, joinPokerInts(result.Tiebreaks[:min(2, len(result.Tiebreaks))]))
	case PokerHighStraight:
		return fmt.Sprintf(config.HighStraight)
	case PokerLowStraight:
		return fmt.Sprintf(config.LowStraight)
	case PokerThreeOfAKind:
		return fmt.Sprintf(config.ThreeOfAKind, strconv.Itoa(firstTiebreak(result)))
	case PokerTwoPair:
		return fmt.Sprintf(config.TwoPair, joinPokerInts(result.Tiebreaks[:min(2, len(result.Tiebreaks))]))
	case PokerOnePair:
		return fmt.Sprintf(config.OnePair, strconv.Itoa(firstTiebreak(result)))
	default:
		return fmt.Sprintf(config.Nothing)
	}
}

func evaluatePokerRules(dices []*Dice) PokerHandResult {
	values := make([]int, 0, len(dices))
	for _, dice := range dices {
		if dice == nil {
			continue
		}
		values = append(values, dice.Value)
	}

	return classifyPokerHand(values)
}

func comparePokerHands(player PokerHandResult, dealer PokerHandResult) PokerWinner {
	if player.Category > dealer.Category {
		return PokerWinnerPlayer
	}
	if player.Category < dealer.Category {
		return PokerWinnerDealer
	}
	// Same category: compare tiebreaks
	cmp := comparePokerTiebreaks(player.Tiebreaks, dealer.Tiebreaks)
	if cmp > 0 {
		return PokerWinnerPlayer
	}
	// Dealer wins ties (exact tie) or when dealer tiebreaks are higher
	return PokerWinnerDealer
}

func classifyPokerHand(values []int) PokerHandResult {
	if len(values) != 5 {
		return PokerHandResult{Category: PokerNothing}
	}

	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	desc := append([]int(nil), sorted...)
	sort.Slice(desc, func(i, j int) bool { return desc[i] > desc[j] })

	if sorted[0] == 1 && sorted[1] == 2 && sorted[2] == 3 && sorted[3] == 4 && sorted[4] == 5 {
		return PokerHandResult{Category: PokerLowStraight, Tiebreaks: []int{5}}
	}
	if sorted[0] == 2 && sorted[1] == 3 && sorted[2] == 4 && sorted[3] == 5 && sorted[4] == 6 {
		return PokerHandResult{Category: PokerHighStraight, Tiebreaks: []int{6}}
	}

	counts := map[int]int{}
	for _, value := range sorted {
		counts[value]++
	}

	countValues := map[int][]int{}
	hasThree := false
	pairCount := 0
	for value, count := range counts {
		countValues[count] = append(countValues[count], value)
		switch count {
		case 5:
			return PokerHandResult{Category: PokerFiveOfAKind, Tiebreaks: []int{value}}
		case 4:
			// resolved after all counts are collected
		case 3:
			hasThree = true
		case 2:
			pairCount++
		}
	}

	sort.Sort(sort.Reverse(sort.IntSlice(countValues[3])))
	sort.Sort(sort.Reverse(sort.IntSlice(countValues[2])))
	sort.Sort(sort.Reverse(sort.IntSlice(countValues[1])))
	sort.Sort(sort.Reverse(sort.IntSlice(countValues[4])))

	if len(countValues[4]) == 1 {
		return PokerHandResult{Category: PokerFourOfAKind, Tiebreaks: []int{countValues[4][0], firstValueDescending(countValues[1])}}
	}
	if hasThree && pairCount == 1 {
		return PokerHandResult{Category: PokerFullHouse, Tiebreaks: []int{firstValueDescending(countValues[3]), firstValueDescending(countValues[2])}}
	}
	if hasThree {
		tiebreaks := []int{firstValueDescending(countValues[3])}
		tiebreaks = append(tiebreaks, countValues[1]...)
		return PokerHandResult{Category: PokerThreeOfAKind, Tiebreaks: tiebreaks}
	}
	if pairCount == 2 {
		tiebreaks := append([]int(nil), countValues[2]...)
		tiebreaks = append(tiebreaks, firstValueDescending(countValues[1]))
		return PokerHandResult{Category: PokerTwoPair, Tiebreaks: tiebreaks}
	}
	if pairCount == 1 {
		tiebreaks := []int{firstValueDescending(countValues[2])}
		tiebreaks = append(tiebreaks, countValues[1]...)
		return PokerHandResult{Category: PokerOnePair, Tiebreaks: tiebreaks}
	}

	return PokerHandResult{Category: PokerNothing, Tiebreaks: desc}
}

func comparePokerTiebreaks(player []int, dealer []int) int {
	limit := len(player)
	if len(dealer) < limit {
		limit = len(dealer)
	}
	for i := 0; i < limit; i++ {
		if player[i] > dealer[i] {
			return 1
		}
		if dealer[i] > player[i] {
			return -1
		}
	}
	// Ignore any additional kickers beyond the compared prefix.
	// Treat equal prefixes as an exact tie (dealer wins ties).
	return 0
}

func firstValueDescending(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return values[0]
}

func joinPokerInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, "")
}

func firstTiebreak(result PokerHandResult) int {
	if len(result.Tiebreaks) == 0 {
		return 0
	}
	return result.Tiebreaks[0]
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
