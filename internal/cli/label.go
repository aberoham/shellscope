package cli

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"teleport-ai/internal/labels"
	"teleport-ai/internal/store"
)

func newLabelCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "label", Short: "Manage Kubernetes-style session labels"}
	cmd.AddCommand(newLabelSetCmd())
	cmd.AddCommand(newLabelLsCmd())
	return cmd
}

func newLabelSetCmd() *cobra.Command {
	var sid, key, value, setBy string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Stamp a label on a session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sid == "" || key == "" {
				return errors.New("--session and --key are required")
			}
			dbPath, _ := cmd.Flags().GetString("db")
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer st.Close()
			if setBy == "" {
				setBy = "manual:cli"
			}
			return st.SetLabel(sid, key, value, setBy, time.Now().UTC().Format(time.RFC3339))
		},
	}
	cmd.Flags().StringVar(&sid, "session", "", "session id")
	cmd.Flags().StringVar(&key, "key", "", "label key (e.g. operator.type)")
	cmd.Flags().StringVar(&value, "value", "", "label value")
	cmd.Flags().StringVar(&setBy, "set-by", "", "who/what stamped the label (default 'manual:cli')")
	return cmd
}

func newLabelLsCmd() *cobra.Command {
	var selectorStr string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List sessions matching a Kubernetes-style label selector",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sel, err := labels.ParseSelector(selectorStr)
			if err != nil {
				return err
			}
			dbPath, _ := cmd.Flags().GetString("db")
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer st.Close()
			rows, err := st.ListBySelector(sel)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SESSION_ID\tUSER\tKIND\tPTY\tCHUNKS\tUPLOADED")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%d\t%s\n",
					r.SessionID, r.User, r.Kind, r.PTYPresent, r.PrintChunks, r.UploadedAt)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&selectorStr, "selector", "", "k=v[,k=v...] selector (empty = all)")
	return cmd
}
