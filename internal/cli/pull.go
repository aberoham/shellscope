package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"teleport-ai/internal/athena"
	"teleport-ai/internal/recording"
	"teleport-ai/internal/s3fetch"
	"teleport-ai/internal/store"
)

const dateLayout = "2006-01-02"

func newPullCmd() *cobra.Command {
	var (
		since        string
		until        string
		sessionID    string
		noRecordings bool
		workgroup    string
		database     string
		catalog      string
		bytesCap     int64
		region       string
	)
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull session.upload events (and recordings) into the local SQLite",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sessionID == "" && (since == "" || until == "") {
				return errors.New("either --session-id, or both --since and --until, are required")
			}
			ctx := cmd.Context()
			dbPath, _ := cmd.Flags().GetString("db")
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer st.Close()

			ac, err := athena.New(ctx, athena.Config{
				Workgroup:    workgroup,
				Database:     database,
				Catalog:      catalog,
				BytesScanCap: bytesCap,
				Region:       region,
			})
			if err != nil {
				return err
			}
			fetcher, err := s3fetch.New(ctx, region)
			if err != nil {
				return err
			}

			var rows []athena.UploadRow
			if sessionID != "" {
				rows, err = ac.QuerySession(ctx, sessionID)
			} else {
				var s, u time.Time
				if s, err = time.Parse(dateLayout, since); err != nil {
					return fmt.Errorf("--since: %w", err)
				}
				if u, err = time.Parse(dateLayout, until); err != nil {
					return fmt.Errorf("--until: %w", err)
				}
				rows, err = ac.QueryRange(ctx, s, u)
			}
			if err != nil {
				return err
			}
			cmd.Printf("athena: %d session.upload events\n", len(rows))

			parserVer := recording.ParserVersion
			for _, r := range rows {
				ses := store.Session{
					SessionID:     r.SessionID,
					User:          r.User,
					Cluster:       r.Cluster,
					UploadedAt:    r.EventTime.UTC().Format(time.RFC3339),
					RecordingURI:  r.RecordingURI,
					ParsedAt:      time.Now().UTC().Format(time.RFC3339),
					ParserVersion: parserVer,
				}
				if !noRecordings && r.RecordingURI != "" {
					body, size, err := fetcher.Get(ctx, r.RecordingURI)
					if err != nil {
						ses.ParseError = "s3 get: " + err.Error()
					} else {
						ses.RecordingBytes = size
						feat, notable, err := recording.Extract(ctx, body)
						body.Close()
						if err != nil {
							ses.ParseError = "parse: " + err.Error()
						} else {
							recording.ApplyFeatures(&ses, feat)
							if err := st.UpsertSession(ses); err != nil {
								return err
							}
							for _, n := range notable {
								if err := st.InsertNotable(ses.SessionID, n); err != nil {
									return err
								}
							}
							continue
						}
					}
				}
				if err := st.UpsertSession(ses); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "start date YYYY-MM-DD (inclusive)")
	cmd.Flags().StringVar(&until, "until", "", "end date YYYY-MM-DD (inclusive)")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "single session id (skips date range)")
	cmd.Flags().BoolVar(&noRecordings, "no-recordings", false, "events-only sweep; do not GET S3")
	cmd.Flags().StringVar(&workgroup, "athena-workgroup", "", "Athena workgroup (e.g. teleport_workgroup_<uuid>)")
	cmd.Flags().StringVar(&database, "athena-database", "", "Athena/Glue database (e.g. teleport_events_<uuid>)")
	cmd.Flags().StringVar(&catalog, "athena-catalog", "AwsDataCatalog", "Athena data catalog")
	cmd.Flags().Int64Var(&bytesCap, "athena-bytes-cap", 0, "per-query bytes-scanned cap (0 = unset)")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (defaults to standard chain)")
	_ = cmd.MarkFlagRequired("athena-workgroup")
	_ = cmd.MarkFlagRequired("athena-database")
	return cmd
}
