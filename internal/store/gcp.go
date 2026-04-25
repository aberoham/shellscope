// GCP-side session writes. Targets the same `sessions` table as the
// Teleport-side flow but only populates the cross-substrate columns
// plus the gcp_* extension columns. Teleport-only columns stay NULL.
package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"teleport-ai/internal/labels"
)

// GCPSession is the session row a GCP-side pull writes. SessionID is
// synthetic (see internal/synthsess); User is the principalEmail.
type GCPSession struct {
	SessionID             string
	User                  string  // principalEmail
	StartedAt             string  // first minute_bucket of the synthetic session, RFC3339
	EndedAt               string  // last minute_bucket + 1 minute, RFC3339
	UploadedAt            string  // mirrors EndedAt; satisfies idx_sessions_uploaded
	DurationSeconds       float64 // EndedAt - StartedAt, seconds
	Cluster               string  // optional: the GCP project_id sample, if available
	GCPPrincipal          string  // principalEmail (same as User; kept for clarity)
	GCPUASample           string  // sample callerSuppliedUserAgent
	GCPCallerIP           string  // sample callerIp
	GCPCallCount          int64
	GCPDistinctServices   int64
	GCPDistinctMethods    int64
	GCPImpersonationCalls int64
	GCPDeniedCalls        int64
	GCPMinuteBuckets      int64
	GCPMedianCallGapMs    float64
	ParsedAt              string
	ParserVersion         string
	ParseError            string
}

// GCPMinuteFeature is the per-minute feature row.
// top_services_json / top_methods_json are pre-rendered JSON strings
// from APPROX_TOP_COUNT.
type GCPMinuteFeature struct {
	SessionID          string
	MinuteBucket       string
	CallCount          int64
	DistinctServices   int64
	DistinctMethods    int64
	ImpersonationCalls int64
	DeniedCalls        int64
	TopServicesJSON    string
	TopMethodsJSON     string
}

const upsertGCPSessionSQL = `
INSERT INTO sessions (
  session_id, user, cluster, started_at, ended_at, uploaded_at,
  duration_seconds, parsed_at, parser_version, parse_error,
  substrate, gcp_principal, gcp_ua_sample, gcp_caller_ip,
  gcp_call_count, gcp_distinct_services, gcp_distinct_methods,
  gcp_impersonation_calls, gcp_denied_calls, gcp_minute_buckets,
  gcp_median_call_gap_ms
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(session_id) DO UPDATE SET
  user                    = excluded.user,
  cluster                 = excluded.cluster,
  started_at              = excluded.started_at,
  ended_at                = excluded.ended_at,
  uploaded_at             = excluded.uploaded_at,
  duration_seconds        = excluded.duration_seconds,
  parsed_at               = excluded.parsed_at,
  parser_version          = excluded.parser_version,
  parse_error             = excluded.parse_error,
  substrate               = excluded.substrate,
  gcp_principal           = excluded.gcp_principal,
  gcp_ua_sample           = excluded.gcp_ua_sample,
  gcp_caller_ip           = excluded.gcp_caller_ip,
  gcp_call_count          = excluded.gcp_call_count,
  gcp_distinct_services   = excluded.gcp_distinct_services,
  gcp_distinct_methods    = excluded.gcp_distinct_methods,
  gcp_impersonation_calls = excluded.gcp_impersonation_calls,
  gcp_denied_calls        = excluded.gcp_denied_calls,
  gcp_minute_buckets      = excluded.gcp_minute_buckets,
  gcp_median_call_gap_ms  = excluded.gcp_median_call_gap_ms
`

func (s *Store) UpsertGCPSession(r GCPSession) error {
	_, err := s.db.Exec(upsertGCPSessionSQL,
		r.SessionID,
		r.User,
		nullable(r.Cluster),
		nullable(r.StartedAt),
		nullable(r.EndedAt),
		nullable(r.UploadedAt),
		nullableFloat(r.DurationSeconds),
		r.ParsedAt,
		r.ParserVersion,
		nullable(r.ParseError),
		SubstrateGCPCloudAudit,
		nullable(r.GCPPrincipal),
		nullable(r.GCPUASample),
		nullable(r.GCPCallerIP),
		nullableInt(r.GCPCallCount),
		nullableInt(r.GCPDistinctServices),
		nullableInt(r.GCPDistinctMethods),
		nullableInt(r.GCPImpersonationCalls),
		nullableInt(r.GCPDeniedCalls),
		nullableInt(r.GCPMinuteBuckets),
		nullableFloat(r.GCPMedianCallGapMs),
	)
	if err != nil {
		return fmt.Errorf("upsert gcp %s: %w", r.SessionID, err)
	}
	return nil
}

const upsertGCPMinuteFeatureSQL = `
INSERT INTO gcp_minute_features (
  session_id, minute_bucket, call_count, distinct_services,
  distinct_methods, impersonation_calls, denied_calls,
  top_services_json, top_methods_json
) VALUES (?,?,?,?,?,?,?,?,?)
ON CONFLICT(session_id, minute_bucket) DO UPDATE SET
  call_count          = excluded.call_count,
  distinct_services   = excluded.distinct_services,
  distinct_methods    = excluded.distinct_methods,
  impersonation_calls = excluded.impersonation_calls,
  denied_calls        = excluded.denied_calls,
  top_services_json   = excluded.top_services_json,
  top_methods_json    = excluded.top_methods_json
`

// ListGCPSessionsBySelector returns GCP-substrate sessions whose
// labels satisfy every requirement in the selector. An empty
// selector returns every GCP session. Always filters to
// substrate = SubstrateGCPCloudAudit so callers don't accidentally
// see Teleport rows in GCP-flavoured output.
func (s *Store) ListGCPSessionsBySelector(sel labels.Selector) ([]GCPSession, error) {
	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`SELECT session_id, user, started_at, ended_at, uploaded_at,
       gcp_principal, gcp_ua_sample, gcp_caller_ip,
       gcp_call_count, gcp_distinct_services, gcp_distinct_methods,
       gcp_impersonation_calls, gcp_denied_calls, gcp_minute_buckets,
       gcp_median_call_gap_ms
FROM sessions s WHERE substrate = ?`)
	args = append(args, SubstrateGCPCloudAudit)
	for i, r := range sel {
		alias := fmt.Sprintf("l%d", i)
		fmt.Fprintf(&query,
			" AND EXISTS(SELECT 1 FROM session_labels %s WHERE %s.session_id=s.session_id AND %s.key=? AND %s.value=?)",
			alias, alias, alias, alias)
		args = append(args, r.Key, r.Value)
	}
	query.WriteString(" ORDER BY started_at DESC")
	rows, err := s.db.Query(query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list gcp: %w", err)
	}
	defer rows.Close()
	var out []GCPSession
	for rows.Next() {
		var (
			r                                                                     GCPSession
			startedAt, endedAt, uploadedAt, principal, uaSample, callerIP         sql.NullString
			callCount, distSvc, distMethod, impCalls, denCalls, minuteBuckets     sql.NullInt64
			medianGap                                                             sql.NullFloat64
		)
		if err := rows.Scan(&r.SessionID, &r.User,
			&startedAt, &endedAt, &uploadedAt,
			&principal, &uaSample, &callerIP,
			&callCount, &distSvc, &distMethod,
			&impCalls, &denCalls, &minuteBuckets, &medianGap); err != nil {
			return nil, err
		}
		r.StartedAt = startedAt.String
		r.EndedAt = endedAt.String
		r.UploadedAt = uploadedAt.String
		r.GCPPrincipal = principal.String
		r.GCPUASample = uaSample.String
		r.GCPCallerIP = callerIP.String
		r.GCPCallCount = callCount.Int64
		r.GCPDistinctServices = distSvc.Int64
		r.GCPDistinctMethods = distMethod.Int64
		r.GCPImpersonationCalls = impCalls.Int64
		r.GCPDeniedCalls = denCalls.Int64
		r.GCPMinuteBuckets = minuteBuckets.Int64
		r.GCPMedianCallGapMs = medianGap.Float64
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpsertGCPMinuteFeature(f GCPMinuteFeature) error {
	_, err := s.db.Exec(upsertGCPMinuteFeatureSQL,
		f.SessionID, f.MinuteBucket,
		f.CallCount, f.DistinctServices, f.DistinctMethods,
		f.ImpersonationCalls, f.DeniedCalls,
		nullable(f.TopServicesJSON), nullable(f.TopMethodsJSON),
	)
	if err != nil {
		return fmt.Errorf("upsert gcp_minute_feature %s/%s: %w",
			f.SessionID, f.MinuteBucket, err)
	}
	return nil
}

// MergeOverlappingGCPSessions finds GCP-substrate sessions for a
// principal whose [started_at, ended_at] intervals overlap (or are
// within idleThreshold of each other) and collapses each such
// group into the earliest-started canonical session.
//
// This exists because the synthetic session_id is derived from
// the *first observed bucket*, which depends on the BigQuery
// window of the pull that produced it. Overlapping re-pulls of a
// long continuous session can therefore produce two different
// IDs for the same activity. Bias-back in gcpcli/pull.go covers
// short sessions; this merger covers the long-session case.
//
// Returns the number of non-canonical sessions absorbed.
//
// All work happens in one transaction per principal. Existing
// labels on the canonical session win on key conflict
// (INSERT OR IGNORE). Derived gcp_* aggregates on the canonical
// session are recomputed from the merged gcp_minute_features.
func (s *Store) MergeOverlappingGCPSessions(principal string, idleThreshold time.Duration) (int, error) {
	type sessRow struct {
		id        string
		startedAt time.Time
		endedAt   time.Time
	}
	rows, err := s.db.Query(`
SELECT session_id, started_at, ended_at
FROM sessions
WHERE substrate = ? AND user = ?
  AND started_at IS NOT NULL AND ended_at IS NOT NULL
ORDER BY started_at ASC`,
		SubstrateGCPCloudAudit, principal)
	if err != nil {
		return 0, fmt.Errorf("merge list %s: %w", principal, err)
	}
	var sessions []sessRow
	for rows.Next() {
		var sid, started, ended string
		if err := rows.Scan(&sid, &started, &ended); err != nil {
			rows.Close()
			return 0, err
		}
		st, err1 := time.Parse(time.RFC3339, started)
		en, err2 := time.Parse(time.RFC3339, ended)
		if err1 != nil || err2 != nil {
			continue // skip rows we can't interpret rather than failing the whole merge
		}
		sessions = append(sessions, sessRow{id: sid, startedAt: st, endedAt: en})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(sessions) < 2 {
		return 0, nil
	}

	// Group adjacent sessions whose start lies within idleThreshold
	// of any prior session in the group's running max ended_at.
	type group []sessRow
	var groups []group
	var cur group
	var runningMax time.Time
	for _, ss := range sessions {
		if len(cur) == 0 {
			cur = group{ss}
			runningMax = ss.endedAt
			continue
		}
		if !ss.startedAt.After(runningMax.Add(idleThreshold)) {
			cur = append(cur, ss)
			if ss.endedAt.After(runningMax) {
				runningMax = ss.endedAt
			}
			continue
		}
		groups = append(groups, cur)
		cur = group{ss}
		runningMax = ss.endedAt
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}

	merged := 0
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		canonical := g[0].id
		var others []string
		for _, ss := range g[1:] {
			others = append(others, ss.id)
		}
		if err := s.mergeOneGCPGroup(canonical, others); err != nil {
			return merged, err
		}
		merged += len(others)
	}
	return merged, nil
}

// mergeOneGCPGroup performs the actual data movement for a single
// merge group: re-key children to canonical, delete the absorbed
// session rows, and recompute canonical's derived aggregates.
func (s *Store) mergeOneGCPGroup(canonical string, others []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("merge begin: %w", err)
	}
	defer tx.Rollback()

	for _, other := range others {
		// Copy children to canonical, ignoring PK conflicts.
		// minute_features: PRIMARY KEY (session_id, minute_bucket).
		if _, err := tx.Exec(`
INSERT OR IGNORE INTO gcp_minute_features
  (session_id, minute_bucket, call_count, distinct_services,
   distinct_methods, impersonation_calls, denied_calls,
   top_services_json, top_methods_json)
SELECT ?, minute_bucket, call_count, distinct_services,
       distinct_methods, impersonation_calls, denied_calls,
       top_services_json, top_methods_json
FROM gcp_minute_features WHERE session_id = ?`, canonical, other); err != nil {
			return fmt.Errorf("merge minute_features %s→%s: %w", other, canonical, err)
		}
		// session_labels: PRIMARY KEY (session_id, key). IGNORE so
		// canonical's existing labels for the same key win.
		if _, err := tx.Exec(`
INSERT OR IGNORE INTO session_labels
  (session_id, key, value, set_by, set_at)
SELECT ?, key, value, set_by, set_at
FROM session_labels WHERE session_id = ?`, canonical, other); err != nil {
			return fmt.Errorf("merge labels %s→%s: %w", other, canonical, err)
		}
		// notable_events: no UNIQUE constraint; UPDATE works without
		// risk of conflicts. (Currently the GCP path does not write
		// notable_events; this is here for forward-compat.)
		if _, err := tx.Exec(
			`UPDATE notable_events SET session_id = ? WHERE session_id = ?`,
			canonical, other,
		); err != nil {
			return fmt.Errorf("merge notable %s→%s: %w", other, canonical, err)
		}
		// Now drop the absorbed session row. The ON DELETE CASCADE
		// on each child FK cleans up the now-stale duplicate rows
		// that we copied from but didn't move (the IGNORE branch).
		if _, err := tx.Exec(
			`DELETE FROM sessions WHERE session_id = ?`, other,
		); err != nil {
			return fmt.Errorf("merge delete %s: %w", other, err)
		}
	}

	// Recompute canonical's derived aggregates from the merged
	// gcp_minute_features set.
	if err := recomputeGCPSessionAggregates(tx, canonical); err != nil {
		return fmt.Errorf("merge recompute %s: %w", canonical, err)
	}
	return tx.Commit()
}

// recomputeGCPSessionAggregates rewrites the canonical session's
// started_at/ended_at and gcp_* aggregate columns from the
// gcp_minute_features rows currently keyed to it. Call inside the
// merge transaction.
func recomputeGCPSessionAggregates(tx *sql.Tx, canonical string) error {
	bucketRows, err := tx.Query(
		`SELECT minute_bucket, call_count, distinct_services, distinct_methods,
		        impersonation_calls, denied_calls
		 FROM gcp_minute_features WHERE session_id = ? ORDER BY minute_bucket ASC`,
		canonical,
	)
	if err != nil {
		return err
	}
	defer bucketRows.Close()

	var (
		buckets                                              []time.Time
		callSum, distSvcMax, distMethMax, impSum, denySum    int64
	)
	for bucketRows.Next() {
		var (
			bucket                                       string
			calls, distSvc, distMeth, impCalls, denCalls int64
		)
		if err := bucketRows.Scan(&bucket, &calls, &distSvc, &distMeth, &impCalls, &denCalls); err != nil {
			return err
		}
		t, err := time.Parse(time.RFC3339, bucket)
		if err != nil {
			continue
		}
		buckets = append(buckets, t)
		callSum += calls
		if distSvc > distSvcMax {
			distSvcMax = distSvc
		}
		if distMeth > distMethMax {
			distMethMax = distMeth
		}
		impSum += impCalls
		denySum += denCalls
	}
	if err := bucketRows.Err(); err != nil {
		return err
	}
	if len(buckets) == 0 {
		return nil // nothing to recompute against
	}

	startedAt := buckets[0]
	endedAt := buckets[len(buckets)-1].Add(time.Minute)
	duration := endedAt.Sub(startedAt).Seconds()
	medianGapMs := medianGapMillis(buckets)

	_, err = tx.Exec(`
UPDATE sessions SET
  started_at              = ?,
  ended_at                = ?,
  uploaded_at             = ?,
  duration_seconds        = ?,
  gcp_call_count          = ?,
  gcp_distinct_services   = ?,
  gcp_distinct_methods    = ?,
  gcp_impersonation_calls = ?,
  gcp_denied_calls        = ?,
  gcp_minute_buckets      = ?,
  gcp_median_call_gap_ms  = ?
WHERE session_id = ?`,
		startedAt.UTC().Format(time.RFC3339),
		endedAt.UTC().Format(time.RFC3339),
		endedAt.UTC().Format(time.RFC3339),
		duration,
		callSum, distSvcMax, distMethMax, impSum, denySum,
		int64(len(buckets)), medianGapMs,
		canonical,
	)
	return err
}

// medianGapMillis is the median pairwise gap between adjacent
// buckets, in milliseconds. Mirrors synthsess.build's calculation
// so post-merge values match what a clean single-pull would have
// produced.
func medianGapMillis(buckets []time.Time) float64 {
	if len(buckets) < 2 {
		return 0
	}
	gaps := make([]float64, 0, len(buckets)-1)
	for i := 1; i < len(buckets); i++ {
		gaps = append(gaps, float64(buckets[i].Sub(buckets[i-1]).Milliseconds()))
	}
	sort.Float64s(gaps)
	mid := len(gaps) / 2
	if len(gaps)%2 == 0 {
		return (gaps[mid-1] + gaps[mid]) / 2
	}
	return gaps[mid]
}

// ReplaceGCPMinuteFeatures wipes any prior per-minute feature rows
// for sessionID and inserts the supplied set in one transaction.
// Use this rather than bare UpsertGCPMinuteFeature when re-running
// `pull` over an overlapping date range — otherwise a re-pull that
// sees fewer buckets (because the date range was tighter) would
// leave stale rows that no longer reflect the synthesised session.
func (s *Store) ReplaceGCPMinuteFeatures(sessionID string, features []GCPMinuteFeature) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM gcp_minute_features WHERE session_id=?`, sessionID,
	); err != nil {
		return fmt.Errorf("delete gcp_minute_features %s: %w", sessionID, err)
	}
	stmt, err := tx.Prepare(`
INSERT INTO gcp_minute_features (
  session_id, minute_bucket, call_count, distinct_services,
  distinct_methods, impersonation_calls, denied_calls,
  top_services_json, top_methods_json
) VALUES (?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return fmt.Errorf("prepare gcp_minute_features insert: %w", err)
	}
	defer stmt.Close()
	for _, f := range features {
		if _, err := stmt.Exec(
			sessionID, f.MinuteBucket,
			f.CallCount, f.DistinctServices, f.DistinctMethods,
			f.ImpersonationCalls, f.DeniedCalls,
			nullable(f.TopServicesJSON), nullable(f.TopMethodsJSON),
		); err != nil {
			return fmt.Errorf("insert gcp_minute_feature %s/%s: %w",
				sessionID, f.MinuteBucket, err)
		}
	}
	return tx.Commit()
}
