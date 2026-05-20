// internal/memory/migrations.go
package memory

import (
	"database/sql"
	"fmt"
	"sort"
)

// migration is a single forward-only schema change. Migrations are applied in
// ascending Version order exactly once; the applied set is tracked in the
// schema_migrations table so startup is idempotent.
type migration struct {
	Version int
	Name    string
	// SQL is executed inside the same transaction that records the version,
	// so a failed migration leaves neither the schema change nor the version
	// marker behind.
	SQL string
}

// schemaMigrations is the ordered, forward-only list of migrations. To evolve
// the schema, append a new entry with the next Version number. Never edit or
// reorder existing entries: they may already be applied in production.
var schemaMigrations = []migration{
	{
		Version: 1,
		Name:    "initial_knowledge_graph",
		SQL: `
		CREATE TABLE IF NOT EXISTS entities (
			id SERIAL PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			entity_type TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT now(),
			updated_at TIMESTAMP DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS observations (
			id SERIAL PRIMARY KEY,
			entity_id INTEGER REFERENCES entities(id) ON DELETE CASCADE,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS relations (
			id SERIAL PRIMARY KEY,
			from_entity_id INTEGER REFERENCES entities(id) ON DELETE CASCADE,
			to_entity_id INTEGER REFERENCES entities(id) ON DELETE CASCADE,
			relation_type TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT now()
		);

		CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);
		CREATE INDEX IF NOT EXISTS idx_entities_name_fts ON entities USING gin(to_tsvector('english', name));
		CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(entity_type);
		CREATE INDEX IF NOT EXISTS idx_observations_entity_id ON observations(entity_id);
		CREATE INDEX IF NOT EXISTS idx_observations_content_fts ON observations USING gin(to_tsvector('english', content));
		CREATE INDEX IF NOT EXISTS idx_relations_from_entity_id ON relations(from_entity_id);
		CREATE INDEX IF NOT EXISTS idx_relations_to_entity_id ON relations(to_entity_id);

		CREATE OR REPLACE FUNCTION update_updated_at_column()
		RETURNS TRIGGER AS $$
		BEGIN
			NEW.updated_at = now();
			RETURN NEW;
		END;
		$$ language 'plpgsql';

		DROP TRIGGER IF EXISTS update_entities_updated_at ON entities;
		CREATE TRIGGER update_entities_updated_at
			BEFORE UPDATE ON entities
			FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
		`,
	},
	{
		Version: 2,
		Name:    "dedupe_relations",
		SQL: `
		-- Collapse any pre-existing duplicate relations down to one row each so
		-- the unique index below can be created, then prevent future dupes.
		DELETE FROM relations r
		USING relations dup
		WHERE r.from_entity_id = dup.from_entity_id
		  AND r.to_entity_id = dup.to_entity_id
		  AND r.relation_type = dup.relation_type
		  AND r.id > dup.id;

		CREATE UNIQUE INDEX IF NOT EXISTS uq_relations_triple
			ON relations(from_entity_id, to_entity_id, relation_type);
		`,
	},
	{
		Version: 3,
		Name:    "dedupe_observations",
		SQL: `
		-- An entity should not carry the same observation text twice.
		DELETE FROM observations o
		USING observations dup
		WHERE o.entity_id = dup.entity_id
		  AND o.content = dup.content
		  AND o.id > dup.id;

		CREATE UNIQUE INDEX IF NOT EXISTS uq_observations_entity_content
			ON observations(entity_id, content);
		`,
	},
}

// orderedMigrations returns a copy of the migration list sorted by ascending
// Version. Sorting defensively means the source slice need not be hand-ordered
// and a regression there cannot apply migrations out of sequence.
func orderedMigrations(migrations []migration) []migration {
	out := make([]migration, len(migrations))
	copy(out, migrations)
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })

	return out
}

// validateMigrations ensures the migration set is well-formed: versions are
// strictly increasing with no duplicates and all are positive. A malformed set
// is a programming error and must fail loudly at startup rather than silently
// skip or double-apply a step.
func validateMigrations(migrations []migration) error {
	ordered := orderedMigrations(migrations)
	prev := 0
	for _, m := range ordered {
		if m.Version <= 0 {
			return fmt.Errorf("migration %q has non-positive version %d", m.Name, m.Version)
		}
		if m.Version == prev {
			return fmt.Errorf("duplicate migration version %d", m.Version)
		}
		prev = m.Version
	}

	return nil
}

// pendingMigrations returns the subset of migrations whose version is greater
// than appliedVersion, in ascending order.
func pendingMigrations(migrations []migration, appliedVersion int) []migration {
	ordered := orderedMigrations(migrations)
	pending := make([]migration, 0, len(ordered))
	for _, m := range ordered {
		if m.Version > appliedVersion {
			pending = append(pending, m)
		}
	}

	return pending
}

// runMigrations applies every pending migration in order. It is idempotent:
// already-applied versions are skipped, and each migration runs in its own
// transaction together with the row that records it.
func runMigrations(db *sql.DB, migrations []migration) error {
	if err := validateMigrations(migrations); err != nil {
		return fmt.Errorf("invalid migration set: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	applied, err := currentSchemaVersion(db)
	if err != nil {
		return err
	}

	for _, m := range pendingMigrations(migrations, applied) {
		if err := applyMigration(db, m); err != nil {
			return fmt.Errorf("migration %d (%s) failed: %w", m.Version, m.Name, err)
		}
	}

	return nil
}

// currentSchemaVersion returns the highest applied migration version, or 0 when
// none have been applied yet.
func currentSchemaVersion(db *sql.DB) (int, error) {
	var version sql.NullInt64
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("failed to read current schema version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}

	return int(version.Int64), nil
}

// applyMigration runs one migration's SQL and records its version atomically.
func applyMigration(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(m.SQL); err != nil {
		return err
	}

	if _, err := tx.Exec(
		"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
		m.Version, m.Name,
	); err != nil {
		return fmt.Errorf("failed to record migration version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}
	committed = true

	return nil
}
