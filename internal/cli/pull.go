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
		region       string
	)
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull session.upload events (and recordings) into the local SQLite",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if since == "" || until == "" {
				return errors.New("both --since and --until are required (Athena queries must prune by event_date)")
			}
			s, err := time.Parse(dateLayout, since)
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}
			u, err := time.Parse(dateLayout, until)
			if err != nil {
				return fmt.Errorf("--until: %w", err)
			}

			ctx := cmd.Context()
			dbPath, _ := cmd.Flags().GetString("db")
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer st.Close()

			ac, err := athena.New(ctx, athena.Config{
				Workgroup: workgroup,
				Database:  database,
				Catalog:   catalog,
				Region:    region,
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
				rows, err = ac.QuerySession(ctx, s, u, sessionID)
			} else {
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
				var (
					notable  []store.NotableEvent
					parsedOK bool
				)
				if !noRecordings && r.RecordingURI != "" {
					body, size, gerr := fetcher.Get(ctx, r.RecordingURI)
					if gerr != nil {
						ses.ParseError = "s3 get: " + gerr.Error()
					} else {
						ses.RecordingBytes = size
						feat, ne, perr := recording.Extract(ctx, body)
						body.Close()
						if perr != nil {
							ses.ParseError = "parse: " + perr.Error()
						} else {
							recording.ApplyFeatures(&ses, feat)
							notable = ne
							parsedOK = true
						}
					}
				}
				if err := st.UpsertSession(ses); err != nil {
					return err
				}
				// Only overwrite notable_events when we actually parsed a
				// recording. Events-only sweeps (--no-recordings) and
				// transient S3/parse failures must leave previously
				// extracted notable rows in place.
				if parsedOK {
					if err := st.ReplaceNotable(ses.SessionID, notable); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "start date YYYY-MM-DD (inclusive)")
	cmd.Flags().StringVar(&until, "until", "", "end date YYYY-MM-DD (inclusive)")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "narrow to a single session id within [since,until]")
	cmd.Flags().BoolVar(&noRecordings, "no-recordings", false, "events-only sweep; do not GET S3")
	cmd.Flags().StringVar(&workgroup, "athena-workgroup", "", "Athena workgroup (e.g. teleport_workgroup_<uuid>); set BytesScannedCutoffPerQuery on the workgroup itself for a cost cap")
	cmd.Flags().StringVar(&database, "athena-database", "", "Athena/Glue database (e.g. teleport_events_<uuid>)")
	cmd.Flags().StringVar(&catalog, "athena-catalog", "AwsDataCatalog", "Athena data catalog")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (defaults to standard chain)")
	_ = cmd.MarkFlagRequired("athena-workgroup")
	_ = cmd.MarkFlagRequired("athena-database")
	return cmd
}
