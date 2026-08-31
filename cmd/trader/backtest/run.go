package backtest

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/cmd/trader/internal/clictx"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/report"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
	svcmarketdata "github.com/rustyeddy/trader/service/marketdata"
)

// runFlags holds "trader backtest run"'s own flag values.
type runFlags struct {
	symbol   string
	interval string
	from     string
	to       string

	startingCash string
	currency     string
	riskFraction string
	adverse      string
	warmupBars   int

	dataStoreRoot string
	dataRawRoot   string
	provider      string

	outputDir string
	format    string
}

func newRunCmd() *cobra.Command {
	var flags runFlags

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a backtest and render/persist its result.",
		Long: "Run a backtest over the M5 application service and render its result.\n\n" +
			"This build accepts only one, provisional demo strategy (a single\n" +
			"buy-and-hold entry on the requested instrument's first bar) — see\n" +
			"the package doc comment. Canonical market data must already be\n" +
			"available under --data-store-root/--data-raw-root (published via\n" +
			"'trader data build'/'trader data sync'); this command never syncs\n" +
			"from a live provider itself.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBacktest(cmd, flags)
		},
	}

	cmd.Flags().StringVar(&flags.symbol, "symbol", "", "instrument symbol, e.g. EURUSD (required)")
	cmd.Flags().StringVar(&flags.interval, "interval", "H1", "bar interval: M1, H1, H4, D1, or W1")
	cmd.Flags().StringVar(&flags.from, "from", "", "replay range start (YYYY-MM-DD or RFC3339), required")
	cmd.Flags().StringVar(&flags.to, "to", "", "replay range end (YYYY-MM-DD or RFC3339), required")

	cmd.Flags().StringVar(&flags.startingCash, "starting-cash", "10000", "starting account cash amount")
	cmd.Flags().StringVar(&flags.currency, "currency", "USD", "account currency")
	cmd.Flags().StringVar(&flags.riskFraction, "risk-fraction", "0.01", "fraction of account equity to risk, e.g. 0.01 for 1%")
	cmd.Flags().StringVar(&flags.adverse, "adverse-distance", "", "adverse price distance used for sizing (required)")
	cmd.Flags().IntVar(&flags.warmupBars, "warmup-bars", 0, "warm-up bars required before the demo strategy may trade")

	cmd.Flags().StringVar(&flags.dataStoreRoot, "data-store-root", "", "canonical data store root (default: a fresh temporary directory)")
	cmd.Flags().StringVar(&flags.dataRawRoot, "data-raw-root", "", "raw archive root (required)")
	cmd.Flags().StringVar(&flags.provider, "provider", "oanda", "market data provider name")

	cmd.Flags().StringVar(&flags.outputDir, "output-dir", "./backtest-runs", "directory run snapshots are written to and 'show' reads from")
	cmd.Flags().StringVar(&flags.format, "format", formatTable, "output format: "+formatTable+", "+formatJSON+", or "+formatOrg)

	_ = cmd.MarkFlagRequired("symbol")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	_ = cmd.MarkFlagRequired("adverse-distance")
	_ = cmd.MarkFlagRequired("data-raw-root")

	return cmd
}

// runBacktest is newRunCmd's own RunE, split out so its own control
// flow is easy to read top to bottom: parse flags -> resolve
// instrument/data -> publish canonical data if needed -> compute the
// demo strategy's one, analytically known next-bar-open fill price ->
// build the concrete EnvironmentFactory -> call service/backtest.Run
// -> project into a report.BacktestReport once -> persist that same
// projection -> render it. No backtest orchestration happens here:
// service/backtest.Service.Run is the only thing that drives a replay.
func runBacktest(cmd *cobra.Command, flags runFlags) error {
	ctx := cmd.Context()

	interval, err := parseInterval(flags.interval)
	if err != nil {
		return err
	}
	from, err := parseDate(flags.from)
	if err != nil {
		return err
	}
	to, err := parseDate(flags.to)
	if err != nil {
		return err
	}
	span, err := marketdata.NewTimeRange(from, to)
	if err != nil {
		return fmt.Errorf("invalid --from/--to range: %w", err)
	}

	currency, err := num.ParseCurrency(flags.currency)
	if err != nil {
		return fmt.Errorf("invalid --currency: %w", err)
	}
	startingCash, err := num.ParseMoney(flags.startingCash, currency)
	if err != nil {
		return fmt.Errorf("invalid --starting-cash: %w", err)
	}
	riskFraction, err := num.ParseRate(flags.riskFraction)
	if err != nil {
		return fmt.Errorf("invalid --risk-fraction: %w", err)
	}
	adverseDistance, err := num.ParsePrice(flags.adverse)
	if err != nil {
		return fmt.Errorf("invalid --adverse-distance: %w", err)
	}

	storeRoot := flags.dataStoreRoot
	if storeRoot == "" {
		dir, err := os.MkdirTemp("", "trader-backtest-store-")
		if err != nil {
			return fmt.Errorf("creating temporary data store: %w", err)
		}
		defer func() { _ = os.RemoveAll(dir) }()
		storeRoot = dir
	}

	// oandaResolver is the marketdata-side resolver Manager fetches
	// bars through; simResolver is the broker-side resolver Runner's
	// default InputBuilder resolves order submissions through — two
	// distinct providers for the same economic instrument, matching
	// ADR-016's own separation.
	oandaResolver := instrument.NewMemoryResolver()
	instrumentID, err := svcmarketdata.RegisterFXInstrument(oandaResolver, flags.provider, flags.symbol)
	if err != nil {
		return err
	}
	simResolver := instrument.NewMemoryResolver()
	if _, err := svcmarketdata.RegisterFXInstrument(simResolver, "sim", flags.symbol); err != nil {
		return err
	}
	simListing, err := simResolver.ResolveInstrument(instrumentID, "sim", "")
	if err != nil {
		return err
	}

	manager, err := marketdata.New(marketdata.Config{
		Clock:        clock.Real{},
		StoreRoot:    storeRoot,
		RawRoot:      flags.dataRawRoot,
		Resolver:     oandaResolver,
		ProviderName: flags.provider,
	})
	if err != nil {
		return err
	}

	plan, err := manager.Plan(ctx, marketdata.BarQuery{Instrument: instrumentID, Interval: interval, Range: span})
	if err != nil {
		return err
	}
	if len(plan.Actions) > 0 {
		if _, err := manager.Build(ctx, plan); err != nil {
			return err
		}
	}

	fillPrice, err := nextBarOpenAfterEntry(ctx, manager, instrumentID, interval, span, flags.warmupBars)
	if err != nil {
		return fmt.Errorf("computing the demo strategy's next-bar-open fill price: %w", err)
	}

	factory := environmentFactory{
		listing: simListing,
		prices:  &simPriceSource{symbol: simListing.Symbol(), price: fillPrice},
	}

	svc, err := svcbacktest.New(manager, simResolver, factory, clictx.LoggerFromContext(ctx))
	if err != nil {
		return err
	}

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:        newDemoStrategy(instrumentID, interval, flags.warmupBars),
		Span:            span,
		StartingCapital: startingCash,
		RiskFraction:    riskFraction,
		AdverseDistance: adverseDistance,
	})
	if err != nil {
		return err
	}

	rep := report.NewBacktestReport(report.BacktestInput{
		Manifest:    resp.Manifest,
		Account:     resp.Account,
		Trades:      resp.Trades,
		OpenTrades:  resp.OpenTrades,
		EquityCurve: resp.EquityCurve,
		Metrics:     resp.Metrics,
	})

	if err := saveSnapshot(flags.outputDir, runSnapshot{SchemaVersion: snapshotSchemaVersion, Report: rep}); err != nil {
		return err
	}

	return render(cmd.OutOrStdout(), flags.format, rep)
}

// nextBarOpenAfterEntry returns the Open of the bar immediately
// following demoStrategy's own entry bar — the exact price Scheduler's
// next-bar-open fill-eligibility rule (issue #214) actually fills a
// market order at, never the entry bar's own Close (PR #240 review).
// demoStrategy receives OnBar starting at bar index warmupBars (the
// first warmupBars bars are consumed as warm-up and never delivered to
// OnBar) and enters on that very first delivered bar, so the entry bar
// is index warmupBars and the fill bar is the one immediately after
// it, index warmupBars+1 — this function reads and discards exactly
// that many bars before returning the following bar's Open.
func nextBarOpenAfterEntry(ctx context.Context, manager *marketdata.Manager, instrumentID instrument.ID, interval marketdata.Interval, span marketdata.TimeRange, warmupBars int) (num.Price, error) {
	reader, err := manager.Bars(ctx, marketdata.BarQuery{Instrument: instrumentID, Interval: interval, Range: span})
	if err != nil {
		return num.Price{}, err
	}
	defer func() { _ = reader.Close() }()

	// Discard the warmupBars warm-up bars plus the entry bar itself
	// (warmupBars + 1 bars total), then the next Next() call returns
	// the fill bar.
	for i := 0; i < warmupBars+1; i++ {
		if _, err := reader.Next(ctx); err != nil {
			return num.Price{}, fmt.Errorf("not enough bars in the requested range for the demo strategy to enter (need at least %d before its fill bar): %w", warmupBars+1, err)
		}
	}

	fillBar, err := reader.Next(ctx)
	if err != nil {
		return num.Price{}, fmt.Errorf("not enough bars in the requested range for the demo strategy's entry to fill on the following bar: %w", err)
	}
	return fillBar.Open, nil
}
