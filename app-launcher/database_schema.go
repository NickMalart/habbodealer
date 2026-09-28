package main

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed database_schema.sql
var databaseSchema string

// ensureWorkspaceSchema applies the canonical schema shared by the workspace's
// database-backed applications. It is deliberately idempotent so it can run on
// both an empty database and an existing one.
func ensureWorkspaceSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema transaction: %w", err)
	}
	defer tx.Rollback()

	for _, statement := range strings.Split(databaseSchema, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("execute schema statement %q: %w", firstSQLLine(statement), err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema transaction: %w", err)
	}
	return nil
}

func firstSQLLine(statement string) string {
	line := strings.SplitN(statement, "\n", 2)[0]
	if len(line) > 100 {
		return line[:100] + "..."
	}
	return line
}
