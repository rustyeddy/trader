// Package account provides CLI commands for inspecting OANDA accounts.
package account

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/config"
	accountsvc "github.com/rustyeddy/trader/service/account"
)

var (
	broker    string
	accountID string
)

// New returns the top-level "account" cobra command.
func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Inspect OANDA accounts",
	}
	cmd.PersistentFlags().StringVar(&broker, "broker", "oanda", "Broker to target")
	cmd.PersistentFlags().StringVar(&accountID, "account-id", "", "Account ID to target")
	cmd.AddCommand(listCmd(rc))
	cmd.AddCommand(summaryCmd(rc))
	cmd.AddCommand(defaultCmd(rc))
	cmd.AddCommand(ordersCmd(rc))
	return cmd
}

// cfgFor builds the accountsvc.AccountCfg shared across subcommands from
// cmd's bound broker/account-id flags.
func accountCfg(cmd *cobra.Command) accountsvc.AccountCfg {
	return accountsvc.AccountCfg{
		Broker:           broker,
		BrokerChanged:    cmd.Flags().Changed("broker"),
		AccountID:        accountID,
		AccountIDChanged: cmd.Flags().Changed("account-id"),
	}
}

func listCmd(rc *config.RootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all account IDs for the given broker",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := accountsvc.List(context.Background(), accountCfg(cmd))
			if err != nil {
				return err
			}

			for _, a := range result.Accounts {
				marker := "  "
				if result.DefaultAccountID != "" && a.ID == result.DefaultAccountID {
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
			results, err := accountsvc.Summary(context.Background(), accountCfg(cmd))
			if err != nil {
				return err
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
			result, err := accountsvc.Default(accountCfg(cmd))
			if err != nil {
				return err
			}

			if !result.DidSet {
				if !result.HasSelection {
					fmt.Fprintln(cmd.OutOrStdout(), "No default set.")
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Default: %s/%s\n", result.Selection.Broker, result.Selection.AccountID)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Default set to %s/%s\n", result.Selection.Broker, result.Selection.AccountID)
			return nil
		},
	}
}

// ordersCmd lists open trades for the resolved account. It only parses
// flags and formats output; account/broker resolution and the broker call
// itself live in service/account (accountsvc.Orders), same as
// listCmd/summaryCmd above.
func ordersCmd(rc *config.RootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "orders",
		Short: "List open trades from OANDA",
		RunE: func(cmd *cobra.Command, args []string) error {
			trades, err := accountsvc.Orders(context.Background(), accountCfg(cmd))
			if err != nil {
				return err
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
