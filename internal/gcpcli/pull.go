package gcpcli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"teleport-ai/internal/bigquery"
	"teleport-ai/internal/store"
	"teleport-ai/internal/synthsess"
	"teleport-ai/internal/uafingerprint"
)

const dateLayout = "2006-01-02"

func newPullCmd() *cobra.Command {
	var (
		since          string
		until          string
		principal      string
		idleSeconds    int
		billingProject string
		auditProject   string
		dataset        string
		table          string
		bytesCap       int64
		location       string
		stampLabels    bool
	)
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Aggregate GCP audit logs into synthetic sessions in the local SQLite",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if since == "" || until == "" {
				return errors.New("--since and --until are required")
			}
			s, err := time.Parse(dateLayout, since)
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}
			u, err := time.Parse(dateLayout, until)
			if err != nil {
				return fmt.Errorf("--until: %w", err)
			}
			// Make --until inclusive by extending to end-of-day UTC.
			u = u.Add(24*time.Hour - time.Nanosecond)

			// Bias the BQ window backward by idle_threshold + 1m so a
			// session whose first bucket is just before --since still
			// gets its lead-in captured. Without this, a re-pull that
			// starts later than the original would synthesise a new
			// (different) session ID for the same activity, leaving the
			// old row stale. Sessions whose latest bucket falls before
			// --since are filtered out below — the bias is only there
			// to stabilise boundaries, not to widen results.
			idleThreshold := time.Duration(idleSeconds) * time.Second
			bias := idleThreshold + time.Minute
			queryStart := s.Add(-bias)

			ctx := cmd.Context()
			dbPath, _ := cmd.Flags().GetString("db")
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer st.Close()

			bq, err := bigquery.New(ctx, bigquery.Config{
				BillingProject: billingProject,
				AuditProject:   auditProject,
				Dataset:        dataset,
				Table:          table,
				BytesScanCap:   bytesCap,
				Location:       location,
			})
			if err != nil {
				return err
			}
			defer bq.Close()

			cmd.Printf("bq: querying %s.%s.%s [%s, %s] (bias=%s) principal=%q\n",
				auditProject, dataset, table,
				queryStart.Format(time.RFC3339), u.Format(time.RFC3339),
				bias, principal)

			rows, err := bq.QueryMinuteFeatures(ctx, queryStart, u, principal)
			if err != nil {
				return err
			}
			cmd.Printf("bq: %d (principal, minute) feature rows\n", len(rows))

			allSessions := synthsess.Synthesise(rows, idleThreshold)
			// Drop bias-zone-only sessions: keep only those with
			// activity actually within [s, u].
			sessions := allSessions[:0]
			for _, sess := range allSessions {
				if sess.EndedAt.After(s) {
					sessions = append(sessions, sess)
				}
			}
			cmd.Printf("synth: %d sessions (%d filtered as bias-only)\n",
				len(sessions), len(allSessions)-len(sessions))

			now := time.Now().UTC().Format(time.RFC3339)
			seenPrincipals := make(map[string]struct{}, len(sessions))
			for _, sess := range sessions {
				ses := store.GCPSession{
					SessionID:             sess.SessionID,
					User:                  sess.Principal,
					StartedAt:             sess.StartedAt.UTC().Format(time.RFC3339),
					EndedAt:               sess.EndedAt.UTC().Format(time.RFC3339),
					UploadedAt:            sess.EndedAt.UTC().Format(time.RFC3339),
					DurationSeconds:       sess.EndedAt.Sub(sess.StartedAt).Seconds(),
					GCPPrincipal:          sess.Principal,
					GCPUASample:           sess.SampleUA,
					GCPCallerIP:           sess.SampleIP,
					GCPCallCount:          sess.CallCount,
					GCPDistinctServices:   sess.DistinctServicesM,
					GCPDistinctMethods:    sess.DistinctMethodsM,
					GCPImpersonationCalls: sess.ImpersonationCalls,
					GCPDeniedCalls:        sess.DeniedCalls,
					GCPMinuteBuckets:      int64(len(sess.Buckets)),
					GCPMedianCallGapMs:    sess.MedianCallGapMs,
					ParsedAt:              now,
					ParserVersion:         parserVersionGCP,
				}
				if err := st.UpsertGCPSession(ses); err != nil {
					return err
				}
				features := make([]store.GCPMinuteFeature, 0, len(sess.Buckets))
				for _, b := range sess.Buckets {
					features = append(features, store.GCPMinuteFeature{
						SessionID:          sess.SessionID,
						MinuteBucket:       b.MinuteBucket.UTC().Format(time.RFC3339),
						CallCount:          b.CallCount,
						DistinctServices:   b.DistinctServices,
						DistinctMethods:    b.DistinctMethods,
						ImpersonationCalls: b.ImpersonationCalls,
						DeniedCalls:        b.DeniedCalls,
						TopServicesJSON:    b.TopServicesJSON,
						TopMethodsJSON:     b.TopMethodsJSON,
					})
				}
				// Replace, not upsert: a re-pull with a tighter range
				// (or different bias) might see fewer buckets for this
				// synthetic session, and stale per-minute rows would
				// otherwise survive.
				if err := st.ReplaceGCPMinuteFeatures(sess.SessionID, features); err != nil {
					return err
				}
				if stampLabels {
					if err := stampPhase1Labels(st, ses, sess); err != nil {
						return err
					}
				}
				seenPrincipals[sess.Principal] = struct{}{}
			}
			// Post-pull merge: bias-back stabilises boundaries for
			// short sessions, but a continuous session longer than
			// idle_threshold can still split across overlapping
			// re-pulls (the second pull's earliest visible bucket
			// becomes a different first_bucket → different ID).
			// Merge here collapses any such overlap into the
			// earliest-started canonical session.
			for principal := range seenPrincipals {
				n, err := st.MergeOverlappingGCPSessions(principal, idleThreshold)
				if err != nil {
					return err
				}
				if n > 0 {
					cmd.Printf("merge: absorbed %d overlapping session(s) for %s\n", n, principal)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "start date YYYY-MM-DD (inclusive)")
	cmd.Flags().StringVar(&until, "until", "", "end date YYYY-MM-DD (inclusive)")
	cmd.Flags().StringVar(&principal, "principal", "", "filter to a single principalEmail")
	cmd.Flags().IntVar(&idleSeconds, "idle-threshold-seconds", defaultIdleSeconds,
		"max gap (s) between adjacent minute buckets that still counts as the same session")
	cmd.Flags().StringVar(&billingProject, "billing-project", "",
		"GCP project that owns the BQ job (where bytes scanned are billed)")
	cmd.Flags().StringVar(&auditProject, "audit-project", "",
		"project hosting the aggregated audit dataset (FFF logging-aggregation project)")
	cmd.Flags().StringVar(&dataset, "dataset", "",
		"BigQuery dataset name (e.g. organization_audit_logs)")
	cmd.Flags().StringVar(&table, "table", defaultAuditTable,
		"BigQuery table within the dataset")
	cmd.Flags().Int64Var(&bytesCap, "bytes-cap", 0,
		"per-query bytes-billed cap (0 = unset; recommend setting in prod)")
	cmd.Flags().StringVar(&location, "location", "",
		"BigQuery dataset location override (e.g. US, EU, us-central1)")
	cmd.Flags().BoolVar(&stampLabels, "stamp-labels", true,
		"stamp phase-1 labels (substrate.kind, gcp.principal.type, gcp.ua.tool, routing.cohort)")
	_ = cmd.MarkFlagRequired("billing-project")
	_ = cmd.MarkFlagRequired("audit-project")
	_ = cmd.MarkFlagRequired("dataset")
	return cmd
}

// stampPhase1Labels writes the cheap, no-LLM labels that step-3 is
// allowed to override. The labels are deterministic functions of the
// synthesised session so re-runs produce stable results.
func stampPhase1Labels(st *store.Store, ses store.GCPSession, sess synthsess.Session) error {
	now := time.Now().UTC().Format(time.RFC3339)
	setBy := "gcpcli-phase1@" + parserVersionGCP

	labels := map[string]string{
		"substrate.kind":            store.SubstrateGCPCloudAudit,
		"gcp.session.synthesised":   "true",
		"gcp.principal.type":        principalType(sess.Principal),
		"gcp.ua.tool":               uafingerprint.Classify(sess.SampleUA),
		"gcp.impersonation.depth":   itoa(int(impDepthHint(sess))),
		"gcp.denials.count":         itoa(int(sess.DeniedCalls)),
		"routing.cohort":            cohortFor(sess),
	}
	for k, v := range labels {
		if v == "" {
			continue
		}
		if err := st.SetLabel(ses.SessionID, k, v, setBy, now); err != nil {
			return err
		}
	}
	return nil
}

// principalType maps a principalEmail shape to the
// gcp.principal.type label value.
func principalType(p string) string {
	switch {
	case p == "":
		return "unknown"
	case hasSuffix(p, ".iam.gserviceaccount.com"):
		return "service-account"
	case hasPrefix(p, "principal://") && contains(p, "/workforcePools/"):
		return "workforce-federation"
	case hasPrefix(p, "principal://") && contains(p, "/workloadIdentityPools/"):
		return "workload-federation"
	case hasSuffix(p, "@google.com"):
		return "google-personnel"
	default:
		return "user"
	}
}

// impDepthHint returns the impersonation depth for the synthesised
// session as a coarse indicator. We don't see chain length per call
// without a deeper query, so we approximate: nonzero
// impersonation_calls in the session → depth 1; zero → depth 0.
// Step-3 can refine.
func impDepthHint(sess synthsess.Session) int64 {
	if sess.ImpersonationCalls > 0 {
		return 1
	}
	return 0
}

// cohortFor picks a routing.cohort value. The defaults match the
// vocabulary in notes-gcp/06.
func cohortFor(sess synthsess.Session) string {
	tool := uafingerprint.Classify(sess.SampleUA)
	switch {
	case principalType(sess.Principal) == "service-account",
		principalType(sess.Principal) == "workload-federation":
		return "sa-only"
	case uafingerprint.IsAgentTool(tool):
		return "phase1-cadence"
	case len(sess.Buckets) == 1 && sess.CallCount <= 3:
		return "iap-tunnel-only" // closest analog to "single-shot non-PTY"
	default:
		return "phase1-cadence"
	}
}

// Tiny string helpers — keep std lib import in one place per file.
func hasSuffix(s, suffix string) bool { return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix }
func hasPrefix(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
