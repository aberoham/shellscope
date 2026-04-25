package store

import (
	"path/filepath"
	"testing"

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
