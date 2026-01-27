package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const version = "1.0.0"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Long:  `Display the current version of the trader CLI.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("trader version %s\n", version)
		fmt.Println("A professional-grade FX trading simulator and research platform")
		fmt.Println("https://github.com/rustyeddy/trader")
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
