// internal/memory/memory_store_test.go
package memory

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/go-logr/logr"
)

// --- Pure unit tests (no database required) ---

// TestInClause checks the parameterized IN-clause builder used by every
// multi-name query. The values must come back as bound args, never inlined.
func TestInClause(t *testing.T) {
	tests := []struct {
		name             string
		values           []string
		startIndex       int
		wantPlaceholders string
		wantArgs         []string
	}{
		{
			name:             "single value from 1",
			values:           []string{"alice"},
			startIndex:       1,
			wantPlaceholders: "$1",
			wantArgs:         []string{"alice"},
		},
		{
			name:             "multiple values from 1",
			values:           []string{"alice", "bob", "carol"},
			startIndex:       1,
			wantPlaceholders: "$1,$2,$3",
			wantArgs:         []string{"alice", "bob", "carol"},
		},
		{
			name:             "offset start index for second bind group",
			values:           []string{"x", "y"},
			startIndex:       4,
			wantPlaceholders: "$4,$5",
			wantArgs:         []string{"x", "y"},
		},
		{
			name:             "empty values",
			values:           []string{},
			startIndex:       1,
			wantPlaceholders: "",
			wantArgs:         []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			placeholders, args := inClause(tt.values, tt.startIndex)
			if placeholders != tt.wantPlaceholders {
				t.Errorf("placeholders: got %q, want %q", placeholders, tt.wantPlaceholders)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args length: got %d, want %d", len(args), len(tt.wantArgs))
			}
			for i, want := range tt.wantArgs {
				got, ok := args[i].(string)
				if !ok {
					t.Fatalf("arg %d is not a string: %T", i, args[i])
				}
				if got != want {
					t.Errorf("arg %d: got %q, want %q", i, got, want)
				}
			}
		})
	}
}

// TestInClauseTwoGroupsDoNotCollide verifies the numbering scheme used by
// GetRelationsForEntities: a second IN group starting after the first must use
// non-overlapping placeholder numbers.
func TestInClauseTwoGroupsDoNotCollide(t *testing.T) {
	names := []string{"a", "b"}
	first, _ := inClause(names, 1)
	second, _ := inClause(names, 1+len(names))

	if first != "$1,$2" {
		t.Errorf("first group: got %q, want $1,$2", first)
	}
	if second != "$3,$4" {
		t.Errorf("second group: got %q, want $3,$4", second)
	}
	if strings.ContainsAny(second, "$1$2") && second != "$3,$4" {
		t.Errorf("placeholder groups collide: %q and %q", first, second)
	}
}

// TestSearchNodesQueryShape asserts the structural properties of the full-text
// search statement: it is parameterized (no string interpolation of the user
// query), uses plainto_tsquery for safe tokenization, and is bounded by a
// LIMIT placeholder.
func TestSearchNodesQueryShape(t *testing.T) {
	if !strings.Contains(searchNodesQuery, "plainto_tsquery('english', $1)") {
		t.Error("search query should tokenize the user query via plainto_tsquery with bind param $1")
	}
	if !strings.Contains(searchNodesQuery, "to_tsvector") {
		t.Error("search query should use to_tsvector for full-text matching")
	}
	if !strings.Contains(searchNodesQuery, "LIMIT $2") {
		t.Error("search query should bound results with a LIMIT bind parameter")
	}
	if strings.Contains(searchNodesQuery, "%s") || strings.Contains(searchNodesQuery, "%v") {
		t.Error("search query must not contain format verbs (would imply string interpolation)")
	}
	// Both the entity branch and observation branch must be present, otherwise
	// search would silently ignore one source.
	if !strings.Contains(searchNodesQuery, "entity_matches") {
		t.Error("search query missing entity_matches CTE")
	}
	if !strings.Contains(searchNodesQuery, "observation_matches") {
		t.Error("search query missing observation_matches CTE")
	}
}

// TestDefaultSearchLimitPositive guards against a zero/negative limit that
// would make search return nothing.
func TestDefaultSearchLimitPositive(t *testing.T) {
	if defaultSearchLimit <= 0 {
		t.Fatalf("defaultSearchLimit must be positive, got %d", defaultSearchLimit)
	}
}

// TestConnectionPoolDefaultsSane checks the pool constants are internally
// consistent before they are ever applied to a real *sql.DB.
func TestConnectionPoolDefaultsSane(t *testing.T) {
	if defaultMaxOpenConns <= 0 {
		t.Errorf("defaultMaxOpenConns must be positive, got %d", defaultMaxOpenConns)
	}
	if defaultMaxIdleConns <= 0 {
		t.Errorf("defaultMaxIdleConns must be positive, got %d", defaultMaxIdleConns)
	}
	if defaultMaxIdleConns > defaultMaxOpenConns {
		t.Errorf("defaultMaxIdleConns (%d) must not exceed defaultMaxOpenConns (%d)",
			defaultMaxIdleConns, defaultMaxOpenConns)
	}
	if defaultConnMaxLifetime <= 0 {
		t.Errorf("defaultConnMaxLifetime must be positive, got %v", defaultConnMaxLifetime)
	}
}

// TestEntityJSONRoundTrip verifies the Entity model marshals to the MCP-facing
// JSON shape (camelCase keys) and unmarshals back losslessly.
func TestEntityJSONRoundTrip(t *testing.T) {
	in := Entity{
		Name:         "Ada Lovelace",
		EntityType:   "person",
		Observations: []string{"wrote the first algorithm", "worked with Babbage"},
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// MCP clients expect camelCase keys.
	for _, key := range []string{`"name"`, `"entityType"`, `"observations"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("marshalled entity missing key %s: %s", key, data)
		}
	}

	var out Entity
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Name != in.Name || out.EntityType != in.EntityType {
		t.Errorf("round trip mismatch: got %+v, want %+v", out, in)
	}
	if len(out.Observations) != len(in.Observations) {
		t.Errorf("observations length mismatch: got %d, want %d", len(out.Observations), len(in.Observations))
	}
}

// TestRelationJSONRoundTrip verifies the Relation model's JSON shape.
func TestRelationJSONRoundTrip(t *testing.T) {
	in := Relation{From: "Ada Lovelace", To: "Charles Babbage", RelationType: "collaborated_with"}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"from"`, `"to"`, `"relationType"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("marshalled relation missing key %s: %s", key, data)
		}
	}

	var out Relation
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch: got %+v, want %+v", out, in)
	}
}

// TestKnowledgeGraphJSONShape ensures a graph serializes with both collections
// present, so a read_graph response always carries entities and relations keys.
func TestKnowledgeGraphJSONShape(t *testing.T) {
	g := KnowledgeGraph{
		Entities:  []Entity{{Name: "n1", EntityType: "concept"}},
		Relations: []Relation{{From: "n1", To: "n1", RelationType: "self"}},
	}
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"entities"`) || !strings.Contains(string(data), `"relations"`) {
		t.Errorf("knowledge graph JSON missing required keys: %s", data)
	}
}

// --- Integration tests (require a live PostgreSQL) ---
//
// These run only when MEMORY_TEST_DSN is set, e.g.:
//   MEMORY_TEST_DSN='postgres://user:pass@localhost:5432/memory_test?sslmode=disable'
// They exercise the migration runner and the full CRUD/search/traversal paths
// against a real database. Without the env var they t.Skip cleanly so the unit
// suite still passes in CI environments with no database.

func testStore(t *testing.T) *MemoryStore {
	t.Helper()
	dsn := os.Getenv("MEMORY_TEST_DSN")
	if dsn == "" {
		t.Skip("MEMORY_TEST_DSN not set; skipping live-database test")
	}

	store, err := NewMemoryStore(dsn, logr.Discard())
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort cleanup so repeated runs start clean. Entities cascade
		// to observations and relations.
		if _, err := store.db.Exec("DELETE FROM entities"); err != nil {
			t.Logf("cleanup failed: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Logf("close failed: %v", err)
		}
	})

	// Start from a known-empty state.
	if _, err := store.db.Exec("DELETE FROM entities"); err != nil {
		t.Fatalf("failed to clear test database: %v", err)
	}

	return store
}

// TestMigrationsApplied confirms NewMemoryStore brought the schema fully up to
// date and that re-running the migration runner is idempotent.
func TestMigrationsApplied(t *testing.T) {
	store := testStore(t)

	version, err := store.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	want := orderedMigrations(schemaMigrations)[len(schemaMigrations)-1].Version
	if version != want {
		t.Errorf("schema version: got %d, want %d", version, want)
	}

	// Running again must not error or change the version.
	if err := runMigrations(store.db, schemaMigrations); err != nil {
		t.Fatalf("re-running migrations should be idempotent: %v", err)
	}
	again, err := store.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion after rerun: %v", err)
	}
	if again != want {
		t.Errorf("schema version changed after idempotent rerun: got %d, want %d", again, want)
	}
}

// TestCreateAndReadGraph exercises the core write+read path including
// observations and relation traversal.
func TestCreateAndReadGraph(t *testing.T) {
	store := testStore(t)

	entities := []Entity{
		{Name: "Ada", EntityType: "person", Observations: []string{"mathematician"}},
		{Name: "Babbage", EntityType: "person", Observations: []string{"engineer"}},
	}
	if err := store.CreateEntities(entities); err != nil {
		t.Fatalf("CreateEntities: %v", err)
	}

	// Idempotent: re-creating with the same observation must not duplicate it.
	if err := store.CreateEntities(entities); err != nil {
		t.Fatalf("CreateEntities (rerun): %v", err)
	}

	if err := store.CreateRelations([]Relation{
		{From: "Ada", To: "Babbage", RelationType: "collaborated_with"},
	}); err != nil {
		t.Fatalf("CreateRelations: %v", err)
	}
	// Duplicate relation insert must be a harmless no-op.
	if err := store.CreateRelations([]Relation{
		{From: "Ada", To: "Babbage", RelationType: "collaborated_with"},
	}); err != nil {
		t.Fatalf("CreateRelations (duplicate): %v", err)
	}

	graph, err := store.ReadGraph()
	if err != nil {
		t.Fatalf("ReadGraph: %v", err)
	}
	if len(graph.Entities) != 2 {
		t.Errorf("expected 2 entities, got %d", len(graph.Entities))
	}
	if len(graph.Relations) != 1 {
		t.Errorf("expected 1 relation (dedup enforced), got %d", len(graph.Relations))
	}
	for _, e := range graph.Entities {
		if len(e.Observations) != 1 {
			t.Errorf("entity %s: expected 1 observation (dedup enforced), got %d", e.Name, len(e.Observations))
		}
	}

	relations, err := store.GetRelationsForEntities([]string{"Ada"})
	if err != nil {
		t.Fatalf("GetRelationsForEntities: %v", err)
	}
	if len(relations) != 1 {
		t.Fatalf("expected 1 relation touching Ada, got %d", len(relations))
	}
	if relations[0].From != "Ada" || relations[0].To != "Babbage" {
		t.Errorf("unexpected relation: %+v", relations[0])
	}
}

// TestEntityNotFoundErrors confirms operations on unknown entities surface
// ErrEntityNotFound rather than a raw sql error.
func TestEntityNotFoundErrors(t *testing.T) {
	store := testStore(t)

	err := store.AddObservations(map[string][]string{"ghost": {"boo"}})
	if err == nil {
		t.Fatal("expected error adding observations to unknown entity")
	}
	if !errors.Is(err, ErrEntityNotFound) {
		t.Errorf("expected ErrEntityNotFound, got %v", err)
	}

	err = store.CreateRelations([]Relation{{From: "ghost", To: "ghost", RelationType: "x"}})
	if err == nil {
		t.Fatal("expected error creating relation with unknown endpoints")
	}
	if !errors.Is(err, ErrEntityNotFound) {
		t.Errorf("expected ErrEntityNotFound, got %v", err)
	}
}

// TestSearchNodes checks full-text search returns relevant entities and that an
// empty query yields no results rather than the whole graph.
func TestSearchNodes(t *testing.T) {
	store := testStore(t)

	if err := store.CreateEntities([]Entity{
		{Name: "PostgreSQL", EntityType: "database", Observations: []string{"relational database system"}},
		{Name: "Redis", EntityType: "database", Observations: []string{"in-memory key value store"}},
	}); err != nil {
		t.Fatalf("CreateEntities: %v", err)
	}

	results, err := store.SearchNodes("relational")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one search result for 'relational'")
	}
	found := false
	for _, r := range results {
		if r.Entity.Name == "PostgreSQL" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected PostgreSQL in results, got %+v", results)
	}

	empty, err := store.SearchNodes("   ")
	if err != nil {
		t.Fatalf("SearchNodes(empty): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("empty query should return no results, got %d", len(empty))
	}
}

// TestDeleteEntitiesCascades confirms deleting an entity removes its
// observations and relations via the FK cascade.
func TestDeleteEntitiesCascades(t *testing.T) {
	store := testStore(t)

	if err := store.CreateEntities([]Entity{
		{Name: "A", EntityType: "node", Observations: []string{"obs"}},
		{Name: "B", EntityType: "node"},
	}); err != nil {
		t.Fatalf("CreateEntities: %v", err)
	}
	if err := store.CreateRelations([]Relation{{From: "A", To: "B", RelationType: "links"}}); err != nil {
		t.Fatalf("CreateRelations: %v", err)
	}

	if err := store.DeleteEntities([]string{"A"}); err != nil {
		t.Fatalf("DeleteEntities: %v", err)
	}
	// Deleting an unknown name is a no-op, not an error.
	if err := store.DeleteEntities([]string{"does-not-exist"}); err != nil {
		t.Fatalf("DeleteEntities(unknown) should be a no-op: %v", err)
	}

	graph, err := store.ReadGraph()
	if err != nil {
		t.Fatalf("ReadGraph: %v", err)
	}
	if len(graph.Entities) != 1 {
		t.Errorf("expected 1 remaining entity, got %d", len(graph.Entities))
	}
	if len(graph.Relations) != 0 {
		t.Errorf("expected relations to cascade-delete, got %d", len(graph.Relations))
	}
}
