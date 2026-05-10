package main

import (
	"testing"
)

func TestPickWinner(t *testing.T) {
	app := NewApp()
	app.users = []string{"Alice", "Bob", "Charlie", "Dave"}

	t.Run("Pick from all", func(t *testing.T) {
		winner := app.PickWinner([]string{})
		if winner == "" {
			t.Error("Expected a winner, got empty string")
		}
		found := false
		for _, u := range app.users {
			if u == winner {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Winner %s not in user list", winner)
		}
	})

	t.Run("Pick with blocklist", func(t *testing.T) {
		blocked := []string{"Alice", "Bob"}
		// Pick many times to be statistically sure
		for i := 0; i < 100; i++ {
			winner := app.PickWinner(blocked)
			if winner == "Alice" || winner == "Bob" {
				t.Errorf("Picked blocked user: %s", winner)
			}
		}
	})

	t.Run("Pick with full blocklist", func(t *testing.T) {
		blocked := []string{"Alice", "Bob", "Charlie", "Dave"}
		winner := app.PickWinner(blocked)
		if winner != "" {
			t.Errorf("Expected no winner with full blocklist, got %s", winner)
		}
	})

	t.Run("Pick with empty user list", func(t *testing.T) {
		app.users = []string{}
		winner := app.PickWinner([]string{})
		if winner != "" {
			t.Errorf("Expected no winner with empty user list, got %s", winner)
		}
	})
}
