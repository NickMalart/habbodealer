package main

import "testing"

func TestPoolConfigSimpleProtocolQueryMode(t *testing.T) {
	cfg, err := newPGXPoolConfig("postgresql://user:pass@host/db?sslmode=require")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if got := cfg.ConnConfig.RuntimeParams["default_query_exec_mode"]; got != "simple_protocol" {
		t.Fatalf("default_query_exec_mode = %q; want %q", got, "simple_protocol")
	}
}
