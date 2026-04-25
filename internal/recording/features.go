package recording

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	apievents "github.com/gravitational/teleport/api/types/events"

	"teleport-ai/internal/store"
)

// ParserVersion is stamped on each upserted row. Bump when the feature
// extraction logic changes in a way that warrants a re-parse.
const ParserVersion = "teleport-analyze@0.1"

// idleGapThreshold is the inter-chunk gap above which we count an "idle
// gap." Two seconds is the working default; tune as classifier evidence
// accumulates.
const idleGapThresholdMs = 2000

type Features struct {
	Kind             string
	User             string
	Cluster          string
	StartedAt        time.Time
	EndedAt          time.Time
	PTYPresent       bool
	PrintChunks      int64
	PrintBytes       int64
	MedianChunkGapMs float64
	IdleGapCount     int64
	EditCharCount    int64
	CommandCount     int64
	BPFPresent       bool
	SingleShot       bool
	JoinCount        int64
	Interactive      bool
}

// Extract reads the entire ProtoStreamV1 stream and derives features +
// notable events. The caller owns r and is responsible for closing it.
func Extract(ctx context.Context, r io.Reader) (Features, []store.NotableEvent, error) {
	rd := newReader(r)
	var (
		feat        Features
		notable     []store.NotableEvent
		gaps        []float64
		lastPrintMs int64 = -1
		seenStart   bool
		startKnown  bool
	)
	for {
		ev, err := rd.next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return feat, notable, err
		}
		switch e := ev.(type) {
		case *apievents.SessionStart:
			seenStart = true
			feat.User = e.User
			feat.Cluster = e.ClusterName
			feat.Kind = inferKind(e)
			if !e.Time.IsZero() {
				feat.StartedAt = e.Time
				startKnown = true
			}
		case *apievents.SessionEnd:
			if !e.EndTime.IsZero() {
				feat.EndedAt = e.EndTime
			} else if !e.Time.IsZero() {
				feat.EndedAt = e.Time
			}
			if !startKnown && !e.StartTime.IsZero() {
				feat.StartedAt = e.StartTime
				startKnown = true
			}
			feat.Interactive = e.Interactive
			if feat.User == "" {
				feat.User = e.User
			}
		case *apievents.SessionPrint:
			feat.PTYPresent = true
			feat.PrintChunks++
			feat.PrintBytes += int64(len(e.Data))
			feat.EditCharCount += countEditChars(e.Data)
			if lastPrintMs >= 0 {
				gap := float64(e.DelayMilliseconds - lastPrintMs)
				if gap < 0 {
					gap = 0
				}
				gaps = append(gaps, gap)
				if gap > idleGapThresholdMs {
					feat.IdleGapCount++
				}
			}
			lastPrintMs = e.DelayMilliseconds
		case *apievents.SessionCommand:
			feat.BPFPresent = true
			feat.CommandCount++
			notable = append(notable, makeNotable(ev, "session.command", map[string]any{
				"path":        e.Path,
				"argv":        e.Argv,
				"return_code": e.ReturnCode,
				"ppid":        e.PPID,
			}))
		case *apievents.SessionJoin:
			feat.JoinCount++
			notable = append(notable, makeNotable(ev, "session.join", map[string]any{
				"user": e.User,
			}))
		case *apievents.Exec:
			notable = append(notable, makeNotable(ev, "session.exec", map[string]any{
				"command":   e.Command,
				"exit_code": e.ExitCode,
			}))
		}
	}
	_ = seenStart
	feat.MedianChunkGapMs = median(gaps)
	feat.SingleShot = isSingleShot(feat)
	return feat, notable, nil
}

// ApplyFeatures copies a Features into a store.Session, computing
// derived fields like duration_seconds.
func ApplyFeatures(s *store.Session, f Features) {
	if s.User == "" {
		s.User = f.User
	}
	if s.Cluster == "" {
		s.Cluster = f.Cluster
	}
	s.Kind = f.Kind
	if !f.StartedAt.IsZero() {
		s.StartedAt = f.StartedAt.UTC().Format(time.RFC3339)
	}
	if !f.EndedAt.IsZero() {
		s.EndedAt = f.EndedAt.UTC().Format(time.RFC3339)
	}
	if !f.StartedAt.IsZero() && !f.EndedAt.IsZero() {
		s.DurationSeconds = f.EndedAt.Sub(f.StartedAt).Seconds()
	}
	s.PTYPresent = f.PTYPresent
	s.PrintChunks = f.PrintChunks
	s.PrintBytes = f.PrintBytes
	s.MedianChunkGapMs = f.MedianChunkGapMs
	s.IdleGapCount = f.IdleGapCount
	s.EditCharCount = f.EditCharCount
	s.CommandCount = f.CommandCount
	s.BPFPresent = f.BPFPresent
	s.SingleShot = f.SingleShot
	s.JoinCount = f.JoinCount
}

func inferKind(s *apievents.SessionStart) string {
	if s.KubernetesCluster != "" {
		return "kube"
	}
	switch {
	case strings.HasPrefix(s.Type, "db."):
		return "db"
	case strings.HasPrefix(s.Type, "windows.desktop."), strings.HasPrefix(s.Type, "desktop."):
		return "desktop"
	case strings.HasPrefix(s.Type, "app."):
		return "app"
	default:
		return "ssh"
	}
}

// isSingleShot identifies non-PTY exec-style invocations: no PTY, very
// short, and no SessionJoin. This is the cohort the plan calls out as
// "looks agent-like by construction" (kubectl, scp, single tsh exec).
func isSingleShot(f Features) bool {
	if f.PTYPresent {
		return false
	}
	if f.JoinCount > 0 {
		return false
	}
	// SessionEnd.Interactive=false is the strongest single signal upstream
	// gives us. Fall back to "no SessionPrint and short duration."
	if f.Interactive {
		return false
	}
	return true
}

// countEditChars approximates "human is editing the line": backspace
// (0x7F or 0x08), ^W (0x17), and ANSI arrow / cursor escape sequences.
// Coding agents driving tsh typically emit none of these.
func countEditChars(data []byte) int64 {
	var n int64
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case 0x7F, 0x08, 0x17:
			n++
		case 0x1B: // ESC; check for CSI cursor sequences
			if i+2 < len(data) && data[i+1] == '[' {
				switch data[i+2] {
				case 'A', 'B', 'C', 'D', 'H', 'F': // arrows + Home/End
					n++
					i += 2
				}
			}
		}
	}
	return n
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func makeNotable(ev apievents.AuditEvent, eventType string, payload map[string]any) store.NotableEvent {
	js, err := json.Marshal(payload)
	if err != nil {
		js = []byte(fmt.Sprintf(`{"_marshal_error":%q}`, err.Error()))
	}
	t := ev.GetTime()
	return store.NotableEvent{
		EventTime: t.UTC().Format(time.RFC3339Nano),
		EventType: eventType,
		Payload:   string(js),
	}
}
