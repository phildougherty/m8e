// internal/memory/memory_store.go
package memory

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/lib/pq"
)

// Connection pool defaults. Postgres connections are relatively expensive and
// the memory service is not write-heavy, so a modest bounded pool keeps the
// database side predictable while still allowing concurrent reads.
const (
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 5 * time.Minute
)

// ErrEntityNotFound is returned when an operation references an entity name
// that does not exist in the graph. Callers can use errors.Is to distinguish
// a missing entity from a transient database failure.
var ErrEntityNotFound = errors.New("entity not found")

// MemoryStore provides PostgreSQL-backed knowledge graph storage
type MemoryStore struct {
	db     *sql.DB
	logger logr.Logger
}

// Entity represents a named entity in the knowledge graph
type Entity struct {
	Name         string    `json:"name"`
	EntityType   string    `json:"entityType"`
	Observations []string  `json:"observations"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Relation represents a typed relationship between entities
type Relation struct {
	From         string    `json:"from"`
	To           string    `json:"to"`
	RelationType string    `json:"relationType"`
	CreatedAt    time.Time `json:"createdAt"`
}

// KnowledgeGraph represents the complete graph structure
type KnowledgeGraph struct {
	Entities  []Entity   `json:"entities"`
	Relations []Relation `json:"relations"`
}

// SearchResult represents search results with relevance
type SearchResult struct {
	Entity    Entity   `json:"entity"`
	Relevance float64  `json:"relevance"`
	Matches   []string `json:"matches"`
}

func NewMemoryStore(databaseURL string, logger logr.Logger) (*MemoryStore, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		// sql.Open only validates the DSN format; a bad driver name or
		// malformed URL surfaces here.
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Bound the connection pool so transient load spikes cannot exhaust the
	// Postgres connection limit.
	db.SetMaxOpenConns(defaultMaxOpenConns)
	db.SetMaxIdleConns(defaultMaxIdleConns)
	db.SetConnMaxLifetime(defaultConnMaxLifetime)

	// Test the connection. A failure here is a clear, returnable error rather
	// than a panic or a silently-nil store.
	if err := db.Ping(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.V(1).Info("failed to close database after failed ping", "error", closeErr)
		}

		return nil, fmt.Errorf("database unavailable: failed to ping database: %w", err)
	}

	store := &MemoryStore{
		db:     db,
		logger: logger,
	}

	// Bring the schema up to date via the ordered migration runner.
	if err := store.initSchema(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.V(1).Info("failed to close database after failed migration", "error", closeErr)
		}

		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

func (ms *MemoryStore) Close() error {
	return ms.db.Close()
}

// initSchema applies all pending schema migrations in order. It is idempotent:
// migrations already recorded in schema_migrations are skipped.
func (ms *MemoryStore) initSchema() error {
	return runMigrations(ms.db, schemaMigrations)
}

// SchemaVersion reports the highest migration version currently applied to the
// database. Useful for diagnostics and health surfaces.
func (ms *MemoryStore) SchemaVersion() (int, error) {
	return currentSchemaVersion(ms.db)
}

// lookupEntityID resolves an entity name to its primary key within a
// transaction, translating the absent-row case into ErrEntityNotFound so
// callers get an actionable error instead of a bare sql.ErrNoRows.
func lookupEntityID(tx *sql.Tx, name string) (int, error) {
	var id int
	err := tx.QueryRow("SELECT id FROM entities WHERE name = $1", name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: %q", ErrEntityNotFound, name)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to look up entity %q: %w", name, err)
	}

	return id, nil
}

// CreateEntities creates multiple entities in the knowledge graph. The whole
// batch is applied in a single transaction so a partial failure leaves the
// graph untouched. Entity names are unique: re-creating an existing name
// updates its type. Observations are inserted idempotently so re-running the
// same create does not accumulate duplicates.
func (ms *MemoryStore) CreateEntities(entities []Entity) error {
	if len(entities) == 0 {
		return nil
	}

	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				ms.logger.V(1).Info("transaction rollback result", "error", rbErr)
			}
		}
	}()

	for _, entity := range entities {
		if entity.Name == "" {
			return fmt.Errorf("entity name must not be empty")
		}
		if entity.EntityType == "" {
			return fmt.Errorf("entity %q: entityType must not be empty", entity.Name)
		}

		_, err := tx.Exec(`
			INSERT INTO entities (name, entity_type)
			VALUES ($1, $2)
			ON CONFLICT (name) DO UPDATE SET
				entity_type = EXCLUDED.entity_type,
				updated_at = now()
		`, entity.Name, entity.EntityType)
		if err != nil {
			return fmt.Errorf("failed to insert entity %s: %w", entity.Name, err)
		}

		entityID, err := lookupEntityID(tx, entity.Name)
		if err != nil {
			return err
		}

		for _, observation := range entity.Observations {
			// ON CONFLICT DO NOTHING relies on the uq_observations_entity_content
			// index (migration 3) so repeated observation text is dropped
			// rather than duplicated.
			_, err := tx.Exec(`
				INSERT INTO observations (entity_id, content)
				VALUES ($1, $2)
				ON CONFLICT (entity_id, content) DO NOTHING
			`, entityID, observation)
			if err != nil {
				return fmt.Errorf("failed to insert observation for entity %s: %w", entity.Name, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	return nil
}

// inClause builds a parameterized "$1,$2,..." placeholder list and the matching
// argument slice for an IN (...) clause, starting numbering at startIndex. It
// keeps every value bound as a query parameter so the names are never
// interpolated into SQL text.
func inClause(values []string, startIndex int) (string, []interface{}) {
	placeholders := make([]string, len(values))
	args := make([]interface{}, len(values))
	for i, v := range values {
		placeholders[i] = fmt.Sprintf("$%d", startIndex+i)
		args[i] = v
	}

	return strings.Join(placeholders, ","), args
}

// DeleteEntities deletes entities and all of their observations and relations.
// The observations and relations rows are removed automatically by the ON
// DELETE CASCADE foreign keys. Deleting an unknown name is a no-op, not an
// error, so the call is idempotent.
func (ms *MemoryStore) DeleteEntities(entityNames []string) error {
	if len(entityNames) == 0 {
		return nil
	}

	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				ms.logger.V(1).Info("transaction rollback result", "error", rbErr)
			}
		}
	}()

	placeholders, args := inClause(entityNames, 1)
	query := fmt.Sprintf("DELETE FROM entities WHERE name IN (%s)", placeholders)
	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to delete entities: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	return nil
}

// AddObservations adds new observations to existing entities. Every entity
// referenced must already exist; an unknown name aborts the whole batch with
// ErrEntityNotFound. Duplicate observation text for an entity is silently
// ignored. The entire batch is one transaction.
func (ms *MemoryStore) AddObservations(observations map[string][]string) error {
	if len(observations) == 0 {
		return nil
	}

	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				ms.logger.V(1).Info("transaction rollback result", "error", rbErr)
			}
		}
	}()

	for entityName, contents := range observations {
		entityID, err := lookupEntityID(tx, entityName)
		if err != nil {
			return err
		}

		for _, content := range contents {
			_, err := tx.Exec(`
				INSERT INTO observations (entity_id, content)
				VALUES ($1, $2)
				ON CONFLICT (entity_id, content) DO NOTHING
			`, entityID, content)
			if err != nil {
				return fmt.Errorf("failed to insert observation for entity %s: %w", entityName, err)
			}
		}

		if _, err := tx.Exec(`UPDATE entities SET updated_at = now() WHERE id = $1`, entityID); err != nil {
			return fmt.Errorf("failed to update entity timestamp %s: %w", entityName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	return nil
}

// DeleteObservations removes specific observations from entities. Every entity
// referenced must already exist. Deleting an observation that is not present
// is a no-op. The entire batch is one transaction.
func (ms *MemoryStore) DeleteObservations(deletions map[string][]string) error {
	if len(deletions) == 0 {
		return nil
	}

	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				ms.logger.V(1).Info("transaction rollback result", "error", rbErr)
			}
		}
	}()

	for entityName, contents := range deletions {
		entityID, err := lookupEntityID(tx, entityName)
		if err != nil {
			return err
		}

		for _, content := range contents {
			_, err := tx.Exec(`
				DELETE FROM observations WHERE entity_id = $1 AND content = $2
			`, entityID, content)
			if err != nil {
				return fmt.Errorf("failed to delete observation for entity %s: %w", entityName, err)
			}
		}

		if _, err := tx.Exec(`UPDATE entities SET updated_at = now() WHERE id = $1`, entityID); err != nil {
			return fmt.Errorf("failed to update entity timestamp %s: %w", entityName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	return nil
}

// CreateRelations creates typed relationships between entities. Both endpoints
// of every relation must already exist; a missing endpoint aborts the whole
// batch with ErrEntityNotFound. A (from, to, relationType) triple is unique,
// so re-creating an existing relation is a harmless no-op. The entire batch is
// one transaction.
func (ms *MemoryStore) CreateRelations(relations []Relation) error {
	if len(relations) == 0 {
		return nil
	}

	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				ms.logger.V(1).Info("transaction rollback result", "error", rbErr)
			}
		}
	}()

	for _, relation := range relations {
		if relation.RelationType == "" {
			return fmt.Errorf("relation %s -> %s: relationType must not be empty", relation.From, relation.To)
		}

		fromEntityID, err := lookupEntityID(tx, relation.From)
		if err != nil {
			return err
		}
		toEntityID, err := lookupEntityID(tx, relation.To)
		if err != nil {
			return err
		}

		// ON CONFLICT relies on uq_relations_triple (migration 2).
		_, err = tx.Exec(`
			INSERT INTO relations (from_entity_id, to_entity_id, relation_type)
			VALUES ($1, $2, $3)
			ON CONFLICT (from_entity_id, to_entity_id, relation_type) DO NOTHING
		`, fromEntityID, toEntityID, relation.RelationType)
		if err != nil {
			return fmt.Errorf("failed to insert relation %s -> %s: %w", relation.From, relation.To, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	return nil
}

// DeleteRelations removes specific relationships. Both endpoints of every
// relation must already exist. Deleting a relation that is not present is a
// no-op. The entire batch is one transaction.
func (ms *MemoryStore) DeleteRelations(relations []Relation) error {
	if len(relations) == 0 {
		return nil
	}

	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				ms.logger.V(1).Info("transaction rollback result", "error", rbErr)
			}
		}
	}()

	for _, relation := range relations {
		fromEntityID, err := lookupEntityID(tx, relation.From)
		if err != nil {
			return err
		}
		toEntityID, err := lookupEntityID(tx, relation.To)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`
			DELETE FROM relations
			WHERE from_entity_id = $1 AND to_entity_id = $2 AND relation_type = $3
		`, fromEntityID, toEntityID, relation.RelationType)
		if err != nil {
			return fmt.Errorf("failed to delete relation %s -> %s: %w", relation.From, relation.To, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	return nil
}

// ReadGraph retrieves the entire knowledge graph
func (ms *MemoryStore) ReadGraph() (*KnowledgeGraph, error) {
	graph := &KnowledgeGraph{
		Entities:  make([]Entity, 0),
		Relations: make([]Relation, 0),
	}

	// Get all entities with their observations
	entities, err := ms.getAllEntities()
	if err != nil {
		return nil, fmt.Errorf("failed to get entities: %w", err)
	}
	graph.Entities = entities

	// Get all relations
	relations, err := ms.getAllRelations()
	if err != nil {
		return nil, fmt.Errorf("failed to get relations: %w", err)
	}
	graph.Relations = relations

	return graph, nil
}

// defaultSearchLimit caps the number of rows SearchNodes returns.
const defaultSearchLimit = 50

// searchNodesQuery is the PostgreSQL full-text search statement used by
// SearchNodes. It is a package-level constant so it can be asserted on in unit
// tests without a live database. The single bind parameter ($1) is the raw
// user query, fed to plainto_tsquery which safely tokenizes arbitrary text
// (no SQL injection surface). It ranks entity name/type matches and
// observation-content matches, sums the ranks, and returns matched
// observations for context.
const searchNodesQuery = `
		WITH entity_matches AS (
			SELECT
				e.name, e.entity_type, e.created_at, e.updated_at,
				ts_rank(to_tsvector('english', e.name || ' ' || e.entity_type), plainto_tsquery('english', $1)) as name_rank
			FROM entities e
			WHERE to_tsvector('english', e.name || ' ' || e.entity_type) @@ plainto_tsquery('english', $1)
		),
		observation_matches AS (
			SELECT
				e.name as entity_name,
				ts_rank(to_tsvector('english', o.content), plainto_tsquery('english', $1)) as content_rank,
				array_agg(o.content) as matching_observations
			FROM observations o
			JOIN entities e ON o.entity_id = e.id
			WHERE to_tsvector('english', o.content) @@ plainto_tsquery('english', $1)
			GROUP BY e.name
		)
		SELECT DISTINCT
			e.name, e.entity_type, e.created_at, e.updated_at,
			COALESCE(em.name_rank, 0) + COALESCE(om.content_rank, 0) as total_rank,
			COALESCE(om.matching_observations, ARRAY[]::text[]) as matches
		FROM entities e
		LEFT JOIN entity_matches em ON e.name = em.name
		LEFT JOIN observation_matches om ON e.name = om.entity_name
		WHERE em.name IS NOT NULL OR om.entity_name IS NOT NULL
		ORDER BY total_rank DESC
		LIMIT $2
	`

// SearchNodes searches for nodes using PostgreSQL full-text search over entity
// names, entity types, and observation content. An empty or whitespace-only
// query matches nothing and returns an empty slice rather than every row.
// Results are returned newest-relevance-first, capped at defaultSearchLimit.
func (ms *MemoryStore) SearchNodes(query string) ([]SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return []SearchResult{}, nil
	}

	rows, err := ms.db.Query(searchNodesQuery, query, defaultSearchLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to search nodes: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			ms.logger.Info("Failed to close database rows", "error", err)
		}
	}()

	results := make([]SearchResult, 0)
	for rows.Next() {
		var entity Entity
		var relevance float64
		// matches comes back as a Postgres text[]; pq.Array is required to
		// scan it into a Go slice.
		var matches pq.StringArray

		err := rows.Scan(
			&entity.Name, &entity.EntityType, &entity.CreatedAt, &entity.UpdatedAt,
			&relevance, &matches,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan search result: %w", err)
		}

		observations, err := ms.getObservationsForEntity(entity.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get observations for entity %s: %w", entity.Name, err)
		}
		entity.Observations = observations

		results = append(results, SearchResult{
			Entity:    entity,
			Relevance: relevance,
			Matches:   []string(matches),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating search results: %w", err)
	}

	return results, nil
}

// OpenNodes retrieves specific nodes by their names
func (ms *MemoryStore) OpenNodes(names []string) ([]Entity, error) {
	if len(names) == 0 {
		return []Entity{}, nil
	}

	placeholders, args := inClause(names, 1)
	query := fmt.Sprintf(`
		SELECT name, entity_type, created_at, updated_at
		FROM entities
		WHERE name IN (%s)
		ORDER BY name
	`, placeholders)

	rows, err := ms.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to open nodes: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			ms.logger.Info("Failed to close database rows", "error", err)
		}
	}()

	entities := make([]Entity, 0)
	for rows.Next() {
		var entity Entity
		err := rows.Scan(&entity.Name, &entity.EntityType, &entity.CreatedAt, &entity.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan entity: %w", err)
		}

		observations, err := ms.getObservationsForEntity(entity.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get observations for entity %s: %w", entity.Name, err)
		}
		entity.Observations = observations

		entities = append(entities, entity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating entities: %w", err)
	}

	return entities, nil
}

// GetRelationsForEntities returns every relation that touches any of the named
// entities, either as the source or the target. This is the graph-traversal
// primitive: given a set of nodes it surfaces the edges connecting them to the
// rest of the graph. An empty input returns an empty slice.
func (ms *MemoryStore) GetRelationsForEntities(names []string) ([]Relation, error) {
	if len(names) == 0 {
		return []Relation{}, nil
	}

	// The same name list is bound twice (once for the "from" side, once for
	// the "to" side), so the second placeholder set continues numbering after
	// the first.
	fromPlaceholders, fromArgs := inClause(names, 1)
	toPlaceholders, toArgs := inClause(names, 1+len(names))
	args := append(fromArgs, toArgs...)

	query := fmt.Sprintf(`
		SELECT e1.name AS from_entity, e2.name AS to_entity, r.relation_type, r.created_at
		FROM relations r
		JOIN entities e1 ON r.from_entity_id = e1.id
		JOIN entities e2 ON r.to_entity_id = e2.id
		WHERE e1.name IN (%s) OR e2.name IN (%s)
		ORDER BY e1.name, e2.name, r.relation_type
	`, fromPlaceholders, toPlaceholders)

	rows, err := ms.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get relations for entities: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			ms.logger.Info("Failed to close database rows", "error", err)
		}
	}()

	relations := make([]Relation, 0)
	for rows.Next() {
		var relation Relation
		if err := rows.Scan(&relation.From, &relation.To, &relation.RelationType, &relation.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan relation: %w", err)
		}
		relations = append(relations, relation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating relations: %w", err)
	}

	return relations, nil
}

// Helper methods

func (ms *MemoryStore) getAllEntities() ([]Entity, error) {
	rows, err := ms.db.Query(`
		SELECT name, entity_type, created_at, updated_at
		FROM entities 
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			ms.logger.Info("Failed to close database rows", "error", err)
		}
	}()

	entities := make([]Entity, 0)
	for rows.Next() {
		var entity Entity
		err := rows.Scan(&entity.Name, &entity.EntityType, &entity.CreatedAt, &entity.UpdatedAt)
		if err != nil {
			return nil, err
		}

		observations, err := ms.getObservationsForEntity(entity.Name)
		if err != nil {
			return nil, err
		}
		entity.Observations = observations

		entities = append(entities, entity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating entities: %w", err)
	}

	return entities, nil
}

func (ms *MemoryStore) getAllRelations() ([]Relation, error) {
	rows, err := ms.db.Query(`
		SELECT e1.name as from_entity, e2.name as to_entity, r.relation_type, r.created_at
		FROM relations r
		JOIN entities e1 ON r.from_entity_id = e1.id
		JOIN entities e2 ON r.to_entity_id = e2.id
		ORDER BY e1.name, e2.name
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			ms.logger.Info("Failed to close database rows", "error", err)
		}
	}()

	relations := make([]Relation, 0)
	for rows.Next() {
		var relation Relation
		err := rows.Scan(&relation.From, &relation.To, &relation.RelationType, &relation.CreatedAt)
		if err != nil {
			return nil, err
		}
		relations = append(relations, relation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating relations: %w", err)
	}

	return relations, nil
}

func (ms *MemoryStore) getObservationsForEntity(entityName string) ([]string, error) {
	rows, err := ms.db.Query(`
		SELECT o.content 
		FROM observations o
		JOIN entities e ON o.entity_id = e.id
		WHERE e.name = $1 
		ORDER BY o.created_at
	`, entityName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			ms.logger.Info("Failed to close database rows", "error", err)
		}
	}()

	observations := make([]string, 0)
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		observations = append(observations, content)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating observations: %w", err)
	}

	return observations, nil
}

// HealthCheck verifies database connectivity
func (ms *MemoryStore) HealthCheck() error {
	return ms.db.Ping()
}

// GetStats returns statistics about the knowledge graph
func (ms *MemoryStore) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Count entities
	var entityCount int
	err := ms.db.QueryRow("SELECT COUNT(*) FROM entities").Scan(&entityCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count entities: %w", err)
	}
	stats["entities"] = entityCount

	// Count observations
	var observationCount int
	err = ms.db.QueryRow("SELECT COUNT(*) FROM observations").Scan(&observationCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count observations: %w", err)
	}
	stats["observations"] = observationCount

	// Count relations
	var relationCount int
	err = ms.db.QueryRow("SELECT COUNT(*) FROM relations").Scan(&relationCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count relations: %w", err)
	}
	stats["relations"] = relationCount

	// Get entity types
	rows, err := ms.db.Query("SELECT entity_type, COUNT(*) FROM entities GROUP BY entity_type ORDER BY COUNT(*) DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to get entity types: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			ms.logger.Info("Failed to close database rows", "error", err)
		}
	}()

	entityTypes := make(map[string]int)
	for rows.Next() {
		var entityType string
		var count int
		if err := rows.Scan(&entityType, &count); err != nil {
			return nil, err
		}
		entityTypes[entityType] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating entity types: %w", err)
	}
	stats["entityTypes"] = entityTypes

	return stats, nil
}
