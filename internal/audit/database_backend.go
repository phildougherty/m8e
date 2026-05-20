package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "github.com/lib/pq"

	"github.com/phildougherty/m8e/internal/logging"
)

const (
	// auditDBBufferSize caps how many entries are held when the database is
	// unreachable. Beyond this the oldest buffered entries are dropped (and
	// logged loudly) rather than growing memory without bound.
	auditDBBufferSize = 1024
	// auditDBRetryInterval is how often the flush loop retries a buffered
	// backlog against a previously-unavailable database.
	auditDBRetryInterval = 15 * time.Second
)

// databaseBackend persists audit entries to PostgreSQL using the same driver
// (lib/pq) and DSN convention as the memory service. Writes that fail because
// the database is temporarily unavailable are buffered in memory and retried
// by a background loop, so transient outages do not silently lose events.
type databaseBackend struct {
	db     *sql.DB
	table  string
	logger *logging.Logger

	mu     sync.Mutex
	buffer []AuditEntry

	stopCh chan struct{}
	wg     sync.WaitGroup
}

func newDatabaseBackend(cfg backendConfig, logger *logging.Logger) (*databaseBackend, error) {
	if cfg.databaseURL == "" {
		return nil, fmt.Errorf("audit database backend: no connection string " +
			"(set M8E_AUDIT_DATABASE_URL or DATABASE_URL)")
	}

	db, err := sql.Open("postgres", cfg.databaseURL)
	if err != nil {
		return nil, fmt.Errorf("audit database backend: open: %w", err)
	}

	// Bound the standard database/sql pool so a burst of audit writes cannot
	// exhaust connections shared with the rest of the process.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("audit database backend: ping: %w", err)
	}

	b := &databaseBackend{
		db:     db,
		table:  cfg.tableName,
		logger: logger,
		stopCh: make(chan struct{}),
	}
	if b.table == "" {
		b.table = defaultAuditTableName
	}

	if err := b.initSchema(); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("audit database backend: init schema: %w", err)
	}

	b.wg.Add(1)
	go b.flushLoop()

	return b, nil
}

// auditSchemaDDL builds the idempotent CREATE TABLE / CREATE INDEX statements
// for the given table. The table name is a fixed, validated identifier (not
// user input) so interpolating it is safe; all row values are always passed
// as bind parameters elsewhere.
func auditSchemaDDL(table string) string {
	return fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		id           TEXT PRIMARY KEY,
		ts           TIMESTAMPTZ NOT NULL,
		event        TEXT NOT NULL,
		user_id      TEXT NOT NULL DEFAULT '',
		client_id    TEXT NOT NULL DEFAULT '',
		ip_address   TEXT NOT NULL DEFAULT '',
		user_agent   TEXT NOT NULL DEFAULT '',
		success      BOOLEAN NOT NULL,
		error        TEXT NOT NULL DEFAULT '',
		details      JSONB
	);
	CREATE INDEX IF NOT EXISTS idx_%s_ts ON %s(ts);
	CREATE INDEX IF NOT EXISTS idx_%s_event ON %s(event);
	CREATE INDEX IF NOT EXISTS idx_%s_user_id ON %s(user_id);
	CREATE INDEX IF NOT EXISTS idx_%s_client_id ON %s(client_id);
	`, table, table, table, table, table, table, table, table, table)
}

// initSchema creates the audit table idempotently.
func (b *databaseBackend) initSchema() error {
	_, err := b.db.Exec(auditSchemaDDL(b.table))

	return err
}

func (b *databaseBackend) insertStmt() string {
	return fmt.Sprintf(`
		INSERT INTO %s (id, ts, event, user_id, client_id, ip_address, user_agent, success, error, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING`, b.table)
}

// insertOne performs a single parameterized insert. Every value is bound, never
// formatted into the SQL text.
func (b *databaseBackend) insertOne(entry *AuditEntry) error {
	var details []byte
	if entry.Details != nil {
		d, err := json.Marshal(entry.Details)
		if err != nil {
			return fmt.Errorf("marshal details: %w", err)
		}
		details = d
	}

	_, err := b.db.Exec(b.insertStmt(),
		entry.ID,
		entry.Timestamp,
		entry.Event,
		entry.UserID,
		entry.ClientID,
		entry.IP,
		entry.UserAgent,
		entry.Success,
		entry.Error,
		details,
	)

	return err
}

func (b *databaseBackend) Store(entry *AuditEntry) error {
	if err := b.insertOne(entry); err != nil {
		// Database unreachable or rejecting writes: buffer for retry rather
		// than dropping the event. The error is still returned so the caller
		// can surface a degraded state.
		b.bufferEntry(*entry)

		return fmt.Errorf("audit database backend: insert (buffered for retry): %w", err)
	}

	return nil
}

func (b *databaseBackend) bufferEntry(entry AuditEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buffer = append(b.buffer, entry)
	if len(b.buffer) > auditDBBufferSize {
		dropped := len(b.buffer) - auditDBBufferSize
		b.buffer = b.buffer[dropped:]
		if b.logger != nil {
			b.logger.Error("AUDIT: database backend buffer full, dropped %d oldest buffered entries", dropped)
		}
	}
}

// flushLoop periodically retries buffered entries against the database.
func (b *databaseBackend) flushLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(auditDBRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopCh:
			b.flushBuffer()

			return
		case <-ticker.C:
			b.flushBuffer()
		}
	}
}

func (b *databaseBackend) flushBuffer() {
	b.mu.Lock()
	pending := b.buffer
	b.buffer = nil
	b.mu.Unlock()

	if len(pending) == 0 {
		return
	}

	var failed []AuditEntry
	for i := range pending {
		if err := b.insertOne(&pending[i]); err != nil {
			// Still failing: keep this and the remainder for the next cycle.
			failed = append(failed, pending[i:]...)

			break
		}
	}

	if len(failed) > 0 {
		b.mu.Lock()
		// Prepend the still-failed entries ahead of anything newly buffered.
		b.buffer = append(failed, b.buffer...)
		if len(b.buffer) > auditDBBufferSize {
			b.buffer = b.buffer[len(b.buffer)-auditDBBufferSize:]
		}
		b.mu.Unlock()
		if b.logger != nil {
			b.logger.Warning("AUDIT: database backend still unavailable, %d entries buffered", len(failed))
		}

		return
	}

	if b.logger != nil {
		b.logger.Info("AUDIT: database backend flushed %d buffered entries", len(pending))
	}
}

func (b *databaseBackend) Query(limit, offset int, filter *AuditFilter) ([]AuditEntry, int, error) {
	where, args := buildWhereClause(filter)

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", b.table, where)
	var total int
	if err := b.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("audit database backend: count: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, ts, event, user_id, client_id, ip_address, user_agent, success, error, details
		FROM %s%s
		ORDER BY ts DESC`, b.table, where)

	// limit/offset are appended as bind parameters, never formatted in.
	queryArgs := args
	if limit >= 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(queryArgs)+1)
		queryArgs = append(queryArgs, limit)
	}
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", len(queryArgs)+1)
		queryArgs = append(queryArgs, offset)
	}

	rows, err := b.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("audit database backend: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries, err := scanEntries(rows)
	if err != nil {
		return nil, 0, err
	}

	return entries, total, nil
}

// buildWhereClause assembles a parameterized WHERE clause from filter. It
// returns the clause text (with a leading " WHERE " when non-empty) and the
// ordered bind arguments. No filter value is ever interpolated into SQL.
func buildWhereClause(filter *AuditFilter) (string, []interface{}) {
	if filter == nil {
		return "", nil
	}

	var conds []string
	var args []interface{}
	add := func(cond string, val interface{}) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	if filter.Event != "" {
		add("event = $%d", filter.Event)
	}
	if filter.UserID != "" {
		add("user_id = $%d", filter.UserID)
	}
	if filter.ClientID != "" {
		add("client_id = $%d", filter.ClientID)
	}
	if filter.Success != nil {
		add("success = $%d", *filter.Success)
	}
	if !filter.StartTime.IsZero() {
		add("ts >= $%d", filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		add("ts <= $%d", filter.EndTime)
	}

	if len(conds) == 0 {
		return "", nil
	}

	clause := " WHERE " + conds[0]
	for _, c := range conds[1:] {
		clause += " AND " + c
	}

	return clause, args
}

func scanEntries(rows *sql.Rows) ([]AuditEntry, error) {
	var entries []AuditEntry
	for rows.Next() {
		var (
			e       AuditEntry
			details []byte
		)
		if err := rows.Scan(
			&e.ID, &e.Timestamp, &e.Event, &e.UserID, &e.ClientID,
			&e.IP, &e.UserAgent, &e.Success, &e.Error, &details,
		); err != nil {
			return nil, fmt.Errorf("audit database backend: scan row: %w", err)
		}
		if len(details) > 0 {
			if err := json.Unmarshal(details, &e.Details); err != nil {
				return nil, fmt.Errorf("audit database backend: unmarshal details: %w", err)
			}
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit database backend: rows: %w", err)
	}

	return entries, nil
}

func (b *databaseBackend) Stats() (AuditStats, error) {
	stats := AuditStats{EventCounts: make(map[string]int)}

	rows, err := b.db.Query(fmt.Sprintf(
		"SELECT event, COUNT(*), SUM(CASE WHEN success THEN 1 ELSE 0 END) FROM %s GROUP BY event",
		b.table))
	if err != nil {
		return AuditStats{}, fmt.Errorf("audit database backend: stats query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var totalSuccess int
	for rows.Next() {
		var (
			event   string
			count   int
			success int
		)
		if err := rows.Scan(&event, &count, &success); err != nil {
			return AuditStats{}, fmt.Errorf("audit database backend: stats scan: %w", err)
		}
		stats.EventCounts[event] = count
		stats.TotalEntries += count
		totalSuccess += success
	}
	if err := rows.Err(); err != nil {
		return AuditStats{}, fmt.Errorf("audit database backend: stats rows: %w", err)
	}

	if stats.TotalEntries > 0 {
		stats.SuccessRate = float64(totalSuccess) / float64(stats.TotalEntries) * PercentageMultiplier
	}

	return stats, nil
}

func (b *databaseBackend) Cleanup(cutoff time.Time) error {
	_, err := b.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE ts < $1", b.table), cutoff)
	if err != nil {
		return fmt.Errorf("audit database backend: cleanup: %w", err)
	}

	return nil
}

func (b *databaseBackend) Close() error {
	close(b.stopCh)
	b.wg.Wait() // flushLoop drains the buffer one last time before returning.

	return b.db.Close()
}
