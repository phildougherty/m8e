// internal/memory/migrations_test.go
package memory

import (
	"strings"
	"testing"
)

// TestOrderedMigrations verifies that orderedMigrations sorts by ascending
// version and does not mutate the caller's slice.
func TestOrderedMigrations(t *testing.T) {
	input := []migration{
		{Version: 3, Name: "c"},
		{Version: 1, Name: "a"},
		{Version: 2, Name: "b"},
	}

	got := orderedMigrations(input)

	wantOrder := []int{1, 2, 3}
	if len(got) != len(wantOrder) {
		t.Fatalf("expected %d migrations, got %d", len(wantOrder), len(got))
	}
	for i, want := range wantOrder {
		if got[i].Version != want {
			t.Errorf("position %d: expected version %d, got %d", i, want, got[i].Version)
		}
	}

	// Source slice must be untouched.
	if input[0].Version != 3 {
		t.Errorf("orderedMigrations mutated the input slice: input[0].Version = %d", input[0].Version)
	}
}

// TestValidateMigrations covers well-formed and malformed migration sets.
func TestValidateMigrations(t *testing.T) {
	tests := []struct {
		name       string
		migrations []migration
		wantErr    bool
	}{
		{
			name:       "empty set is valid",
			migrations: nil,
			wantErr:    false,
		},
		{
			name: "strictly increasing is valid",
			migrations: []migration{
				{Version: 1, Name: "a"},
				{Version: 2, Name: "b"},
				{Version: 5, Name: "c"},
			},
			wantErr: false,
		},
		{
			name: "out of order is still valid after sort",
			migrations: []migration{
				{Version: 3, Name: "c"},
				{Version: 1, Name: "a"},
			},
			wantErr: false,
		},
		{
			name: "duplicate version is invalid",
			migrations: []migration{
				{Version: 1, Name: "a"},
				{Version: 1, Name: "b"},
			},
			wantErr: true,
		},
		{
			name: "zero version is invalid",
			migrations: []migration{
				{Version: 0, Name: "a"},
			},
			wantErr: true,
		},
		{
			name: "negative version is invalid",
			migrations: []migration{
				{Version: -1, Name: "a"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMigrations(tt.migrations)
			if tt.wantErr && err == nil {
				t.Errorf("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestPackagedMigrationsAreValid guards the real migration set shipped with the
// package: it must always validate, otherwise startup would fail.
func TestPackagedMigrationsAreValid(t *testing.T) {
	if err := validateMigrations(schemaMigrations); err != nil {
		t.Fatalf("packaged schemaMigrations is invalid: %v", err)
	}
	if len(schemaMigrations) == 0 {
		t.Fatal("expected at least one packaged migration")
	}
	for _, m := range schemaMigrations {
		if strings.TrimSpace(m.SQL) == "" {
			t.Errorf("migration %d (%s) has empty SQL", m.Version, m.Name)
		}
		if strings.TrimSpace(m.Name) == "" {
			t.Errorf("migration version %d has empty name", m.Version)
		}
	}
}

// TestPendingMigrations verifies the runner only selects migrations newer than
// the already-applied version, which is the core of idempotency.
func TestPendingMigrations(t *testing.T) {
	migrations := []migration{
		{Version: 1, Name: "a"},
		{Version: 2, Name: "b"},
		{Version: 3, Name: "c"},
	}

	tests := []struct {
		name     string
		applied  int
		wantVers []int
	}{
		{name: "nothing applied yet", applied: 0, wantVers: []int{1, 2, 3}},
		{name: "partially applied", applied: 1, wantVers: []int{2, 3}},
		{name: "fully applied is idempotent", applied: 3, wantVers: []int{}},
		{name: "applied beyond known set", applied: 99, wantVers: []int{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pendingMigrations(migrations, tt.applied)
			if len(got) != len(tt.wantVers) {
				t.Fatalf("expected %d pending, got %d", len(tt.wantVers), len(got))
			}
			for i, want := range tt.wantVers {
				if got[i].Version != want {
					t.Errorf("position %d: expected version %d, got %d", i, want, got[i].Version)
				}
			}
		})
	}
}

// TestPendingMigrationsSortsBeforeFiltering ensures an unsorted source slice
// still yields correctly ordered pending migrations.
func TestPendingMigrationsSortsBeforeFiltering(t *testing.T) {
	migrations := []migration{
		{Version: 3, Name: "c"},
		{Version: 1, Name: "a"},
		{Version: 2, Name: "b"},
	}

	got := pendingMigrations(migrations, 1)
	if len(got) != 2 || got[0].Version != 2 || got[1].Version != 3 {
		t.Fatalf("expected [2 3], got %v", versions(got))
	}
}

func versions(migrations []migration) []int {
	out := make([]int, len(migrations))
	for i, m := range migrations {
		out[i] = m.Version
	}

	return out
}
