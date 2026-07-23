// Package account provides CLI commands for inspecting OANDA accounts.
package account

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/config"
	accountsvc "github.com/rustyeddy/trader/service/account"
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
	cmd.AddCommand(ordersCmd(rc))
	return cmd
}

// resolveTarget binds this command's flags to accountsvc.ResolveTarget: the
// core broker/account-ID resolution logic lives in the account package
// (see account.ResolveTarget), this just supplies the cobra-specific flag
// state and global-config default.
func resolveTarget(cmd *cobra.Command, rc *config.RootConfig) (resolvedBroker, resolvedAccountID string, err error) {
	configAccountID := ""
	if rc != nil {
		configAccountID = rc.OANDAAccountID
	}
	return accountsvc.ResolveTarget(broker, cmd.Flags().Changed("broker"), accountID, cmd.Flags().Changed("account-id"), configAccountID)
}

func listCmd(rc *config.RootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all account IDs for the given broker",
		RunE: func(cmd *cobra.Command, args []string) error {
			tok := resolveToken(cmd, rc)
			targetBroker, defaultAccountID, err := resolveTarget(cmd, rc)
			if err != nil {
				return err
			}
			b, err := accountsvc.NewBroker(targetBroker, resolveEnv(cmd, rc), tok)
			if err != nil {
				return err
			}

			accounts, err := accountsvc.ListAccounts(context.Background(), b)
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
			targetBroker, _, err := resolveTarget(cmd, rc)
			if err != nil {
				return err
			}
			b, err := accountsvc.NewBroker(targetBroker, resolveEnv(cmd, rc), tok)
			if err != nil {
				return err
			}

			var ids []string
			if cmd.Flags().Changed("account-id") {
				ids = []string{accountID}
			}
			results, err := accountsvc.AccountSummaries(ctx, b, ids)
			if err != nil {
				return fmt.Errorf("list accounts: %w", err)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%-26s %-20s %3s  %10s  %10s  %10s  %10s  %10s\n",
				"Account ID", "Name", "CCY", "Balance", "NAV", "Unreal P/L", "Margin Used", "Margin Free")
			fmt.Fprintf(w, "%s\n", strings.Repeat("─", 106))

			for _, result := range results {
				if result.Err != nil {
					fmt.Fprintf(w, "%-26s  error: %v\n", result.ID, result.Err)
					continue
				}
				s := result.Summary
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
				sel, err := accountsvc.DefaultSelection()
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
			if !accountsvc.IsKnownBroker(broker) {
				return fmt.Errorf("unknown broker %q (supported: %s)", broker, strings.Join(accountsvc.KnownBrokers, ", "))
			}

			if err := accountsvc.SetDefault(broker, accountID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Default set to %s/%s\n", broker, accountID)
			return nil
		},
	}
}

// ordersCmd lists open trades for the resolved account. It only parses
// flags and formats output; account/broker selection and the broker call
// itself live in service/account (accountsvc.OpenTrades), same as
// listCmd/summaryCmd above.
func ordersCmd(rc *config.RootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "orders",
		Short: "List open trades from OANDA",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			tok := resolveToken(cmd, rc)
			targetBroker, resolvedAccountID, err := resolveTarget(cmd, rc)
			if err != nil {
				return err
			}
			b, err := accountsvc.NewBroker(targetBroker, resolveEnv(cmd, rc), tok)
			if err != nil {
				return err
			}

			trades, err := accountsvc.OpenTrades(ctx, b, resolvedAccountID)
			if err != nil {
				return fmt.Errorf("list open trades: %w", err)
			}

			w := cmd.OutOrStdout()
			if len(trades) == 0 {
				fmt.Fprintln(w, "No open trades.")
				return nil
			}
			bar := strings.Repeat("─", 52)
			fmt.Fprintln(w, bar)
			fmt.Fprintf(w, "  %-10s %-10s %8s %12s %10s %10s\n", "Trade ID", "Instrument", "Units", "Entry", "Stop", "Unreal P/L")
			fmt.Fprintln(w, bar)
			for _, t := range trades {
				stopStr := "—"
				if t.StopLoss > 0 {
					stopStr = fmt.Sprintf("%.5f", t.StopLoss)
				}
				fmt.Fprintf(w, "  %-10s %-10s %8d %12.5f %10s %+10.2f\n",
					t.ID, t.Instrument, t.Units, t.EntryPrice, stopStr, t.UnrealizedPL)
			}
			fmt.Fprintln(w, bar)
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
