package main

import "testing"

func TestStockedItemsSchemaStatementsIncludeCurrentColumns(t *testing.T) {
	got := stockedItemsSchemaStatements()
	if len(got) == 0 {
		t.Fatal("expected migration statements for stocked_items")
	}
	checks := []string{
		"ALTER TABLE public.stocked_items ADD COLUMN IF NOT EXISTS owner_key TEXT NOT NULL DEFAULT '';",
		"ALTER TABLE public.stocked_items ADD COLUMN IF NOT EXISTS canonical_name TEXT NOT NULL DEFAULT '';",
		"ALTER TABLE public.stocked_items ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';",
		"ALTER TABLE public.stocked_items ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;",
		"CREATE UNIQUE INDEX IF NOT EXISTS stocked_items_owner_raw_name_idx ON public.stocked_items (owner_key, raw_name);",
	}
	for _, want := range checks {
		found := false
		for _, stmt := range got {
			if stmt == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing stocked_items migration statement: %s\nstatements=%v", want, got)
		}
	}
}
