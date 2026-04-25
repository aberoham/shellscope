package store

import (
	"path/filepath"
	"testing"
	"time"

	"teleport-ai/internal/labels"
)

func TestUpsertAndLabelSelector(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "s.sqlite")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for i, sid := range []string{"sid-a", "sid-b", "sid-c"} {
		row := Session{
			SessionID:     sid,
			User:          "abe",
			Kind:          "ssh",
			UploadedAt:    "2026-04-25T10:00:00Z",
			PTYPresent:    i != 2,
			PrintChunks:   int64(10 * (i + 1)),
			ParsedAt:      "2026-04-25T11:00:00Z",
			ParserVersion: "test",
		}
		if err := st.UpsertSession(row); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.SetLabel("sid-a", "operator.type", "human", "test", "now"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetLabel("sid-b", "operator.type", "agent", "test", "now"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetLabel("sid-b", "agent.tool", "claude-code", "test", "now"); err != nil {
		t.Fatal(err)
	}
	// Re-stamping the same key updates in place rather than failing.
	if err := st.SetLabel("sid-b", "agent.tool", "codex", "test2", "now2"); err != nil {
		t.Fatal(err)
	}

	sel, _ := labels.ParseSelector("operator.type=agent,agent.tool=codex")
	rows, err := st.ListBySelector(sel)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SessionID != "sid-b" {
		t.Fatalf("selector match: got %+v", rows)
	}

	all, err := st.ListBySelector(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(all))
	}
}

func TestUpsertSession_DefaultsSubstrate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "s.sqlite")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Caller leaves Substrate empty — the Teleport flow.
	if err := st.UpsertSession(Session{
		SessionID: "sid-default", User: "abe",
		ParsedAt: "now", ParserVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	// Caller sets Substrate explicitly — should be honoured.
	if err := st.UpsertSession(Session{
		SessionID: "sid-explicit", User: "abe",
		ParsedAt: "now", ParserVersion: "test",
		Substrate: SubstrateTeleportRecording,
	}); err != nil {
		t.Fatal(err)
	}
	// Re-upsert should preserve substrate, not blank it back to NULL.
	if err := st.UpsertSession(Session{
		SessionID: "sid-default", User: "abe",
		ParsedAt: "later", ParserVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := st.db.Query(
		`SELECT session_id, substrate FROM sessions ORDER BY session_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var sid, sub string
		if err := rows.Scan(&sid, &sub); err != nil {
			t.Fatal(err)
		}
		got[sid] = sub
	}
	if got["sid-default"] != SubstrateTeleportRecording {
		t.Errorf("default substrate: got %q, want %q",
			got["sid-default"], SubstrateTeleportRecording)
	}
	if got["sid-explicit"] != SubstrateTeleportRecording {
		t.Errorf("explicit substrate: got %q, want %q",
			got["sid-explicit"], SubstrateTeleportRecording)
	}
}

func TestMigrate_BackfillsExistingRowsToTeleport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.sqlite")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Sneak a NULL substrate row past the upsert default to simulate
	// a row that was inserted before the substrate column existed.
	if _, err := st.db.Exec(`
		INSERT INTO sessions (session_id, user, parsed_at, parser_version, substrate)
		VALUES (?, ?, ?, ?, NULL)`,
		"old-row", "abe", "2026-04-25T10:00:00Z", "pre-substrate"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	// Reopen — migrate should backfill the NULL row.
	st2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	var sub string
	if err := st2.db.QueryRow(
		`SELECT substrate FROM sessions WHERE session_id = ?`, "old-row",
	).Scan(&sub); err != nil {
		t.Fatal(err)
	}
	if sub != SubstrateTeleportRecording {
		t.Errorf("backfill: got %q, want %q", sub, SubstrateTeleportRecording)
	}
}

// TestMergeOverlappingGCPSessions covers the reviewer's scenario in
// review C of 4a98166: a 60-minute continuous session pulled twice
// with different --since values yields two synthetic IDs (because
// each pull's first visible bucket sits past the bias). Merge must
// collapse them into the earliest-started canonical session, move
// minute_features and labels to the canonical, drop the duplicate
// session row, and recompute the canonical's aggregates.
func TestMergeOverlappingGCPSessions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "merge.sqlite")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	principal := "alice@example.com"
	idleThreshold := 10 * time.Minute

	// First pull: --since 10:00, full session captured 10:00..11:00.
	// 13 buckets at 5-min granularity (10:00, 10:05, ..., 11:00).
	mustUpsertGCP(t, st, "gcp-FIRST", principal, "2026-04-25T10:00:00Z", "2026-04-25T11:01:00Z")
	for i := 0; i <= 12; i++ {
		mustUpsertMinute(t, st, "gcp-FIRST",
			time.Date(2026, 4, 25, 10, i*5, 0, 0, time.UTC).Format(time.RFC3339))
	}
	// Stamp a label on the first session — must survive merge.
	if err := st.SetLabel("gcp-FIRST", "operator.type", "human", "phase1", "now"); err != nil {
		t.Fatal(err)
	}

	// Second pull: --since 10:30, bias=11m sees from 10:19 onward,
	// first visible bucket = 10:20. 9 buckets (10:20..11:00).
	// Different synthetic ID, partial bucket overlap with FIRST.
	mustUpsertGCP(t, st, "gcp-SECOND", principal, "2026-04-25T10:20:00Z", "2026-04-25T11:01:00Z")
	for i := 4; i <= 12; i++ {
		mustUpsertMinute(t, st, "gcp-SECOND",
			time.Date(2026, 4, 25, 10, i*5, 0, 0, time.UTC).Format(time.RFC3339))
	}
	// SECOND has a competing operator.type label that must NOT
	// overwrite the canonical (FIRST) one.
	if err := st.SetLabel("gcp-SECOND", "operator.type", "agent", "phase1-rerun", "now"); err != nil {
		t.Fatal(err)
	}
	// SECOND also has a unique label that should propagate to canonical.
	if err := st.SetLabel("gcp-SECOND", "gcp.ua.tool", "gcloud", "phase1-rerun", "now"); err != nil {
		t.Fatal(err)
	}

	// Sentinel: a different principal's session must be untouched.
	mustUpsertGCP(t, st, "gcp-OTHER", "bob@example.com",
		"2026-04-25T10:30:00Z", "2026-04-25T10:35:00Z")
	mustUpsertMinute(t, st, "gcp-OTHER", "2026-04-25T10:30:00Z")

	merged, err := st.MergeOverlappingGCPSessions(principal, idleThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if merged != 1 {
		t.Fatalf("merged count: want 1, got %d", merged)
	}

	// SECOND row gone.
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE session_id = ?`, "gcp-SECOND").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("absorbed session still present: %d rows", n)
	}

	// FIRST keeps the union of buckets (still 13 unique).
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM gcp_minute_features WHERE session_id = ?`, "gcp-FIRST").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 13 {
		t.Errorf("FIRST minute_features after merge: want 13, got %d", n)
	}

	// FIRST's started_at recomputed — should still be 10:00.
	var startedAt, endedAt string
	if err := st.db.QueryRow(
		`SELECT started_at, ended_at FROM sessions WHERE session_id = ?`, "gcp-FIRST",
	).Scan(&startedAt, &endedAt); err != nil {
		t.Fatal(err)
	}
	if startedAt != "2026-04-25T10:00:00Z" {
		t.Errorf("canonical started_at: got %q, want 2026-04-25T10:00:00Z", startedAt)
	}
	if endedAt != "2026-04-25T11:01:00Z" {
		t.Errorf("canonical ended_at: got %q, want 2026-04-25T11:01:00Z", endedAt)
	}

	// FIRST's operator.type label preserved (canonical wins).
	var labelValue string
	if err := st.db.QueryRow(
		`SELECT value FROM session_labels WHERE session_id = ? AND key = ?`,
		"gcp-FIRST", "operator.type",
	).Scan(&labelValue); err != nil {
		t.Fatal(err)
	}
	if labelValue != "human" {
		t.Errorf("operator.type after merge: got %q, want %q (canonical wins)", labelValue, "human")
	}

	// SECOND's gcp.ua.tool label propagated.
	if err := st.db.QueryRow(
		`SELECT value FROM session_labels WHERE session_id = ? AND key = ?`,
		"gcp-FIRST", "gcp.ua.tool",
	).Scan(&labelValue); err != nil {
		t.Fatal(err)
	}
	if labelValue != "gcloud" {
		t.Errorf("propagated label gcp.ua.tool: got %q, want gcloud", labelValue)
	}

	// Sentinel untouched.
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE session_id = ?`, "gcp-OTHER").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("sentinel disturbed: %d rows", n)
	}
}

func TestMergeOverlappingGCPSessions_NoOpWhenDisjoint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "disjoint.sqlite")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mustUpsertGCP(t, st, "gcp-A", "alice@example.com",
		"2026-04-25T10:00:00Z", "2026-04-25T10:05:00Z")
	// Gap is 30 min — well past idle threshold of 10 min.
	mustUpsertGCP(t, st, "gcp-B", "alice@example.com",
		"2026-04-25T10:35:00Z", "2026-04-25T10:40:00Z")

	merged, err := st.MergeOverlappingGCPSessions("alice@example.com", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if merged != 0 {
		t.Errorf("disjoint sessions should not merge; merged=%d", merged)
	}
}

func mustUpsertGCP(t *testing.T, st *Store, sid, principal, startedAt, endedAt string) {
	t.Helper()
	if err := st.UpsertGCPSession(GCPSession{
		SessionID:     sid,
		User:          principal,
		GCPPrincipal:  principal,
		StartedAt:     startedAt,
		EndedAt:       endedAt,
		UploadedAt:    endedAt,
		ParsedAt:      "2026-04-25T11:30:00Z",
		ParserVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
}

func mustUpsertMinute(t *testing.T, st *Store, sid, bucket string) {
	t.Helper()
	if err := st.UpsertGCPMinuteFeature(GCPMinuteFeature{
		SessionID:        sid,
		MinuteBucket:     bucket,
		CallCount:        1,
		DistinctServices: 1,
		DistinctMethods:  1,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceGCPMinuteFeatures_DropsStaleRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "g.sqlite")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Two separate sessions; the second is a sentinel that should be
	// untouched by replacing rows on the first.
	for _, sid := range []string{"gcp-aaa", "gcp-bbb"} {
		if err := st.UpsertGCPSession(GCPSession{
			SessionID:     sid,
			User:          "abe@example.com",
			GCPPrincipal:  "abe@example.com",
			ParsedAt:      "2026-04-25T11:00:00Z",
			ParserVersion: "test",
		}); err != nil {
			t.Fatal(err)
		}
	}

	wide := []GCPMinuteFeature{
		{MinuteBucket: "2026-04-25T10:00:00Z", CallCount: 3, DistinctServices: 1, DistinctMethods: 1},
		{MinuteBucket: "2026-04-25T10:01:00Z", CallCount: 5, DistinctServices: 1, DistinctMethods: 2},
		{MinuteBucket: "2026-04-25T10:02:00Z", CallCount: 7, DistinctServices: 2, DistinctMethods: 3},
	}
	tighter := []GCPMinuteFeature{
		{MinuteBucket: "2026-04-25T10:01:00Z", CallCount: 5, DistinctServices: 1, DistinctMethods: 2},
	}
	sentinel := []GCPMinuteFeature{
		{MinuteBucket: "2026-04-25T20:00:00Z", CallCount: 9, DistinctServices: 1, DistinctMethods: 1},
	}

	if err := st.ReplaceGCPMinuteFeatures("gcp-aaa", wide); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceGCPMinuteFeatures("gcp-bbb", sentinel); err != nil {
		t.Fatal(err)
	}
	// Re-run with a tighter bucket set — the two stale rows should
	// disappear, sentinel session unchanged.
	if err := st.ReplaceGCPMinuteFeatures("gcp-aaa", tighter); err != nil {
		t.Fatal(err)
	}

	count := func(sid string) int {
		var n int
		if err := st.db.QueryRow(
			`SELECT COUNT(*) FROM gcp_minute_features WHERE session_id=?`, sid,
		).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := count("gcp-aaa"); got != 1 {
		t.Errorf("gcp-aaa rows after tighter re-run: got %d, want 1", got)
	}
	if got := count("gcp-bbb"); got != 1 {
		t.Errorf("gcp-bbb sentinel disturbed: got %d rows, want 1", got)
	}
}

func TestReplaceNotable_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "n.sqlite")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertSession(Session{
		SessionID: "sid-x", User: "abe", ParsedAt: "now", ParserVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}

	notable := []NotableEvent{
		{EventTime: "2026-04-25T10:00:00Z", EventType: "session.command", Payload: `{"path":"/usr/bin/ls"}`},
		{EventTime: "2026-04-25T10:00:01Z", EventType: "session.command", Payload: `{"path":"/usr/bin/cat"}`},
	}

	for i := 0; i < 3; i++ {
		if err := st.ReplaceNotable("sid-x", notable); err != nil {
			t.Fatalf("ReplaceNotable iter %d: %v", i, err)
		}
	}

	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM notable_events WHERE session_id=?`, "sid-x").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("after 3 ReplaceNotable calls, got %d rows; want 2 (idempotent)", n)
	}
}
