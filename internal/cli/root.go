package cli

import (
	"github.com/spf13/cobra"
)

const defaultDB = "sessions.sqlite"

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "teleport-analyze",
		Short:         "Ad-hoc Teleport session analyzer",
		Long:          "Pulls session.upload events from Athena, fetches recordings from S3, parses ProtoStreamV1 audit events, and writes per-session features and Kubernetes-style labels into a local SQLite file.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("db", defaultDB, "path to sessions.sqlite")

	root.AddCommand(newPullCmd())
	root.AddCommand(newParseCmd())
	root.AddCommand(newLabelCmd())
	return root
}
