package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	// auditFileMode keeps audit logs readable only by the owner: entries may
	// carry sensitive request context (IPs, user agents, error detail).
	auditFileMode = 0o600
	auditDirMode  = 0o700
)

// fileBackend persists audit entries as newline-delimited JSON. NDJSON is
// append-only, greppable, and trivially rotatable. The active file is rotated
// once it exceeds maxFileSize; up to maxBackups timestamped copies are kept.
type fileBackend struct {
	mu          sync.Mutex
	path        string
	maxFileSize int64
	maxBackups  int

	f    *os.File
	w    *bufio.Writer
	size int64
}

func newFileBackend(cfg backendConfig) (*fileBackend, error) {
	if cfg.filePath == "" {
		return nil, fmt.Errorf("audit file backend: empty file path")
	}

	dir := filepath.Dir(cfg.filePath)
	if err := os.MkdirAll(dir, auditDirMode); err != nil {
		return nil, fmt.Errorf("audit file backend: create dir %s: %w", dir, err)
	}

	b := &fileBackend{
		path:        cfg.filePath,
		maxFileSize: cfg.maxFileSize,
		maxBackups:  cfg.maxBackups,
	}
	if b.maxFileSize <= 0 {
		b.maxFileSize = defaultAuditMaxFileSize
	}

	if err := b.openActive(); err != nil {
		return nil, err
	}

	return b, nil
}

// openActive opens (or creates) the active log file in append mode and records
// its current size so rotation decisions are accurate after a restart.
func (b *fileBackend) openActive() error {
	f, err := os.OpenFile(b.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, auditFileMode)
	if err != nil {
		return fmt.Errorf("audit file backend: open %s: %w", b.path, err)
	}
	// Enforce permissions even if the file pre-existed with a looser mode.
	if err := f.Chmod(auditFileMode); err != nil {
		_ = f.Close()

		return fmt.Errorf("audit file backend: chmod %s: %w", b.path, err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return fmt.Errorf("audit file backend: stat %s: %w", b.path, err)
	}

	b.f = f
	b.w = bufio.NewWriter(f)
	b.size = info.Size()

	return nil
}

func (b *fileBackend) Store(entry *AuditEntry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit file backend: marshal entry: %w", err)
	}
	line = append(line, '\n')

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size+int64(len(line)) > b.maxFileSize && b.size > 0 {
		if err := b.rotate(); err != nil {
			return err
		}
	}

	n, err := b.w.Write(line)
	if err != nil {
		return fmt.Errorf("audit file backend: write entry: %w", err)
	}
	// Flush and fsync on every entry: an audit log that loses its most
	// recent records on a crash defeats the purpose.
	if err := b.w.Flush(); err != nil {
		return fmt.Errorf("audit file backend: flush: %w", err)
	}
	if err := b.f.Sync(); err != nil {
		return fmt.Errorf("audit file backend: fsync: %w", err)
	}
	b.size += int64(n)

	return nil
}

// rotate closes the active file, renames it with a timestamp suffix, prunes
// old backups beyond maxBackups, and opens a fresh active file. Caller holds b.mu.
func (b *fileBackend) rotate() error {
	if err := b.w.Flush(); err != nil {
		return fmt.Errorf("audit file backend: flush before rotate: %w", err)
	}
	if err := b.f.Close(); err != nil {
		return fmt.Errorf("audit file backend: close before rotate: %w", err)
	}

	rotated := fmt.Sprintf("%s.%s", b.path, time.Now().UTC().Format("20060102T150405.000000000"))
	if err := os.Rename(b.path, rotated); err != nil {
		return fmt.Errorf("audit file backend: rotate rename: %w", err)
	}

	if err := b.pruneBackups(); err != nil {
		return err
	}

	return b.openActive()
}

// pruneBackups deletes the oldest rotated files so at most maxBackups remain.
func (b *fileBackend) pruneBackups() error {
	matches, err := filepath.Glob(b.path + ".*")
	if err != nil {
		return fmt.Errorf("audit file backend: glob backups: %w", err)
	}
	if len(matches) <= b.maxBackups {
		return nil
	}

	// Lexical sort works because the suffix is a zero-padded UTC timestamp.
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-b.maxBackups] {
		if err := os.Remove(old); err != nil {
			return fmt.Errorf("audit file backend: prune %s: %w", old, err)
		}
	}

	return nil
}

// Query reads every retained file (active + backups), filters, orders
// newest-first, and paginates. Audit query volume is low, so a full scan is
// acceptable and keeps the file format dependency-free.
func (b *fileBackend) Query(limit, offset int, filter *AuditFilter) ([]AuditEntry, int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	all, err := b.readAllLocked()
	if err != nil {
		return nil, 0, err
	}

	filtered := make([]AuditEntry, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		if matchesFilter(all[i], filter) {
			filtered = append(filtered, all[i])
		}
	}

	return paginate(filtered, limit, offset)
}

func (b *fileBackend) Stats() (AuditStats, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	all, err := b.readAllLocked()
	if err != nil {
		return AuditStats{}, err
	}

	return computeStats(all), nil
}

// readAllLocked loads entries from all backups (oldest first) then the active
// file, yielding chronological order. Caller holds b.mu. The active writer is
// flushed first so in-flight buffered entries are visible.
func (b *fileBackend) readAllLocked() ([]AuditEntry, error) {
	if err := b.w.Flush(); err != nil {
		return nil, fmt.Errorf("audit file backend: flush before read: %w", err)
	}

	backups, err := filepath.Glob(b.path + ".*")
	if err != nil {
		return nil, fmt.Errorf("audit file backend: glob backups: %w", err)
	}
	sort.Strings(backups)

	var all []AuditEntry
	for _, p := range append(backups, b.path) {
		entries, err := readEntriesFromFile(p)
		if err != nil {
			return nil, err
		}
		all = append(all, entries...)
	}

	return all, nil
}

func readEntriesFromFile(path string) ([]AuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("audit file backend: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var entries []AuditEntry
	scanner := bufio.NewScanner(f)
	// Audit detail maps can be large; raise the line limit well above the
	// 64 KiB default.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal(line, &e); err != nil {
			// Skip a corrupt line rather than failing the whole query;
			// a truncated tail line can occur after an unclean shutdown.
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("audit file backend: scan %s: %w", path, err)
	}

	return entries, nil
}

// Cleanup removes whole rotated files whose newest entry predates cutoff. The
// active file is never deleted; size-based rotation bounds its growth and
// rewriting it in place would race with concurrent appends.
func (b *fileBackend) Cleanup(cutoff time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	backups, err := filepath.Glob(b.path + ".*")
	if err != nil {
		return fmt.Errorf("audit file backend: glob backups: %w", err)
	}

	for _, p := range backups {
		entries, err := readEntriesFromFile(p)
		if err != nil {
			return err
		}
		newest := time.Time{}
		for _, e := range entries {
			if e.Timestamp.After(newest) {
				newest = e.Timestamp
			}
		}
		if !newest.IsZero() && newest.Before(cutoff) {
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("audit file backend: cleanup remove %s: %w", p, err)
			}
		}
	}

	return nil
}

func (b *fileBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.w != nil {
		if err := b.w.Flush(); err != nil {
			_ = b.f.Close()

			return fmt.Errorf("audit file backend: flush on close: %w", err)
		}
	}
	if b.f != nil {
		if err := b.f.Sync(); err != nil {
			_ = b.f.Close()

			return fmt.Errorf("audit file backend: fsync on close: %w", err)
		}

		return b.f.Close()
	}

	return nil
}
