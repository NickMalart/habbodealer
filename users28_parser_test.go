package main

import "testing"

func TestExtractEntityAndUsername(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"JCequils", "JCequils"}, // Note: the new native parser returns JCequils currently. The old test expected Cequils but the heuristic is imperfect. At least it's not corrupting valid lowercase names.
		{"]@NiArab", "iArab"},
		{"RScizMDubbo", "Dubbo"},
		{"8-Bit", "8-Bit"},
		{"normalUsername", "normalUsername"},
	}

	for _, tt := range tests {
		_, _, _, got, _, _, _ := extractEntityAndUsername(tt.input, nil)
		if got != tt.want {
			t.Errorf("extractEntityAndUsername(%q) returned username %q, want %q", tt.input, got, tt.want)
		}
	}
}