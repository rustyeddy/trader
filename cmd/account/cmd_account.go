// Package account provides CLI commands for inspecting OANDA accounts.
package account

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/brokers"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/config/active"
)

var (
	token     string
	env       string
	broker    string
	accountID string
)

// New returns the top-level "account" cobra command.
func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Inspect OANDA accounts",
	}
	cmd.PersistentFlags().StringVar(&token, "token", "", "OANDA API token (overrides config/env)")
	cmd.PersistentFlags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.PersistentFlags().StringVar(&broker, "broker", "oanda", "Broker to target")
	cmd.PersistentFlags().StringVar(&accountID, "account-id", "", "Account ID to target (default: resolved from env/config/active selection)")
	cmd.AddCommand(listCmd(rc))
	cmd.AddCommand(summaryCmd(rc))
	cmd.AddCommand(defaultCmd(rc))
	return cmd
}

// resolveTarget resolves the broker and account ID a command should
// operate on, in priority order: explicit --broker/--account-id flags,
// then OANDA_ACCOUNT_ID env var / global config (account only — broker
// has no env/config-file precedent since only one exists today), then
// the CLI's locally persisted "active" selection (see the active
// package), then unset. accountID may be returned empty — a valid state
// meaning nothing resolved a target.
func resolveTarget(cmd *cobra.Command, rc *config.RootConfig) (resolvedBroker, resolvedAccountID string, err error) {
	sel, _ := active.Load() // best-effort; missing/unreadable file just means no fallback

	resolvedBroker = "oanda"
	if sel.Broker != "" {
		resolvedBroker = sel.Broker
	}
	if cmd.Flags().Changed("broker") {
		resolvedBroker = broker
	}
	if !brokers.IsKnownBroker(resolvedBroker) {
		return "", "", fmt.Errorf("unknown broker %q (supported: %s)", resolvedBroker, strings.Join(brokers.KnownBrokers, ", "))
	}

	switch {
	case cmd.Flags().Changed("account-id"):
		resolvedAccountID = accountID
	case os.Getenv("OANDA_ACCOUNT_ID") != "":
		resolvedAccountID = os.Getenv("OANDA_ACCOUNT_ID")
	case rc != nil && rc.OANDAAccountID != "":
		resolvedAccountID = rc.OANDAAccountID
	case sel.AccountID != "":
		resolvedAccountID = sel.AccountID
	}
	return resolvedBroker, resolvedAccountID, nil
}

func listCmd(rc *config.RootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all OANDA account IDs for the configured token",
		RunE: func(cmd *cobra.Command, args []string) error {
			tok := resolveToken(cmd, rc)
			client, err := oanda.NewClient(resolveEnv(cmd, rc), tok)
			if err != nil {
				return err
			}
			_, defaultAccountID, err := resolveTarget(cmd, rc)
			if err != nil {
				return err
			}

			accounts, err := client.GetAccounts(context.Background())
			if err != nil {
				return fmt.Errorf("list accounts: %w", err)
			}

			for _, a := range accounts {
				marker := "  "
				if defaultAccountID != "" && a.ID == defaultAccountID {
					marker = "* "
				}
				tags := ""
				if len(a.Tags) > 0 {
					tags = "  [" + strings.Join(a.Tags, ", ") + "]"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s%s%s\n", marker, a.ID, tags)
			}
			return nil
		},
	}
}

func summaryCmd(rc *config.RootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "summary",
		Short: "Print balance, NAV, margin, and P/L for one or all accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			tok := resolveToken(cmd, rc)
			client, err := oanda.NewClient(resolveEnv(cmd, rc), tok)
			if err != nil {
				return err
			}

			var refs []oanda.AccountRef
			if cmd.Flags().Changed("account-id") {
				refs = []oanda.AccountRef{{ID: accountID}}
			} else {
				refs, err = client.GetAccounts(ctx)
				if err != nil {
					return fmt.Errorf("list accounts: %w", err)
				}
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%-26s %-20s %3s  %10s  %10s  %10s  %10s  %10s\n",
				"Account ID", "Name", "CCY", "Balance", "NAV", "Unreal P/L", "Margin Used", "Margin Free")
			fmt.Fprintf(w, "%s\n", strings.Repeat("─", 106))

			for _, ref := range refs {
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

// defaultCmd shows or sets the CLI's locally-persisted default
// broker/account selection (see the active package). With no flags, it
// prints the current selection. With --broker and --account-id given
// together, it sets the selection.
func defaultCmd(rc *config.RootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "default",
		Short: "Show or set the CLI's default broker/account",
		Long: `With no flags, prints the currently active broker/account selection.
With --broker and --account-id given together, sets the active selection.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			brokerChanged := cmd.Flags().Changed("broker")
			accountChanged := cmd.Flags().Changed("account-id")

			if !brokerChanged && !accountChanged {
				sel, err := active.Load()
				if err != nil {
					return err
				}
				if sel.IsZero() {
					fmt.Fprintln(cmd.OutOrStdout(), "No default set.")
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Default: %s/%s\n", sel.Broker, sel.AccountID)
				return nil
			}

			if brokerChanged != accountChanged {
				return fmt.Errorf("--broker and --account-id must be set together")
			}
			if !brokers.IsKnownBroker(broker) {
				return fmt.Errorf("unknown broker %q (supported: %s)", broker, strings.Join(brokers.KnownBrokers, ", "))
			}

			sel := active.Selection{Broker: broker, AccountID: accountID}
			if err := active.Save(sel); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Default set to %s/%s\n", sel.Broker, sel.AccountID)
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
