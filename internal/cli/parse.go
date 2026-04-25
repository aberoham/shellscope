package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"teleport-ai/internal/recording"
	"teleport-ai/internal/store"
)

func newParseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parse <path/to/sid.tar>",
		Short: "Parse a local recording file and upsert into the SQLite",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()

			feat, notable, err := recording.Extract(cmd.Context(), f)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}

			dbPath, _ := cmd.Flags().GetString("db")
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer st.Close()

			sid := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			ses := store.Session{
				SessionID:     sid,
				ParsedAt:      time.Now().UTC().Format(time.RFC3339),
				ParserVersion: recording.ParserVersion,
			}
			recording.ApplyFeatures(&ses, feat)
			if err := st.UpsertSession(ses); err != nil {
				return err
			}
			if err := st.ReplaceNotable(ses.SessionID, notable); err != nil {
				return err
			}
			cmd.Printf("parsed %s: kind=%s pty=%v print_chunks=%d notable=%d\n",
				sid, ses.Kind, ses.PTYPresent, ses.PrintChunks, len(notable))
			return nil
		},
	}
	return cmd
}
