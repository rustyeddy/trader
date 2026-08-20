package main

import "github.com/spf13/cobra"

// newDataCmd returns the "data" command group: the shell M2.5's
// historical market-data commands attach to (bars/coverage/plan, #109;
// sync/build/update, this issue's own scope).
//
// Its PersistentPreRunE builds the *marketdata.Manager/Service every
// data subcommand shares, from datasetFlags/datasetConfig
// (dataservice.go). This relies on cobra.EnableTraverseRunHooks (set
// once in newRootCmd): without it, Cobra would run only the *closest*
// PersistentPreRunE up the command tree for a given invocation, and
// data's own hook would entirely shadow root's logging-setup hook for
// every data subcommand rather than running after it.
//
// RunE prints help rather than being left nil, for the same reason
// root.go's own RunE does: a non-runnable command skips
// PersistentPreRunE entirely, which would silently skip both logging
// and data-service configuration validation on a bare "trader data"
// invocation.
func newDataCmd() *cobra.Command {
	var flags datasetFlags

	cmd := &cobra.Command{
		Use:   "data",
		Short: "Historical market-data commands.",
		Long: "Historical market-data commands: query canonical bars, " +
			"inspect coverage, and plan, sync, build, or update a dataset.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			dc, err := buildDataContext(cmd, flags)
			if err != nil {
				return err
			}
			cmd.SetContext(withDataContext(cmd.Context(), dc))
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().StringVar(&flags.storeRoot, "store-root", "",
		"canonical data store root path, required")
	cmd.PersistentFlags().StringVar(&flags.rawRoot, "raw-root", "",
		"raw provider archive root path (required for coverage/plan/sync/build/update)")
	cmd.PersistentFlags().StringVar(&flags.provider, "provider", "",
		"canonical dataset provider name (default oanda)")
	cmd.PersistentFlags().StringVar(&flags.oandaBaseURL, "oanda-base-url", "",
		"OANDA API base URL (required for sync/update; the token itself is "+
			"never a flag -- set TRADER_OANDA_TOKEN instead)")

	cmd.AddCommand(newBarsCmd())
	cmd.AddCommand(newCoverageCmd())
	cmd.AddCommand(newPlanCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newBuildCmd())
	cmd.AddCommand(newUpdateCmd())

	return cmd
}
