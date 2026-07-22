package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDatabaseURLFromConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "db.local.json")
	configContent := []byte(`{"databaseUrl":"postgresql://example/db"}`)
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := readDatabaseURL(tempDir)
	if err != nil {
		t.Fatalf("readDatabaseURL returned error: %v", err)
	}
	if got != "postgresql://example/db" {
		t.Fatalf("readDatabaseURL returned %q, want %q", got, "postgresql://example/db")
	}
}
