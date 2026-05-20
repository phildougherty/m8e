package audit

// matchesFilter reports whether entry satisfies every constraint in filter.
// A nil filter matches everything.
func matchesFilter(entry AuditEntry, filter *AuditFilter) bool {
	if filter == nil {
		return true
	}
	if filter.Event != "" && entry.Event != filter.Event {
		return false
	}
	if filter.UserID != "" && entry.UserID != filter.UserID {
		return false
	}
	if filter.ClientID != "" && entry.ClientID != filter.ClientID {
		return false
	}
	if filter.Success != nil && entry.Success != *filter.Success {
		return false
	}
	if !filter.StartTime.IsZero() && entry.Timestamp.Before(filter.StartTime) {
		return false
	}
	if !filter.EndTime.IsZero() && entry.Timestamp.After(filter.EndTime) {
		return false
	}

	return true
}

// paginate applies limit/offset to an already-filtered, already-ordered slice.
// total is the length before pagination. A negative limit means "no limit"
// (consistent with the database backend, which omits the LIMIT clause); a
// zero limit returns no rows.
func paginate(entries []AuditEntry, limit, offset int) ([]AuditEntry, int, error) {
	total := len(entries)

	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}

	end := total
	if limit >= 0 {
		end = offset + limit
		if end > total {
			end = total
		}
	}

	return entries[offset:end], total, nil
}

// computeStats aggregates event counts and the success rate over entries.
func computeStats(entries []AuditEntry) AuditStats {
	stats := AuditStats{
		TotalEntries: len(entries),
		EventCounts:  make(map[string]int),
	}

	successCount := 0
	for _, entry := range entries {
		stats.EventCounts[entry.Event]++
		if entry.Success {
			successCount++
		}
	}

	if len(entries) > 0 {
		stats.SuccessRate = float64(successCount) / float64(len(entries)) * PercentageMultiplier
	}

	return stats
}
