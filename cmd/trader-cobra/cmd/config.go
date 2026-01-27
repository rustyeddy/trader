package cmd

import (
	"fmt"

	"github.com/rustyeddy/trader/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Generate or validate configuration files",
	Long: `Manage configuration files for trading simulations.

Subcommands:
  init     - Generate a default configuration file
  validate - Validate an existing configuration file

Examples:
  trader config init -output my-config.yaml
  trader config validate -config my-config.yaml`,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a default configuration file",
	Long: `Create a new configuration file with default settings.

Example:
  trader config init -output simulation.yaml`,
	RunE: runConfigInit,
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a configuration file",
	Long: `Check if a configuration file is valid and can be loaded.

Example:
  trader config validate -config simulation.yaml`,
	RunE: runConfigValidate,
}

var (
	configInitOutput   string
	configValidatePath string
)

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configValidateCmd)

	configInitCmd.Flags().StringVarP(&configInitOutput, "output", "o", "simulation.yaml", "output config file path")
	configValidateCmd.Flags().StringVarP(&configValidatePath, "file", "f", "", "path to config file (required)")
	configValidateCmd.MarkFlagRequired("file")
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	cfg := config.Default()
	if err := cfg.SaveToFile(configInitOutput); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("✓ Created default configuration: %s\n", configInitOutput)
	fmt.Println("\nEdit the file and run with:")
	fmt.Printf("  trader run -config %s\n", configInitOutput)
	return nil
}

func runConfigValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadFromFile(configValidatePath)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	fmt.Printf("✓ Configuration valid: %s\n", configValidatePath)
	fmt.Printf("  Account: %s ($%.2f %s)\n", cfg.Account.ID, cfg.Account.Balance, cfg.Account.Currency)
	fmt.Printf("  Strategy: %s (Risk: %.1f%%)\n", cfg.Strategy.Instrument, cfg.Strategy.RiskPercent*100)
	fmt.Printf("  Journal: %s\n", cfg.Journal.Type)
	return nil
}
