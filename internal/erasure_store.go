package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ADR-0035 (user erasure ledger) disposition for media-library-maintainer:
// candidates whose criteria_json names the erased user id are deleted, never
// anonymised in place. The next scan regenerates them from current data.
//
// The maintainer stores no tenant (candidates carry none), so the
// tombstone's tenant is recorded in erasure_applied but cannot be compared;
// user ids are globally unique and never reused by the provider.

// Counts keys reported to the identity provider (match [a-z0-9_.-]{1,64}).
const (
	countCandidates            = "candidates"
	countCandidatesUnparseable = "candidates_unparseable"
)

// criteriaUserRefs is the part of EvalContext (stored verbatim in
// candidates.criteria_json) that carries a user id: the requester and the
// per-user watch maps. Matching is exact string equality on the id.
type criteriaUserRefs struct {
	UserWatchedPercent         map[string]json.RawMessage
	UserWatchedDurationMinutes map[string]json.RawMessage
	RequestedBy                string
}

// criteriaReferencesUser reports whether a stored criteria document names
// userID as RequestedBy or as a key of a per-user watch map. A document that
// cannot be decoded is checked for the id as a whole JSON string token, so
// a corrupt row cannot keep an erased id alive; unparseable reports that
// path. Substrings and prefixes never match.
func criteriaReferencesUser(raw, userID string) (refs, unparseable bool) {
	if userID == "" {
		return false, false
	}
	var c criteriaUserRefs
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		token, merr := json.Marshal(userID)
		if merr != nil {
			return false, true
		}
		return strings.Contains(raw, string(token)), true
	}
	if c.RequestedBy == userID {
		return true, false
	}
	if _, ok := c.UserWatchedPercent[userID]; ok {
		return true, false
	}
	_, ok := c.UserWatchedDurationMinutes[userID]
	return ok, false
}

// criteriaNameErasedUser reports whether criteria names any id in erased.
func criteriaNameErasedUser(raw string, erased map[string]struct{}) bool {
	for id := range erased {
		if refs, _ := criteriaReferencesUser(raw, id); refs {
			return true
		}
	}
	return false
}

// erasedUserIDs returns the user ids with an erasure_applied record.
func erasedUserIDs(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}) (map[string]struct{}, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT user_id FROM erasure_applied`)
	if err != nil {
		return nil, fmt.Errorf("read erasure records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read erasure records: %w", err)
		}
		out[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read erasure records: %w", err)
	}
	return out, nil
}

// erasureApplied reports whether erasureID is recorded as applied.
func (m *Module) erasureApplied(ctx context.Context, erasureID string) (bool, error) {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return false, errors.New("maintainer database not initialized")
	}
	var one int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM erasure_applied WHERE erasure_id = ?`, erasureID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read erasure record: %w", err)
	}
	return true, nil
}

// eraseUser applies one erasure tombstone in ONE transaction: it deletes the
// candidates that name userID and records erasureID in erasure_applied. On
// any error nothing has changed. Calling it again for an applied erasure id
// is a no-op that returns the recorded counts.
//
// It holds m.mu for the whole transaction, as persistCandidates does, so a
// scan can neither interleave with it nor persist a candidate for the user
// between the delete and the record.
func (m *Module) eraseUser(ctx context.Context, erasureID, userID, tenantID string) (map[string]int64, error) {
	if erasureID == "" || userID == "" {
		return nil, errors.New("erasure id and user id are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return nil, errors.New("maintainer database not initialized")
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin erasure: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var prior string
	switch qerr := tx.QueryRowContext(ctx, `SELECT counts_json FROM erasure_applied WHERE erasure_id = ?`, erasureID).Scan(&prior); {
	case qerr == nil:
		counts := map[string]int64{}
		if jerr := json.Unmarshal([]byte(prior), &counts); jerr != nil {
			return nil, fmt.Errorf("decode recorded erasure counts: %w", jerr)
		}
		return counts, nil
	case !errors.Is(qerr, sql.ErrNoRows):
		return nil, fmt.Errorf("read erasure record: %w", qerr)
	}

	victims, unparseable, err := candidatesNamingUser(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	for _, id := range victims {
		if _, derr := tx.ExecContext(ctx, `DELETE FROM candidates WHERE id = ?`, id); derr != nil {
			return nil, fmt.Errorf("erase candidate: %w", derr)
		}
	}
	counts := map[string]int64{
		countCandidates:            int64(len(victims)),
		countCandidatesUnparseable: int64(unparseable),
	}
	rawCounts, err := json.Marshal(counts)
	if err != nil {
		return nil, fmt.Errorf("encode erasure counts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO erasure_applied (erasure_id, user_id, tenant_id, applied_at, counts_json)
		VALUES (?, ?, ?, ?, ?)`, erasureID, userID, tenantID, time.Now().UTC().Format(time.RFC3339Nano), string(rawCounts)); err != nil {
		return nil, fmt.Errorf("record erasure: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit erasure: %w", err)
	}
	return counts, nil
}

// candidatesNamingUser returns the ids of every candidate whose criteria
// names userID, and how many of those were matched through the unparseable
// fallback. All rows are read before any is returned: the database has a
// single connection.
func candidatesNamingUser(ctx context.Context, tx *sql.Tx, userID string) (ids []string, unparseable int, err error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, criteria_json FROM candidates`)
	if err != nil {
		return nil, 0, fmt.Errorf("read candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, 0, fmt.Errorf("read candidates: %w", err)
		}
		refs, bad := criteriaReferencesUser(raw, userID)
		if !refs {
			continue
		}
		ids = append(ids, id)
		if bad {
			unparseable++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read candidates: %w", err)
	}
	return ids, unparseable, nil
}

// countCandidatesNamingUser is the post-condition of eraseUser: candidates
// whose criteria still name userID. It must be 0 afterwards.
func (m *Module) countCandidatesNamingUser(ctx context.Context, userID string) (int, error) {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return 0, errors.New("maintainer database not initialized")
	}
	rows, err := db.QueryContext(ctx, `SELECT criteria_json FROM candidates`)
	if err != nil {
		return 0, fmt.Errorf("read candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return 0, fmt.Errorf("read candidates: %w", err)
		}
		if refs, _ := criteriaReferencesUser(raw, userID); refs {
			n++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read candidates: %w", err)
	}
	return n, nil
}
