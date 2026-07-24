// Package order hosts CLI subcommands for live order management. All
// business logic lives in service; these handlers parse flags, call into
// the service, and format output.
package order

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"log/slog"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/log"
	accountsvc "github.com/rustyeddy/trader/service/account"
	"github.com/rustyeddy/trader/types"
)

// Shared flag variables across order subcommands.
var (
	instrument string
	side       string
	riskPct    float64
	stopPips   float64
	accountID  string
	token      string
	env        string
	tradeID    string
	closeUnits int64
)

func New(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "order",
		Short: "Live order management (OANDA demo)",
	}
	cmd.AddCommand(newOrderCmd(rc))
	cmd.AddCommand(closeOrderCmd(rc))
	cmd.AddCommand(updateStopCmd(rc))
	cmd.AddCommand(transactionsCmd(rc))
	cmd.AddCommand(transactionsStreamCmd(rc))
	cmd.AddCommand(pricesCmd(rc))
	return cmd
}

// buildDeps resolves the OANDA client, logger, and default account ID from
// current flag values + global config + env fallbacks. cmd is used to
// detect which flags were explicitly set by the user.
func buildDeps(ctx context.Context, cmd *cobra.Command, rc *config.RootConfig) (client *oanda.Client, logger *slog.Logger, resolvedAccountID string, err error) {
	// Token: explicit flag > global config > env var.
	tok := token
	if !cmd.Flags().Changed("token") {
		if rc != nil && rc.OANDA.Token != "" {
			tok = rc.OANDA.Token
		} else {
			tok = os.Getenv("OANDA_TOKEN")
		}
	}

	// Account: explicit flag > global config > env var.
	resolvedAccount := accountID
	if !cmd.Flags().Changed("account-id") {
		if rc != nil && rc.OANDA.AccountID != "" {
			resolvedAccount = rc.OANDA.AccountID
		} else {
			resolvedAccount = os.Getenv("OANDA_ACCOUNT_ID")
		}
	}

	// Env: explicit flag > global config > default "practice".
	resolvedEnv := env
	if !cmd.Flags().Changed("env") && rc != nil && rc.OANDA.Env != "" {
		resolvedEnv = rc.OANDA.Env
	}

	client, err = oanda.NewClient(resolvedEnv, tok)
	if err != nil {
		return nil, nil, "", err
	}
	resolvedID, err := accountsvc.ResolveAccountID(ctx, client, resolvedAccount)
	if err != nil {
		var amb accountsvc.AmbiguousAccountError
		if errors.As(err, &amb) {
			fmt.Println("Multiple accounts found — specify one with --account-id:")
			for _, id := range amb.Accounts {
				fmt.Printf("  %s\n", id)
			}
		}
		return nil, nil, "", err
	}
	return client, log.L, resolvedID, nil
}

// addCommonFlags adds the OANDA auth/account flags every order subcommand needs.
func addCommonFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID (takes precedence over global config and OANDA_ACCOUNT_ID env var)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (takes precedence over global config, OANDA_TOKEN env var, and ~/.config/oanda/pat.txt)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live (takes precedence over global config)")
}

// ── order new ─────────────────────────────────────────────────────────────

func newOrderCmd(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Size and submit a market order with confirmation",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNewOrder(cmd, args, rc)
		},
	}
	cmd.Flags().StringVar(&instrument, "instrument", "", "Instrument in OANDA format, e.g. USD_JPY (required)")
	cmd.Flags().StringVar(&side, "side", "", "Trade direction: long or short (required)")
	cmd.Flags().Float64Var(&riskPct, "risk-pct", 1.0, "Percent of account equity to risk per trade")
	cmd.Flags().Float64Var(&stopPips, "stop-pips", 0, "Stop distance in pips (required)")
	addCommonFlags(cmd)
	_ = cmd.MarkFlagRequired("instrument")
	_ = cmd.MarkFlagRequired("side")
	return cmd
}

func runNewOrder(cmd *cobra.Command, args []string, rc *config.RootConfig) error {
	ctx := context.Background()
	client, logger, resolvedAccountID, err := buildDeps(ctx, cmd, rc)
	if err != nil {
		return err
	}
	acc, err := accountsvc.Resolve(ctx, resolvedAccountID, client, logger)
	if err != nil {
		return err
	}

	// First pass: get the proposal without submitting.
	preview, err := acc.PlaceMarketOrder(ctx, account.PlaceMarketOrderRequest{
		Instrument: instrument,
		Side:       side,
		RiskPct:    types.RateFromFloat(riskPct / 100.0),
		StopPips:   stopPips,
		Confirm:    false,
	})
	if err != nil {
		return err
	}

	printProposal(env, preview.Proposal, riskPct)

	fmt.Print("Submit order? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("Order cancelled.")
		return nil
	}

	// Second pass: submit for real.
	final, err := acc.PlaceMarketOrder(ctx, account.PlaceMarketOrderRequest{
		Instrument: instrument,
		Side:       side,
		RiskPct:    types.RateFromFloat(riskPct / 100.0),
		StopPips:   stopPips,
		Confirm:    true,
	})
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("✓ Order filled")
	fmt.Printf("  Order ID : %s\n", final.Filled.OrderID)
	fmt.Printf("  Trade ID : %s\n", final.Filled.TradeID)
	fmt.Printf("  Units    : %d\n", final.Filled.Units)
	fmt.Printf("  Price    : %.5f\n", final.Filled.Price)
	return nil
}

func printProposal(env string, p account.OrderProposal, riskPct float64) {
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────┐")
	fmt.Printf("│  PROPOSED ORDER — %s %-28s│\n", strings.ToUpper(env), "")
	fmt.Println("├─────────────────────────────────────────────────┤")
	fmt.Printf("│  Instrument   : %-32s│\n", p.Instrument)
	fmt.Printf("│  Side         : %-32s│\n", strings.ToUpper(p.Side))
	fmt.Printf("│  Entry price  : %-32.5f│\n", p.EntryPrice)
	fmt.Printf("│  Stop price   : %-32.5f│\n", p.StopPrice)
	fmt.Printf("│  Units        : %-32d│\n", p.Units)
	fmt.Println("├─────────────────────────────────────────────────┤")
	fmt.Printf("│  Account NAV  : $%-31.2f│\n", p.AccountNAV)
	fmt.Printf("│  Risk %%       : %-31.2f%%│\n", riskPct)
	fmt.Printf("│  Risk amount  : $%-31.2f│\n", p.RiskAmount)
	fmt.Println("└─────────────────────────────────────────────────┘")
	fmt.Println()
}

// ── order close ───────────────────────────────────────────────────────────

func closeOrderCmd(rc *config.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close an open trade (full or partial)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloseOrder(cmd, args, rc)
		},
	}
	cmd.Flags().StringVar(&tradeID, "trade-id", "", "Trade ID to close (required)")
	cmd.Flags().Int64Var(&closeUnits, "units", 0, "Units to close (default 0 = full close)")
	addCommonFlags(cmd)
	_ = cmd.MarkFlagRequired("trade-id")
	return cmd
}

func runCloseOrder(cmd *cobra.Command, args []string, rc *config.RootConfig) error {
	ctx := context.Background()
	client, logger, resolvedAccountID, err := buildDeps(ctx, cmd, rc)
	if err != nil {
		return err
	}
	acc, err := accountsvc.Resolve(ctx, resolvedAccountID, client, logger)
	if err != nil {
		return err
	}

	closeDesc := "full"
	if closeUnits > 0 {
		closeDesc = fmt.Sprintf("%d units", closeUnits)
	}
	fmt.Printf("Close trade %s (%s)? [y/N] ", tradeID, closeDesc)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("Cancelled.")
		return nil
	}

	result, err := acc.CloseTrade(ctx, tradeID, closeUnits)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("✓ Trade closed")
	fmt.Printf("  Order ID : %s\n", result.OrderID)
	fmt.Printf("  Trade ID : %s\n", result.TradeID)
	fmt.Printf("  Units    : %d\n", result.Units)
	fmt.Printf("  Price    : %.5f\n", result.Price)
	return nil
}

// ── order transactions ────────────────────────────────────────────────────

func transactionsCmd(rc *config.RootConfig) *cobra.Command {
	var (
		sinceID int64
		limit   int
	)
	cmd := &cobra.Command{
		Use:   "transactions",
		Short: "List OANDA account transactions since a given ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			client, logger, resolvedAccountID, err := buildDeps(ctx, cmd, rc)
			if err != nil {
				return err
			}
			acc, err := accountsvc.ResolveFirst(ctx, resolvedAccountID, client, logger)
			if err != nil {
				return err
			}

			txns, lastID, err := acc.GetTransactions(ctx, sinceID)
			if err != nil {
				return err
			}

			if len(txns) == 0 {
				fmt.Printf("No transactions since ID %d. lastTransactionID=%d\n", sinceID, lastID)
				return nil
			}

			start := 0
			if limit > 0 && len(txns) > limit {
				start = len(txns) - limit
			}

			bar := strings.Repeat("─", 88)
			fmt.Println(bar)
			fmt.Printf("  %-5s %-22s %-10s %-16s %10s %12s %12s\n",
				"ID", "Type", "Instr", "Time", "Units", "Price", "P/L")
			fmt.Println(bar)
			for _, t := range txns[start:] {
				timeStr := t.Time.Format("2006-01-02 15:04")
				priceStr := "—"
				if t.Price != 0 {
					priceStr = fmt.Sprintf("%.5f", t.Price)
				}
				plStr := "—"
				if t.PL != 0 {
					plStr = fmt.Sprintf("%+.2f", t.PL)
				}
				fmt.Printf("  %-5s %-22s %-10s %-16s %10d %12s %12s\n",
					t.ID, t.Type, t.Instrument, timeStr, t.Units, priceStr, plStr)
			}
			fmt.Println(bar)
			fmt.Printf("Shown: %d of %d total since ID %d. lastTransactionID=%d\n",
				len(txns)-start, len(txns), sinceID, lastID)
			return nil
		},
	}
	cmd.Flags().Int64Var(&sinceID, "since", 0, "Return transactions with ID > this value (0 = from the start)")
	cmd.Flags().IntVar(&limit, "limit", 25, "Max transactions to display (0 = all). Most-recent are shown.")
	addCommonFlags(cmd)
	return cmd
}

// ── order transactions-stream ─────────────────────────────────────────────

func transactionsStreamCmd(rc *config.RootConfig) *cobra.Command {
	var showHeartbeats bool
	cmd := &cobra.Command{
		Use:   "transactions-stream",
		Short: "Subscribe to OANDA transaction stream (push). Ctrl-C to exit.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			client, logger, resolvedAccountID, err := buildDeps(ctx, cmd, rc)
			if err != nil {
				return err
			}
			acc, err := accountsvc.ResolveFirst(ctx, resolvedAccountID, client, logger)
			if err != nil {
				return err
			}

			opts := oanda.StreamOptions{}
			if showHeartbeats {
				opts.OnHeartbeat = func(hb oanda.Heartbeat) {
					fmt.Printf("  ♥  %s   lastTxID=%d\n", hb.Time.Format("15:04:05.000"), hb.LastTxID)
				}
			}

			fmt.Printf("Subscribing to %s transaction stream (Ctrl-C to exit)...\n", resolvedAccountID)
			ch, err := acc.StreamTransactions(ctx, opts)
			if err != nil {
				return fmt.Errorf("subscribe: %w", err)
			}

			for ev := range ch {
				if ev.Err != nil {
					fmt.Fprintf(os.Stderr, "stream error: %v\n", ev.Err)
					continue
				}
				t := ev.Tx
				priceStr := "—"
				if t.Price != 0 {
					priceStr = fmt.Sprintf("%.5f", t.Price)
				}
				plStr := ""
				if t.PL != 0 {
					plStr = fmt.Sprintf("  P/L=%+.2f", t.PL)
				}
				fmt.Printf("  ▸ %s  %-22s %-10s units=%-7d  price=%s%s\n",
					t.ID, t.Type, t.Instrument, t.Units, priceStr, plStr)
			}
			fmt.Println("Stream closed.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&showHeartbeats, "heartbeats", false, "Also print heartbeat messages")
	addCommonFlags(cmd)
	return cmd
}

// ── order update-stop ─────────────────────────────────────────────────────

func updateStopCmd(rc *config.RootConfig) *cobra.Command {
	var (
		stopPrice float64
		takePrice float64
	)
	cmd := &cobra.Command{
		Use:   "update-stop",
		Short: "Update stop-loss and/or take-profit on an open trade",
		Long: `Update the stop-loss and/or take-profit price on an open trade.

Use 0 to leave a value unchanged, or a negative value to cancel it.

Examples:
  trader order update-stop --trade-id 12345 --stop 1.08200
  trader order update-stop --trade-id 12345 --stop 1.08200 --take 1.09500
  trader order update-stop --trade-id 12345 --take -1  # cancel take-profit`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fail fast if neither flag is set
			if stopPrice == 0 && takePrice == 0 {
				return fmt.Errorf("at least one of --stop or --take must be non-zero")
			}
			ctx := context.Background()
			client, logger, resolvedAccountID, err := buildDeps(ctx, cmd, rc)
			if err != nil {
				return err
			}
			acc, err := accountsvc.Resolve(ctx, resolvedAccountID, client, logger)
			if err != nil {
				return err
			}
			if err := acc.UpdateTradeStop(ctx, tradeID, stopPrice, takePrice); err != nil {
				return err
			}
			fmt.Printf("Updated trade %s", tradeID)
			if stopPrice != 0 {
				if stopPrice < 0 {
					fmt.Printf("  stop=cancelled")
				} else {
					fmt.Printf("  stop=%.5f", stopPrice)
				}
			}
			if takePrice != 0 {
				if takePrice < 0 {
					fmt.Printf("  take=cancelled")
				} else {
					fmt.Printf("  take=%.5f", takePrice)
				}
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().StringVar(&tradeID, "trade-id", "", "Trade ID to update (required)")
	cmd.Flags().Float64Var(&stopPrice, "stop", 0, "New stop-loss price (0=unchanged, <0=cancel)")
	cmd.Flags().Float64Var(&takePrice, "take", 0, "New take-profit price (0=unchanged, <0=cancel)")
	_ = cmd.MarkFlagRequired("trade-id")
	addCommonFlags(cmd)
	return cmd
}
