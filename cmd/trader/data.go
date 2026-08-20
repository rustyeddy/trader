package main

import "github.com/spf13/cobra"

// newDataCmd returns the "data" command group: the shell M2.5's
// historical market-data commands (bars, coverage, plan, sync, build,
// update; issues #109-#110) attach to. It has no subcommands yet —
// issue #108's own scope is establishing the framework, not
// market-data command behavior.
//
// RunE prints help rather than being left nil: a nil Run/RunE makes
// Cobra treat the command as non-runnable, which skips
// PersistentPreRunE entirely (see root.go's identical RunE for the
// full explanation). An explicit RunE that just shows help keeps
// "trader data" alone visually identical to Cobra's own default
// behavior while still running the root command's logging/context
// validation on every invocation, not only once a real leaf command
// exists under it.
func newDataCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "data",
		Short: "Historical market-data commands.",
		Long: "Historical market-data commands: query canonical bars, " +
			"inspect coverage, and plan, sync, build, or update a dataset.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
}
