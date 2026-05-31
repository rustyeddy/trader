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

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/service"
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

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "order",
		Short: "Live order management (OANDA demo)",
	}
	cmd.AddCommand(newOrderCmd())
	cmd.AddCommand(listOrdersCmd())
	cmd.AddCommand(closeOrderCmd())
	cmd.AddCommand(transactionsCmd())
	cmd.AddCommand(transactionsStreamCmd())
	cmd.AddCommand(pricesCmd())
	return cmd
}

// buildService wires a Service from current flag values + env fallbacks.
// Used by every subcommand in this package.
func buildService(ctx context.Context) (*service.Service, error) {
	svc, err := service.New(service.Config{
		Env:       env,
		Token:     token,
		AccountID: accountID,
	})
	if err != nil {
		return nil, err
	}
	if err := svc.ResolveAccount(ctx); err != nil {
		var amb service.AmbiguousAccountError
		if errors.As(err, &amb) {
			fmt.Println("Multiple accounts found — specify one with --account-id:")
			for _, id := range amb.Accounts {
				fmt.Printf("  %s\n", id)
			}
		}
		return nil, err
	}
	return svc, nil
}

// addCommonFlags adds the OANDA auth/account flags every order subcommand needs.
func addCommonFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID (auto-discovered if omitted)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (falls back to ~/.config/oanda/pat.txt)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
}

// ── order new ─────────────────────────────────────────────────────────────

func newOrderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Size and submit a market order with confirmation",
		RunE:  runNewOrder,
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

func runNewOrder(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	svc, err := buildService(ctx)
	if err != nil {
		return err
	}

	// First pass: get the proposal without submitting.
	preview, err := svc.PlaceMarketOrder(ctx, service.PlaceMarketOrderRequest{
		Instrument: instrument,
		Side:       side,
		RiskPct:    riskPct,
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
	final, err := svc.PlaceMarketOrder(ctx, service.PlaceMarketOrderRequest{
		Instrument: instrument,
		Side:       side,
		RiskPct:    riskPct,
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

func printProposal(env string, p service.OrderProposal, riskPct float64) {
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

// ── order list ────────────────────────────────────────────────────────────

func listOrdersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List open trades from OANDA",
		RunE:  runListOrders,
	}
	addCommonFlags(cmd)
	return cmd
}

func runListOrders(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	svc, err := buildService(ctx)
	if err != nil {
		return err
	}
	trades, err := svc.ListOpenTrades(ctx)
	if err != nil {
		return err
	}
	if len(trades) == 0 {
		fmt.Println("No open trades.")
		return nil
	}
	bar := strings.Repeat("─", 52)
	fmt.Println(bar)
	fmt.Printf("  %-10s %-10s %8s %12s %10s %10s\n", "Trade ID", "Instrument", "Units", "Entry", "Stop", "Unreal P/L")
	fmt.Println(bar)
	for _, t := range trades {
		stopStr := "—"
		if t.StopLoss > 0 {
			stopStr = fmt.Sprintf("%.5f", t.StopLoss)
		}
		fmt.Printf("  %-10s %-10s %8d %12.5f %10s %+10.2f\n",
			t.ID, t.Instrument, t.Units, t.EntryPrice, stopStr, t.UnrealizedPL)
	}
	fmt.Println(bar)
	return nil
}

// ── order close ───────────────────────────────────────────────────────────

func closeOrderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close an open trade (full or partial)",
		RunE:  runCloseOrder,
	}
	cmd.Flags().StringVar(&tradeID, "trade-id", "", "Trade ID to close (required)")
	cmd.Flags().Int64Var(&closeUnits, "units", 0, "Units to close (default 0 = full close)")
	addCommonFlags(cmd)
	_ = cmd.MarkFlagRequired("trade-id")
	return cmd
}

func runCloseOrder(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	svc, err := buildService(ctx)
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

	result, err := svc.CloseTrade(ctx, tradeID, closeUnits)
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

func transactionsCmd() *cobra.Command {
	var (
		sinceID int64
		limit   int
	)
	cmd := &cobra.Command{
		Use:   "transactions",
		Short: "List OANDA account transactions since a given ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			svc, err := buildService(ctx)
			if err != nil {
				return err
			}

			txns, lastID, err := svc.GetTransactions(ctx, sinceID)
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

func transactionsStreamCmd() *cobra.Command {
	var showHeartbeats bool
	cmd := &cobra.Command{
		Use:   "transactions-stream",
		Short: "Subscribe to OANDA transaction stream (push). Ctrl-C to exit.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			svc, err := buildService(ctx)
			if err != nil {
				return err
			}

			opts := oanda.StreamOptions{}
			if showHeartbeats {
				opts.OnHeartbeat = func(hb oanda.Heartbeat) {
					fmt.Printf("  ♥  %s   lastTxID=%d\n", hb.Time.Format("15:04:05.000"), hb.LastTxID)
				}
			}

			fmt.Printf("Subscribing to %s transaction stream (Ctrl-C to exit)...\n", svc.AccountID)
			ch, err := svc.StreamTransactions(ctx, opts)
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
