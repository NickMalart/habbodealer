package main

import "testing"

func TestComparePokerHands(t *testing.T) {
	tests := []struct {
		name     string
		player   PokerHandResult
		dealer   PokerHandResult
		expected PokerWinner
	}{
		{
			name: "Player higher category wins",
			player: PokerHandResult{
				Category:  PokerOnePair,
				Tiebreaks: []int{2, 6, 5, 4},
			},
			dealer: PokerHandResult{
				Category:  PokerNothing,
				Tiebreaks: []int{6, 5, 4, 3, 2},
			},
			expected: PokerWinnerPlayer,
		},
		{
			name: "Dealer higher category wins",
			player: PokerHandResult{
				Category:  PokerNothing,
				Tiebreaks: []int{6, 5, 4, 3, 2},
			},
			dealer: PokerHandResult{
				Category:  PokerOnePair,
				Tiebreaks: []int{2, 6, 5, 4},
			},
			expected: PokerWinnerDealer,
		},
		{
			name: "Player higher pair wins (The reported bug)",
			player: PokerHandResult{
				Category:  PokerOnePair,
				Tiebreaks: []int{6, 5, 4, 3},
			},
			dealer: PokerHandResult{
				Category:  PokerOnePair,
				Tiebreaks: []int{1, 6, 5, 4},
			},
			expected: PokerWinnerPlayer,
		},
		{
			name: "Same pair, dealer wins (ignoring kickers)",
			player: PokerHandResult{
				Category:  PokerOnePair,
				Tiebreaks: []int{6, 5, 4, 3},
			},
			dealer: PokerHandResult{
				Category:  PokerOnePair,
				Tiebreaks: []int{6, 2, 2, 2}, // Dealer has worse kickers but same pair
			},
			expected: PokerWinnerDealer,
		},
		{
			name: "Same pair, dealer wins even if player has better kickers",
			player: PokerHandResult{
				Category:  PokerOnePair,
				Tiebreaks: []int{6, 5, 4, 3}, // Better kickers
			},
			dealer: PokerHandResult{
				Category:  PokerOnePair,
				Tiebreaks: []int{6, 2, 2, 2}, 
			},
			expected: PokerWinnerDealer,
		},
		{
			name: "Player higher two pair wins",
			player: PokerHandResult{
				Category:  PokerTwoPair,
				Tiebreaks: []int{6, 2, 5},
			},
			dealer: PokerHandResult{
				Category:  PokerTwoPair,
				Tiebreaks: []int{5, 4, 6},
			},
			expected: PokerWinnerPlayer,
		},
		{
			name: "Player higher second pair wins",
			player: PokerHandResult{
				Category:  PokerTwoPair,
				Tiebreaks: []int{6, 4, 2},
			},
			dealer: PokerHandResult{
				Category:  PokerTwoPair,
				Tiebreaks: []int{6, 3, 5},
			},
			expected: PokerWinnerPlayer,
		},
		{
			name: "Nothing category: player higher card wins",
			player: PokerHandResult{
				Category:  PokerNothing,
				Tiebreaks: []int{6, 4, 3, 2, 1},
			},
			dealer: PokerHandResult{
				Category:  PokerNothing,
				Tiebreaks: []int{5, 4, 3, 2, 1},
			},
			expected: PokerWinnerPlayer,
		},
		{
			name: "Nothing category: same high card, dealer wins (ignoring kickers)",
			player: PokerHandResult{
				Category:  PokerNothing,
				Tiebreaks: []int{6, 5, 4, 3, 1},
			},
			dealer: PokerHandResult{
				Category:  PokerNothing,
				Tiebreaks: []int{6, 2, 2, 2, 2},
			},
			expected: PokerWinnerDealer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := comparePokerHands(tt.player, tt.dealer)
			if actual != tt.expected {
				t.Errorf("got %v, want %v", actual, tt.expected)
			}
		})
	}
}
