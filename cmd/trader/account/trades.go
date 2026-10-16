package account

import (
	"fmt"

	"github.com/spf13/cobra"

	rpt "github.com/rustyeddy/trader/internal/service/accountreport"
)

// TradesOutput is what a trades listing prints. Open selects the open
// positions view instead of completed trades.
type TradesOutput struct {
	Response rpt.TradesResponse
	Open     bool
}

func newTradesCmd(d deps, flags *groupFlags) *cobra.Command {
	var from, to, benchmark string
	var open bool
	cmd := &cobra.Command{
		Use:   "trades",
		Short: "List completed trades reconstructed from the ledger.",
		Long: "List completed trades. Trades are reconstructed from the whole ledger first and the range is " +
			"applied afterwards, so --from/--to select trades by when they closed: [from, to). " +
			"A trade that opened before the range is still a whole trade.\n\n" +
			"--open lists positions still open instead. Exits with no known entry and activity that belongs " +
			"to no trade are reported, never dropped.\n\n" +
			"Execution quality appears only with --benchmark broker-vwap or broker-quote. Without it no " +
			"slippage column exists; with it, a figure that cannot be measured prints as unknown.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, t, err := parseRange(from, to)
			if err != nil {
				return err
			}
			b, err := rpt.ParseBenchmark(benchmark)
			if err != nil {
				return fmt.Errorf("--benchmark: %w", err)
			}
			s, err := d.open(cmd, flags)
			if err != nil {
				return err
			}
			cur, err := s.cfg.Currency()
			if err != nil {
				return err
			}
			resp, err := rpt.Trades(cmd.Context(), rpt.Params{Store: s.store, Stream: s.stream, From: f, To: t, Benchmark: b, AccountCurrency: cur})
			if err != nil {
				return err
			}
			return s.formatter.FormatTrades(cmd.OutOrStdout(), TradesOutput{Response: resp, Open: open})
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "list trades closed from here (YYYY-MM-DD or RFC3339), inclusive")
	cmd.Flags().StringVar(&to, "to", "", "list trades closed before here (YYYY-MM-DD or RFC3339), exclusive")
	cmd.Flags().BoolVar(&open, "open", false, "list open positions instead of completed trades")
	cmd.Flags().StringVar(&benchmark, "benchmark", "", "measure execution quality against broker-vwap or broker-quote (default: not measured)")

	cmd.AddCommand(newShowCmd(d, flags))
	return cmd
}

func newShowCmd(d deps, flags *groupFlags) *cobra.Command {
	var benchmark string
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one trade with its legs, source activities and provenance.",
		Long: "Show one trade, completed or open, by its full ID or a unique prefix of it. A prefix that " +
			"matches several trades is an error that lists them; one is never chosen for you.\n\n" +
			"This is the audit view: every leg with its price, units and broker trade ID, the source " +
			"activities in full, which figures were reported by the broker and which derived, and, with " +
			"--benchmark, each fill's execution quality.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := rpt.ParseBenchmark(benchmark)
			if err != nil {
				return fmt.Errorf("--benchmark: %w", err)
			}
			s, err := d.open(cmd, flags)
			if err != nil {
				return err
			}
			cur, err := s.cfg.Currency()
			if err != nil {
				return err
			}
			detail, err := rpt.Trade(cmd.Context(), rpt.Params{Store: s.store, Stream: s.stream, Benchmark: b, AccountCurrency: cur}, args[0])
			if err != nil {
				return err
			}
			return s.formatter.FormatTrade(cmd.OutOrStdout(), detail)
		},
	}
	cmd.Flags().StringVar(&benchmark, "benchmark", "", "measure each fill against broker-vwap or broker-quote (default: not measured)")
	return cmd
}
