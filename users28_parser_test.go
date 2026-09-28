package main

import "testing"

func TestRestoreDroppedUsernamePrefix(t *testing.T) {
	if got := restoreDroppedUsernamePrefix("prefixCequils", "equils"); got != "Cequils" {
		t.Fatalf("restoreDroppedUsernamePrefix() = %q, want %q", got, "Cequils")
	}

	if got := restoreDroppedUsernamePrefix("prefixcequils", "cequils"); got != "cequils" {
		t.Fatalf("restoreDroppedUsernamePrefix() changed a lowercase name to %q", got)
	}
}

func TestNormalizeUsers28NameRemovesProtocolPrefix(t *testing.T) {
	tests := map[string]string{
		"JCequils":       "Cequils",
		"]@NiArab":       "iArab",
		"RScizMDubbo":    "Dubbo",
		"8-Bit":          "8-Bit",
		"normalUsername": "normalUsername",
	}

	for input, want := range tests {
		if got := normalizeUsers28Name(input, ""); got != want {
			t.Errorf("normalizeUsers28Name(%q) = %q, want %q", input, got, want)
		}
	}
}