// internal/memory/memory_store.go
package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	_ "github.com/lib/pq"
)

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
	Entity    Entity  `json:"entity"`
	Relevance float64 `json:"relevance"`
	Matches   []string `json:"matches"`
}

func NewMemoryStore(databaseURL string, logger logr.Logger) (*MemoryStore, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &MemoryStore{
		db:     db,
		logger: logger,
	}

	// Initialize the database schema
	if err := store.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

func (ms *MemoryStore) Close() error {
	return ms.db.Close()
}

func (ms *MemoryStore) initSchema() error {
	// Create tables if they don't exist (compatible with memory service schema)
	schema := `
	-- Entities table
	CREATE TABLE IF NOT EXISTS entities (
		id SERIAL PRIMARY KEY,
		name TEXT UNIQUE NOT NULL,
		entity_type TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT now(),
		updated_at TIMESTAMP DEFAULT now()
	);

	-- Observations table
	CREATE TABLE IF NOT EXISTS observations (
		id SERIAL PRIMARY KEY,
		entity_id INTEGER REFERENCES entities(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT now()
	);

	-- Relations table
	CREATE TABLE IF NOT EXISTS relations (
		id SERIAL PRIMARY KEY,
		from_entity_id INTEGER REFERENCES entities(id) ON DELETE CASCADE,
		to_entity_id INTEGER REFERENCES entities(id) ON DELETE CASCADE,
		relation_type TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT now()
	);

	-- Create indexes if they don't exist
	CREATE INDEX IF NOT EXISTS entities_name_key ON entities(name);
	CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);
	CREATE INDEX IF NOT EXISTS idx_entities_name_fts ON entities USING gin(to_tsvector('english', name));
	CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(entity_type);
	CREATE INDEX IF NOT EXISTS idx_observations_entity_id ON observations(entity_id);
	CREATE INDEX IF NOT EXISTS idx_observations_content_fts ON observations USING gin(to_tsvector('english', content));
	CREATE INDEX IF NOT EXISTS idx_relations_from_entity_id ON relations(from_entity_id);
	CREATE INDEX IF NOT EXISTS idx_relations_to_entity_id ON relations(to_entity_id);

	-- Create trigger function for updating updated_at
	CREATE OR REPLACE FUNCTION update_updated_at_column()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = now();
		RETURN NEW;
	END;
	$$ language 'plpgsql';

	-- Create trigger for entities table
	DROP TRIGGER IF EXISTS update_entities_updated_at ON entities;
	CREATE TRIGGER update_entities_updated_at
		BEFORE UPDATE ON entities
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
	`

	_, err := ms.db.Exec(schema)
	return err
}

// CreateEntities creates multiple entities in the knowledge graph
func (ms *MemoryStore) CreateEntities(entities []Entity) error {
	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			// Only log rollback errors if the transaction hasn't been committed
			ms.logger.V(1).Info("Transaction rollback result", "error", err)
		}
	}()

	for _, entity := range entities {
		// Insert entity
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

		// Get entity ID for observations
		var entityID int
		err = tx.QueryRow("SELECT id FROM entities WHERE name = $1", entity.Name).Scan(&entityID)
		if err != nil {
			return fmt.Errorf("failed to get entity ID for %s: %w", entity.Name, err)
		}

		// Insert observations
		for _, observation := range entity.Observations {
			_, err := tx.Exec(`
				INSERT INTO observations (entity_id, content) 
				VALUES ($1, $2)
			`, entityID, observation)
			
			if err != nil {
				return fmt.Errorf("failed to insert observation for entity %s: %w", entity.Name, err)
			}
		}
	}

	return tx.Commit()
}

// DeleteEntities deletes entities and their associated data
func (ms *MemoryStore) DeleteEntities(entityNames []string) error {
	if len(entityNames) == 0 {
		return nil
	}

	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			// Only log rollback errors if the transaction hasn't been committed
			ms.logger.V(1).Info("Transaction rollback result", "error", err)
		}
	}()

	// Create placeholders for IN clause
	placeholders := make([]string, len(entityNames))
	args := make([]interface{}, len(entityNames))
	for i, name := range entityNames {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = name
	}

	query := fmt.Sprintf("DELETE FROM entities WHERE name IN (%s)", strings.Join(placeholders, ","))
	_, err = tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete entities: %w", err)
	}

	return tx.Commit()
}

// AddObservations adds new observations to existing entities
func (ms *MemoryStore) AddObservations(observations map[string][]string) error {
	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			// Only log rollback errors if the transaction hasn't been committed
			ms.logger.V(1).Info("Transaction rollback result", "error", err)
		}
	}()

	for entityName, contents := range observations {
		// Get entity ID
		var entityID int
		err := tx.QueryRow("SELECT id FROM entities WHERE name = $1", entityName).Scan(&entityID)
		if err != nil {
			return fmt.Errorf("failed to get entity ID for %s: %w", entityName, err)
		}

		for _, content := range contents {
			_, err := tx.Exec(`
				INSERT INTO observations (entity_id, content) 
				VALUES ($1, $2)
			`, entityID, content)
			
			if err != nil {
				return fmt.Errorf("failed to insert observation for entity %s: %w", entityName, err)
			}
		}

		// Update entity's updated_at timestamp
		_, err = tx.Exec(`
			UPDATE entities SET updated_at = now() WHERE name = $1
		`, entityName)
		
		if err != nil {
			return fmt.Errorf("failed to update entity timestamp %s: %w", entityName, err)
		}
	}

	return tx.Commit()
}

// DeleteObservations removes specific observations from entities
func (ms *MemoryStore) DeleteObservations(deletions map[string][]string) error {
	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			// Only log rollback errors if the transaction hasn't been committed
			ms.logger.V(1).Info("Transaction rollback result", "error", err)
		}
	}()

	for entityName, contents := range deletions {
		// Get entity ID
		var entityID int
		err := tx.QueryRow("SELECT id FROM entities WHERE name = $1", entityName).Scan(&entityID)
		if err != nil {
			return fmt.Errorf("failed to get entity ID for %s: %w", entityName, err)
		}

		for _, content := range contents {
			_, err := tx.Exec(`
				DELETE FROM observations WHERE entity_id = $1 AND content = $2
			`, entityID, content)
			
			if err != nil {
				return fmt.Errorf("failed to delete observation for entity %s: %w", entityName, err)
			}
		}

		// Update entity's updated_at timestamp
		_, err = tx.Exec(`
			UPDATE entities SET updated_at = now() WHERE name = $1
		`, entityName)
		
		if err != nil {
			return fmt.Errorf("failed to update entity timestamp %s: %w", entityName, err)
		}
	}

	return tx.Commit()
}

// CreateRelations creates typed relationships between entities
func (ms *MemoryStore) CreateRelations(relations []Relation) error {
	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			// Only log rollback errors if the transaction hasn't been committed
			ms.logger.V(1).Info("Transaction rollback result", "error", err)
		}
	}()

	for _, relation := range relations {
		// Get entity IDs
		var fromEntityID, toEntityID int
		err := tx.QueryRow("SELECT id FROM entities WHERE name = $1", relation.From).Scan(&fromEntityID)
		if err != nil {
			return fmt.Errorf("failed to get from entity ID for %s: %w", relation.From, err)
		}
		err = tx.QueryRow("SELECT id FROM entities WHERE name = $1", relation.To).Scan(&toEntityID)
		if err != nil {
			return fmt.Errorf("failed to get to entity ID for %s: %w", relation.To, err)
		}

		_, err = tx.Exec(`
			INSERT INTO relations (from_entity_id, to_entity_id, relation_type) 
			VALUES ($1, $2, $3)
		`, fromEntityID, toEntityID, relation.RelationType)
		
		if err != nil {
			return fmt.Errorf("failed to insert relation %s -> %s: %w", relation.From, relation.To, err)
		}
	}

	return tx.Commit()
}

// DeleteRelations removes specific relationships
func (ms *MemoryStore) DeleteRelations(relations []Relation) error {
	tx, err := ms.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			// Only log rollback errors if the transaction hasn't been committed
			ms.logger.V(1).Info("Transaction rollback result", "error", err)
		}
	}()

	for _, relation := range relations {
		// Get entity IDs
		var fromEntityID, toEntityID int
		err := tx.QueryRow("SELECT id FROM entities WHERE name = $1", relation.From).Scan(&fromEntityID)
		if err != nil {
			return fmt.Errorf("failed to get from entity ID for %s: %w", relation.From, err)
		}
		err = tx.QueryRow("SELECT id FROM entities WHERE name = $1", relation.To).Scan(&toEntityID)
		if err != nil {
			return fmt.Errorf("failed to get to entity ID for %s: %w", relation.To, err)
		}

		_, err = tx.Exec(`
			DELETE FROM relations 
			WHERE from_entity_id = $1 AND to_entity_id = $2 AND relation_type = $3
		`, fromEntityID, toEntityID, relation.RelationType)
		
		if err != nil {
			return fmt.Errorf("failed to delete relation %s -> %s: %w", relation.From, relation.To, err)
		}
	}

	return tx.Commit()
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

// SearchNodes searches for nodes using full-text search
func (ms *MemoryStore) SearchNodes(query string) ([]SearchResult, error) {
	// Search in entity names, types, and observation content
	sqlQuery := `
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
		LIMIT 50
	`

	rows, err := ms.db.Query(sqlQuery, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search nodes: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			ms.logger.Info("Failed to close database rows", "error", err)
		}
	}()

	var results []SearchResult
	for rows.Next() {
		var entity Entity
		var relevance float64
		var matches []string

		err := rows.Scan(
			&entity.Name, &entity.EntityType, &entity.CreatedAt, &entity.UpdatedAt,
			&relevance, &matches,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan search result: %w", err)
		}

		// Get all observations for this entity
		observations, err := ms.getObservationsForEntity(entity.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get observations for entity %s: %w", entity.Name, err)
		}
		entity.Observations = observations

		results = append(results, SearchResult{
			Entity:    entity,
			Relevance: relevance,
			Matches:   matches,
		})
	}

	return results, nil
}

// OpenNodes retrieves specific nodes by their names
func (ms *MemoryStore) OpenNodes(names []string) ([]Entity, error) {
	if len(names) == 0 {
		return []Entity{}, nil
	}

	// Create placeholders for IN clause
	placeholders := make([]string, len(names))
	args := make([]interface{}, len(names))
	for i, name := range names {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = name
	}

	query := fmt.Sprintf(`
		SELECT name, entity_type, created_at, updated_at
		FROM entities 
		WHERE name IN (%s)
		ORDER BY name
	`, strings.Join(placeholders, ","))

	rows, err := ms.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to open nodes: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			ms.logger.Info("Failed to close database rows", "error", err)
		}
	}()

	var entities []Entity
	for rows.Next() {
		var entity Entity
		err := rows.Scan(&entity.Name, &entity.EntityType, &entity.CreatedAt, &entity.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan entity: %w", err)
		}

		// Get observations for this entity
		observations, err := ms.getObservationsForEntity(entity.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get observations for entity %s: %w", entity.Name, err)
		}
		entity.Observations = observations

		entities = append(entities, entity)
	}

	return entities, nil
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

	var entities []Entity
	for rows.Next() {
		var entity Entity
		err := rows.Scan(&entity.Name, &entity.EntityType, &entity.CreatedAt, &entity.UpdatedAt)
		if err != nil {
			return nil, err
		}

		// Get observations for this entity
		observations, err := ms.getObservationsForEntity(entity.Name)
		if err != nil {
			return nil, err
		}
		entity.Observations = observations

		entities = append(entities, entity)
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

	var relations []Relation
	for rows.Next() {
		var relation Relation
		err := rows.Scan(&relation.From, &relation.To, &relation.RelationType, &relation.CreatedAt)
		if err != nil {
			return nil, err
		}
		relations = append(relations, relation)
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

	var observations []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		observations = append(observations, content)
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
	stats["entityTypes"] = entityTypes

	return stats, nil
}