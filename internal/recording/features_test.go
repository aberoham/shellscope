package recording

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"testing"
	"time"

	apievents "github.com/gravitational/teleport/api/types/events"
)

// writeStream serialises events into a single ProtoStreamV1 part. Mirror
// of upstream-repo/lib/events/stream.go's writer side, kept tiny.
func writeStream(t *testing.T, events []apievents.AuditEvent) []byte {
	t.Helper()
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	for _, ev := range events {
		oneof, err := apievents.ToOneOf(ev)
		if err != nil {
			t.Fatalf("ToOneOf: %v", err)
		}
		buf, err := oneof.Marshal()
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var sizeBuf [4]byte
		binary.BigEndian.PutUint32(sizeBuf[:], uint32(len(buf)))
		if _, err := gz.Write(sizeBuf[:]); err != nil {
			t.Fatal(err)
		}
		if _, err := gz.Write(buf); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var hdr [24]byte
	binary.BigEndian.PutUint64(hdr[0:8], 1) // version
	binary.BigEndian.PutUint64(hdr[8:16], uint64(body.Len()))
	binary.BigEndian.PutUint64(hdr[16:24], 0) // padding
	out.Write(hdr[:])
	out.Write(body.Bytes())
	return out.Bytes()
}

func TestExtract_HumanLikeSSH(t *testing.T) {
	start := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	end := start.Add(45 * time.Second)
	mkPrint := func(idx int64, delayMs int64, data []byte) *apievents.SessionPrint {
		return &apievents.SessionPrint{
			Metadata:          apievents.Metadata{Index: idx, Type: "print", Time: start.Add(time.Duration(delayMs) * time.Millisecond)},
			ChunkIndex:        idx,
			Data:              data,
			Bytes:             int64(len(data)),
			DelayMilliseconds: delayMs,
		}
	}
	events := []apievents.AuditEvent{
		&apievents.SessionStart{
			Metadata:        apievents.Metadata{Index: 0, Type: "session.start", Time: start, ClusterName: "test.cluster"},
			UserMetadata:    apievents.UserMetadata{User: "abe"},
			SessionMetadata: apievents.SessionMetadata{SessionID: "sid-human"},
		},
		mkPrint(1, 0, []byte("ls\r\n")),
		// Simulated typing with backspace (0x7F) + cursor up arrow (ESC [ A)
		mkPrint(2, 250, []byte{'l', 's', 0x7F, 's', 0x1B, '[', 'A'}),
		// Long idle then more input — should bump idle_gap_count
		mkPrint(3, 5500, []byte("echo done\r\n")),
		&apievents.SessionEnd{
			Metadata:        apievents.Metadata{Index: 4, Type: "session.end", Time: end},
			UserMetadata:    apievents.UserMetadata{User: "abe"},
			SessionMetadata: apievents.SessionMetadata{SessionID: "sid-human"},
			StartTime:       start,
			EndTime:         end,
			Interactive:     true,
		},
	}

	stream := writeStream(t, events)
	feat, _, err := Extract(context.Background(), bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !feat.PTYPresent {
		t.Error("expected PTYPresent=true")
	}
	if feat.PrintChunks != 3 {
		t.Errorf("PrintChunks=%d want 3", feat.PrintChunks)
	}
	if feat.IdleGapCount != 1 {
		t.Errorf("IdleGapCount=%d want 1", feat.IdleGapCount)
	}
	if feat.EditCharCount < 2 {
		t.Errorf("EditCharCount=%d want >=2 (backspace + arrow up)", feat.EditCharCount)
	}
	if feat.Kind != "ssh" {
		t.Errorf("Kind=%q want ssh", feat.Kind)
	}
	if feat.SingleShot {
		t.Error("interactive PTY session must not be SingleShot")
	}
	if !feat.StartedAt.Equal(start) {
		t.Errorf("StartedAt=%v want %v", feat.StartedAt, start)
	}
	if !feat.EndedAt.Equal(end) {
		t.Errorf("EndedAt=%v want %v", feat.EndedAt, end)
	}
}

func TestExtract_AgentLikeSingleShotKube(t *testing.T) {
	start := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)
	events := []apievents.AuditEvent{
		&apievents.SessionStart{
			Metadata:                  apievents.Metadata{Index: 0, Type: "session.start", Time: start},
			UserMetadata:              apievents.UserMetadata{User: "bot"},
			SessionMetadata:           apievents.SessionMetadata{SessionID: "sid-bot"},
			KubernetesClusterMetadata: apievents.KubernetesClusterMetadata{KubernetesCluster: "prod-east"},
		},
		&apievents.SessionEnd{
			Metadata:        apievents.Metadata{Index: 1, Type: "session.end", Time: end},
			SessionMetadata: apievents.SessionMetadata{SessionID: "sid-bot"},
			StartTime:       start,
			EndTime:         end,
			Interactive:     false,
		},
	}
	stream := writeStream(t, events)
	feat, _, err := Extract(context.Background(), bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if feat.Kind != "kube" {
		t.Errorf("Kind=%q want kube", feat.Kind)
	}
	if feat.PTYPresent {
		t.Error("expected PTYPresent=false")
	}
	if !feat.SingleShot {
		t.Error("expected SingleShot=true")
	}
}
