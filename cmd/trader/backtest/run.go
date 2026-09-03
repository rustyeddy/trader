package backtest

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/cmd/trader/internal/clictx"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/report"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
	svcmarketdata "github.com/rustyeddy/trader/service/marketdata"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/strategy/emacross"
)

// runFlags holds "trader backtest run"'s own flag values.
type runFlags struct {
	symbols  []string
	interval string
	from     string
	to       string

	startingCash string
	currency     string
	riskFraction string
	adverse      string
	warmupBars   int

	config       string
	strategyName string
	fastPeriod   int
	slowPeriod   int

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
			"Without --config, this command runs a provisional demo strategy\n" +
			"(a single buy-and-hold entry per instrument's own first bar) —\n" +
			"see the package doc comment. Canonical market data must already\n" +
			"be available under --data-store-root/--data-raw-root (published\n" +
			"via 'trader data build'/'trader data sync'); this command never\n" +
			"syncs from a live provider itself.\n\n" +
			"--symbol may be repeated to run a multi-instrument portfolio\n" +
			"backtest (issue #224) with the demo strategy: one Scheduler and\n" +
			"one shared account/pipeline still replay every requested\n" +
			"instrument — this is not a per-symbol engine.\n\n" +
			"--config supplies backtest/strategy parameters from a YAML file\n" +
			"(issue #247) and runs the real EMA crossover strategy\n" +
			"(issue #252) instead of the demo strategy, for a single\n" +
			"instrument; any explicit flag above still overrides its value.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBacktest(cmd, flags)
		},
	}

	cmd.Flags().StringArrayVar(&flags.symbols, "symbol", nil, "instrument symbol, e.g. EURUSD (required, repeatable for a multi-instrument run)")
	cmd.Flags().StringVar(&flags.interval, "interval", "H1", "bar interval: M1, H1, H4, D1, or W1")
	cmd.Flags().StringVar(&flags.from, "from", "", "replay range start (YYYY-MM-DD or RFC3339), required")
	cmd.Flags().StringVar(&flags.to, "to", "", "replay range end (YYYY-MM-DD or RFC3339), required")

	cmd.Flags().StringVar(&flags.startingCash, "starting-cash", "10000", "starting account cash amount")
	cmd.Flags().StringVar(&flags.currency, "currency", "USD", "account currency")
	cmd.Flags().StringVar(&flags.riskFraction, "risk-fraction", "0.01", "fraction of account equity to risk, e.g. 0.01 for 1%")
	cmd.Flags().StringVar(&flags.adverse, "adverse-distance", "", "adverse price distance used for sizing (required, unless supplied by --config)")
	cmd.Flags().IntVar(&flags.warmupBars, "warmup-bars", 0, "warm-up bars required before the demo strategy may trade, per instrument")

	cmd.Flags().StringVar(&flags.config, "config", "", "YAML config file supplying backtest/strategy parameters (issue #247); explicit flags above always override it")
	cmd.Flags().StringVar(&flags.strategyName, "strategy-name", "", "strategy name recorded in the run manifest (descriptive only: --config always runs the EMA crossover strategy, there is no strategy registry to select from)")
	cmd.Flags().IntVar(&flags.fastPeriod, "fast-period", 0, "EMA fast period; only used when --config is also given")
	cmd.Flags().IntVar(&flags.slowPeriod, "slow-period", 0, "EMA slow period; only used when --config is also given")

	cmd.Flags().StringVar(&flags.dataStoreRoot, "data-store-root", "", "canonical data store root (default: a fresh temporary directory)")
	cmd.Flags().StringVar(&flags.dataRawRoot, "data-raw-root", "", "raw archive root (required)")
	cmd.Flags().StringVar(&flags.provider, "provider", "oanda", "market data provider name")

	cmd.Flags().StringVar(&flags.outputDir, "output-dir", "./backtest-runs", "directory run snapshots are written to and 'show' reads from")
	cmd.Flags().StringVar(&flags.format, "format", formatTable, "output format: "+formatTable+", "+formatJSON+", or "+formatOrg)

	// --symbol/--from/--to/--adverse-distance are no longer cobra-required:
	// each is also satisfiable from --config (issue #247), so their
	// presence is instead enforced uniformly by buildRunConfig's
	// config.Load call, which aggregates every missing/invalid field into
	// one error rather than cobra stopping at the first missing flag.
	_ = cmd.MarkFlagRequired("data-raw-root")

	return cmd
}

// instrumentSet is one canonically resolved, de-duplicated, order-
// independent set of requested instruments: the CLI's own vocabulary
// (a --symbol string) resolved to instrument.ID once, then sorted by
// that ID's own canonical string form so that "--symbol GBPUSD
// --symbol EURUSD" and "--symbol EURUSD --symbol GBPUSD" produce the
// identical requirement/price ordering — flag order must never become
// semantically meaningful (issue #224 review, point 3), even though
// backtest.NewManifest's own Universe canonicalization would catch it
// one layer down regardless.
type instrumentSet struct {
	ids          []instrument.ID
	oandaListing map[string]instrument.Listing // keyed by instrument.ID.String()
	simListing   map[string]instrument.Listing
}

// effectiveSymbols reconciles --symbol (repeatable, multi-instrument,
// issue #224) with backtest.symbol from --config (single-instrument,
// issue #247): explicit --symbol flags always win when present (a
// --config combined with more than one --symbol was already rejected
// by buildRunConfig before this point; --symbol without --config is
// never restricted to one value), and configSymbol is used only as a
// fallback when no --symbol flag was given at all.
func effectiveSymbols(flagSymbols []string, configSymbol string) ([]string, error) {
	if len(flagSymbols) > 0 {
		return flagSymbols, nil
	}
	if configSymbol != "" {
		return []string{configSymbol}, nil
	}
	return nil, fmt.Errorf("at least one --symbol, or backtest.symbol in --config, is required")
}

// resolveInstrumentSet parses flags.symbols into a canonical
// instrumentSet: each symbol is registered under both the oanda-side
// resolver (Manager's own bar-fetching resolver) and the sim-side
// resolver (the broker-side resolver Runner's default InputBuilder
// resolves order submissions through — two distinct providers for the
// same economic instrument, matching ADR-016's own separation),
// rejecting an explicit duplicate symbol as a clear CLI validation
// error (issue #224 review, point 3) rather than letting two identical
// DataRequirements reach Replay/Runner and fail deeper in the stack.
func resolveInstrumentSet(symbols []string, provider string, oandaResolver, simResolver *instrument.MemoryResolver) (instrumentSet, error) {
	if len(symbols) == 0 {
		return instrumentSet{}, fmt.Errorf("at least one --symbol is required")
	}

	set := instrumentSet{
		oandaListing: make(map[string]instrument.Listing, len(symbols)),
		simListing:   make(map[string]instrument.Listing, len(symbols)),
	}
	seen := make(map[string]string, len(symbols)) // normalized symbol -> itself, recorded once seen so a duplicate can report it

	for _, raw := range symbols {
		symbol := strings.ToUpper(strings.TrimSpace(raw))

		// Duplicate detection happens here, against the normalized
		// symbol string directly, deliberately before either resolver
		// is touched (issue #224 review, point 3): registering the same
		// (provider, symbol) pair twice would also be caught by
		// instrument.MemoryResolver.Register's own duplicate check, but
		// that error is worded for a resolver-internal audience, not a
		// CLI user, and would additionally leave a partially-registered
		// sim-side resolver from the first of the two colliding calls.
		if first, dup := seen[symbol]; dup {
			return instrumentSet{}, fmt.Errorf("duplicate --symbol %q: already requested as %q", symbol, first)
		}
		seen[symbol] = symbol

		instrumentID, err := svcmarketdata.RegisterFXInstrument(oandaResolver, provider, symbol)
		if err != nil {
			return instrumentSet{}, err
		}
		if _, err := svcmarketdata.RegisterFXInstrument(simResolver, "sim", symbol); err != nil {
			return instrumentSet{}, err
		}
		simListing, err := simResolver.ResolveInstrument(instrumentID, "sim", "")
		if err != nil {
			return instrumentSet{}, err
		}
		oandaListing, err := oandaResolver.ResolveInstrument(instrumentID, provider, "")
		if err != nil {
			return instrumentSet{}, err
		}

		key := instrumentID.String()
		set.ids = append(set.ids, instrumentID)
		set.oandaListing[key] = oandaListing
		set.simListing[key] = simListing
	}

	sort.Slice(set.ids, func(i, j int) bool { return set.ids[i].String() < set.ids[j].String() })
	return set, nil
}

// runBacktest is newRunCmd's own RunE, split out so its own control
// flow is easy to read top to bottom: resolve effective configuration
// (flags/--config/defaults, issue #247) -> resolve every requested
// instrument (canonically, order-independently) -> publish canonical
// data for each if needed -> select and configure a strategy and its
// matching FillPriceSource (the EMA crossover strategy plus a general
// per-bar-lookup price source when --config is given, issue #252;
// otherwise the demo strategy plus its precomputed one-shot price, as
// before) -> build the concrete EnvironmentFactory -> call
// service/backtest.Run -> project into a report.BacktestReport once ->
// persist that same projection -> render it. No backtest orchestration
// happens here: service/backtest.Service.Run is the only thing that
// drives a replay, and it drives exactly one Scheduler/account/
// pipeline regardless of how many instruments were requested (issue
// #224's own "no per-symbol backtest engine fork" acceptance
// criterion).
func runBacktest(cmd *cobra.Command, flags runFlags) error {
	ctx := cmd.Context()

	cfg, err := buildRunConfig(cmd, flags)
	if err != nil {
		return err
	}

	symbols, err := effectiveSymbols(flags.symbols, cfg.Backtest.Symbol)
	if err != nil {
		return err
	}

	interval, err := parseInterval(cfg.Backtest.Interval)
	if err != nil {
		return err
	}
	from, err := parseDate(cfg.Backtest.From)
	if err != nil {
		return err
	}
	to, err := parseDate(cfg.Backtest.To)
	if err != nil {
		return err
	}
	span, err := marketdata.NewTimeRange(from, to)
	if err != nil {
		return fmt.Errorf("invalid backtest.from/backtest.to range: %w", err)
	}

	currency, err := num.ParseCurrency(cfg.Backtest.Currency)
	if err != nil {
		return fmt.Errorf("invalid backtest.currency: %w", err)
	}
	startingCash, err := num.ParseMoney(cfg.Backtest.StartingCapital, currency)
	if err != nil {
		return fmt.Errorf("invalid backtest.starting_capital: %w", err)
	}
	riskFraction := cfg.Backtest.RiskFraction
	adverseDistance := cfg.Backtest.AdverseDistance

	storeRoot := flags.dataStoreRoot
	if storeRoot == "" {
		dir, err := os.MkdirTemp("", "trader-backtest-store-")
		if err != nil {
			return fmt.Errorf("creating temporary data store: %w", err)
		}
		defer func() { _ = os.RemoveAll(dir) }()
		storeRoot = dir
	}

	oandaResolver := instrument.NewMemoryResolver()
	simResolver := instrument.NewMemoryResolver()
	instruments, err := resolveInstrumentSet(symbols, flags.provider, oandaResolver, simResolver)
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

	// Ensure canonical data is published for every requested instrument
	// before either strategy path below reads it.
	for _, instrumentID := range instruments.ids {
		plan, err := manager.Plan(ctx, marketdata.BarQuery{Instrument: instrumentID, Interval: interval, Range: span})
		if err != nil {
			return err
		}
		if len(plan.Actions) > 0 {
			if _, err := manager.Build(ctx, plan); err != nil {
				return err
			}
		}
	}

	var strat strategy.Strategy
	var strategyParams any
	var prices simbroker.FillPriceSource

	if flags.config != "" {
		// --config describes a single-instrument EMA crossover
		// experiment (buildRunConfig already rejected combining it
		// with more than one --symbol), so instruments.ids has exactly
		// one entry here.
		instID := instruments.ids[0]
		listing := instruments.simListing[instID.String()]

		emaStrategy, err := emacross.New(instID, interval, emacross.Config{
			FastPeriod: cfg.Strategy.FastPeriod,
			SlowPeriod: cfg.Strategy.SlowPeriod,
		})
		if err != nil {
			return err
		}

		src := newNextBarOpenPriceSource()
		if err := src.load(ctx, manager, listing.Symbol(), marketdata.BarQuery{Instrument: instID, Interval: interval, Range: span}); err != nil {
			return fmt.Errorf("loading canonical prices for %s: %w", instID, err)
		}

		strat = emaStrategy
		strategyParams = emaStrategy.Config()
		prices = src
	} else {
		// prices accumulates one precomputed next-bar-open fill price
		// per instrument (never a live per-bar feed — see
		// simPriceSource's own doc comment for why that is sufficient
		// and correct for this provisional demo strategy specifically,
		// and why it must not be mistaken for a general multi-bar
		// portfolio fill model).
		precomputed := make(map[string]num.Price, len(instruments.ids))
		for _, instrumentID := range instruments.ids {
			fillPrice, err := nextBarOpenAfterEntry(ctx, manager, instrumentID, interval, span, flags.warmupBars)
			if err != nil {
				return fmt.Errorf("computing %s's next-bar-open fill price: %w", instrumentID, err)
			}
			listing := instruments.simListing[instrumentID.String()]
			precomputed[listing.Symbol()] = fillPrice
		}

		strat = newDemoStrategy(instruments.ids, interval, flags.warmupBars)
		prices = simPriceSource(precomputed)
	}

	factory := environmentFactory{prices: prices}

	svc, err := svcbacktest.New(manager, simResolver, factory, clictx.LoggerFromContext(ctx))
	if err != nil {
		return err
	}

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:           strat,
		StrategyParameters: strategyParams,
		Span:               span,
		StartingCapital:    startingCash,
		RiskFraction:       riskFraction,
		AdverseDistance:    adverseDistance,
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
// following demoStrategy's own entry bar for instrumentID — the exact
// price Scheduler's next-bar-open fill-eligibility rule (issue #214)
// actually fills a market order at, never the entry bar's own Close
// (PR #240 review). Scheduler calls Strategy.OnBar for every bar,
// including each of the first warmupBars warm-up bars — it discards
// whatever intent OnBar returns during warm-up itself, it does not
// suppress the call (PR #240 second-review correction; demo_strategy.go's
// own doc comment records the same rule). demoStrategy tracks these
// callbacks itself and deliberately withholds its Enter intent until
// callback/bar index warmupBars — the first one Scheduler actually
// honors — so that bar is the entry bar, and the fill bar is the one
// immediately after it, index warmupBars+1. This function reads and
// discards exactly warmupBars+1 bars before returning the following
// bar's Open. Each instrument's own entry/fill bar is computed
// independently, since demoStrategy enters each instrument on that
// instrument's own first bar, not a shared portfolio-wide bar index.
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
