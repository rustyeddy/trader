// Package account provides CLI commands for inspecting OANDA accounts.
package account

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/config"
)

var (
	token string
	env   string
)

// New returns the top-level "account" cobra command.
func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Inspect OANDA accounts",
	}
	cmd.PersistentFlags().StringVar(&token, "token", "", "OANDA API token (overrides config/env)")
	cmd.PersistentFlags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.AddCommand(listCmd(rc))
	cmd.AddCommand(summaryCmd(rc))
	return cmd
}

func listCmd(rc *config.RootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all OANDA account IDs for the configured token",
		RunE: func(cmd *cobra.Command, args []string) error {
			tok := resolveToken(cmd, rc)
			if tok == "" {
				return fmt.Errorf("no OANDA token: set --token, OANDA_TOKEN env var, or trader.yml")
			}
			resolvedEnv := resolveEnv(cmd, rc)

			baseURL, err := oanda.BaseURL(resolvedEnv)
			if err != nil {
				return err
			}
			client := &oanda.Client{BaseURL: baseURL, Token: tok}

			accounts, err := client.GetAccounts(context.Background())
			if err != nil {
				return fmt.Errorf("list accounts: %w", err)
			}

			for _, a := range accounts {
				tags := ""
				if len(a.Tags) > 0 {
					tags = "  [" + strings.Join(a.Tags, ", ") + "]"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s%s\n", a.ID, tags)
			}
			return nil
		},
	}
}

func summaryCmd(rc *config.RootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "summary",
		Short: "Print balance, NAV, margin, and P/L for all accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			tok := resolveToken(cmd, rc)
			if tok == "" {
				return fmt.Errorf("no OANDA token: set --token, OANDA_TOKEN env var, or trader.yml")
			}
			baseURL, err := oanda.BaseURL(resolveEnv(cmd, rc))
			if err != nil {
				return err
			}
			client := &oanda.Client{BaseURL: baseURL, Token: tok}

			accounts, err := client.GetAccounts(ctx)
			if err != nil {
				return fmt.Errorf("list accounts: %w", err)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%-26s %-20s %3s  %10s  %10s  %10s  %10s  %10s\n",
				"Account ID", "Name", "CCY", "Balance", "NAV", "Unreal P/L", "Margin Used", "Margin Free")
			fmt.Fprintf(w, "%s\n", strings.Repeat("─", 106))

			for _, ref := range accounts {
				s, err := client.GetAccountSummary(ctx, ref.ID)
				if err != nil {
					fmt.Fprintf(w, "%-26s  error: %v\n", ref.ID, err)
					continue
				}
				name := s.Alias
				if name == "" {
					name = "-"
				}
				fmt.Fprintf(w, "%-26s %-20s %3s  %10.2f  %10.2f  %+10.2f  %10.2f  %10.2f\n",
					s.ID, name, s.Currency,
					s.Balance, s.NAV, s.UnrealizedPL, s.MarginUsed, s.MarginAvail)
			}
			return nil
		},
	}
}

func resolveToken(cmd *cobra.Command, rc *config.RootConfig) string {
	if cmd.Flags().Changed("token") {
		return token
	}
	if rc != nil && rc.OANDAToken != "" {
		return rc.OANDAToken
	}
	return os.Getenv("OANDA_TOKEN")
}

func resolveEnv(cmd *cobra.Command, rc *config.RootConfig) string {
	if cmd.Flags().Changed("env") {
		return env
	}
	if rc != nil && rc.OANDAEnv != "" {
		return rc.OANDAEnv
	}
	return env // default "practice"
}
