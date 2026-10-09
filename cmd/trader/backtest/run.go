package backtest

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/internal/backtestcfg"
	"github.com/rustyeddy/trader/cmd/internal/marketdatacfg"
	"github.com/rustyeddy/trader/cmd/trader/internal/clictx"
	"github.com/rustyeddy/trader/internal/adapters/journal/jsonl"
	"github.com/rustyeddy/trader/internal/journal"
	svcbacktest "github.com/rustyeddy/trader/internal/service/backtest"
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

	initialMarginRatio string

	config       string
	strategyName string
	fastPeriod   int
	slowPeriod   int
	allowedSide  string

	quantity string
	buyDate  string
	sellDate string

	strategyExec   string
	strategyArgs   []string
	strategyConfig string

	dataStoreRoot string
	dataRawRoot   string
	provider      string

	outputDir string
	format    string
	journal   string
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
			"--config supplies backtest and generic strategy parameters from a YAML file\n" +
			"strategy.name selects a registered in-process strategy; any explicit\n" +
			"flag above still overrides its corresponding config-file value.\n\n" +
			"--strategy-exec runs an out-of-tree strategy executable instead\n" +
			"(issue #382, ADR-062/ADR-063): trader launches it, completes\n" +
			"Strategy Protocol v1's Handshake, and drives it exactly like an\n" +
			"in-tree strategy for the rest of the run -- the executable's own\n" +
			"Descriptor determines the instrument/interval universe, so\n" +
			"--symbol/--interval must still be given to publish canonical\n" +
			"data for whatever it will actually request. --strategy-args\n" +
			"passes extra arguments to the executable unmodified;\n" +
			"--strategy-config forwards a config file path via the " + backtestcfg.StrategyConfigPathEnv + "\n" +
			"environment variable, never parsed by trader itself. With --config,\n" +
			"strategy.exec and strategy.config set the same two values, and\n" +
			"backtest.symbols (comma-separated) supplies a multi-instrument\n" +
			"universe, so a config file alone can drive an external strategy\n" +
			"(issue #469); explicit flags still override it.\n\n" +
			"--journal optionally writes a durable JSONL audit trail of\n" +
			"the run (adapters/journal/jsonl); off by default, and never\n" +
			"read back by 'show' (see the package doc comment).",
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
	cmd.Flags().StringVar(&flags.initialMarginRatio, "initial-margin-ratio", "1", "account initial-margin ratio (ADR-066): equity required per unit of gross notional; 1 is unlevered, 0.5 permits 2x, 0.25 permits 4x")

	cmd.Flags().StringVar(&flags.config, "config", "", "YAML config file supplying backtest/strategy parameters (issue #247); explicit flags above always override it")
	cmd.Flags().StringVar(&flags.strategyName, "strategy-name", "", "in-process strategy name selected by --config")
	cmd.Flags().IntVar(&flags.fastPeriod, "fast-period", 0, "EMA fast period; used by the ema-cross strategy")
	cmd.Flags().IntVar(&flags.slowPeriod, "slow-period", 0, "EMA slow period; used by the ema-cross strategy")
	cmd.Flags().StringVar(&flags.allowedSide, "allowed-side", "", "restrict ema-cross to one position direction: both, long-only, or short-only")
	cmd.Flags().StringVar(&flags.quantity, "quantity", "", "buy-and-hold quantity mode: buy exactly this quantity of the single --symbol (never sized or resized)")
	cmd.Flags().StringVar(&flags.buyDate, "buy-date", "", "buy-and-hold quantity mode: buy on the first bar at or after this date (default: the first bar)")
	cmd.Flags().StringVar(&flags.sellDate, "sell-date", "", "buy-and-hold quantity mode: exit on the first bar at or after this date (default: hold through the run's end)")

	cmd.Flags().StringVar(&flags.strategyExec, "strategy-exec", "", "path to an out-of-tree strategy executable, launched and driven over Strategy Protocol v1 (ADR-062/ADR-063) instead of an in-tree strategy; may also be set as strategy.exec in --config")
	cmd.Flags().StringArrayVar(&flags.strategyArgs, "strategy-args", nil, "extra argument passed to the external strategy executable, unmodified; repeatable, in order; requires --strategy-exec or strategy.exec")
	cmd.Flags().StringVar(&flags.strategyConfig, "strategy-config", "", "path to a config file for --strategy-exec's own executable; forwarded as the "+backtestcfg.StrategyConfigPathEnv+" environment variable, never parsed by trader itself; requires an executable from --strategy-exec or strategy.exec; the same path may instead be set as strategy.config in --config")

	cmd.Flags().StringVar(&flags.dataStoreRoot, "data-store-root", "", "canonical data store root (default: /srv/trading/data/canonical, per --config/config-file/env precedence; an explicit empty value opts back into a fresh temporary directory per run)")
	cmd.Flags().StringVar(&flags.dataRawRoot, "data-raw-root", "", "raw archive root (required, or supplied by --config)")
	cmd.Flags().StringVar(&flags.provider, "provider", "", "market data provider name (default: oanda, or backtest.provider from --config)")

	cmd.Flags().StringVar(&flags.outputDir, "output-dir", "", "directory run snapshots are written to and 'show' reads from (default: backtest.output_dir, TRADER_BACKTEST_OUTPUT_DIR, or "+backtestcfg.DefaultOutputDir+")")
	cmd.Flags().StringVar(&flags.format, "format", formatTable, "output format: "+formatTable+", "+formatJSON+", or "+formatOrg)
	cmd.Flags().StringVar(&flags.journal, "journal", "", "optional path to write a durable JSONL journal of this run (adapters/journal/jsonl); path must not already exist")

	// --symbol/--from/--to/--adverse-distance/--data-raw-root are no longer cobra-required:
	// each is also satisfiable from --config (issue #247), so their
	// presence is instead enforced uniformly by buildRunConfig's
	// config.Load call, which aggregates every missing/invalid field into
	// one error rather than cobra stopping at the first missing flag.

	return cmd
}

// validateStrategyFlags is the early, config-free half of the external-
// strategy rules: with neither --strategy-exec nor a --config that could
// supply strategy.exec, --strategy-args and --strategy-config have no
// executable to apply to, and fail before anything else is read.
func validateStrategyFlags(cmd *cobra.Command, flags runFlags) error {
	if flags.strategyExec != "" || flags.config != "" {
		return nil
	}
	if cmd.Flags().Changed("strategy-args") {
		return fmt.Errorf("--strategy-args requires --strategy-exec or strategy.exec")
	}
	if cmd.Flags().Changed("strategy-config") {
		return fmt.Errorf("--strategy-config requires --strategy-exec or strategy.exec")
	}
	return nil
}

// validateStrategySelection enforces the external-strategy rules once
// the effective config is known (issue #382's "invalid combinations fail
// before run starts" criterion, and #469): the executable may now come
// from --strategy-exec or strategy.exec, so --strategy-args and
// --strategy-config are meaningless (and therefore rejected, rather than
// silently ignored) only when neither names one. A config may also carry
// a multi-instrument universe only for an external strategy, whose own
// Descriptor determines what it trades; --config with more than one
// --symbol or backtest.symbols remains rejected for in-process
// strategies, whose config describes a single experiment; symbols is the
// effective universe from effectiveSymbols, so a YAML-carried universe is
// checked as well as repeated flags.
func validateStrategySelection(cmd *cobra.Command, flags runFlags, cfg backtestcfg.RunConfig, symbols []string) error {
	if cfg.Strategy.Exec == "" {
		if cmd.Flags().Changed("strategy-args") {
			return fmt.Errorf("--strategy-args requires --strategy-exec or strategy.exec")
		}
		if cmd.Flags().Changed("strategy-config") {
			return fmt.Errorf("--strategy-config requires --strategy-exec or strategy.exec")
		}
		if flags.config != "" && len(symbols) > 1 {
			return fmt.Errorf("--config describes a single-instrument experiment for an in-process strategy; " +
				"a multi-instrument universe (repeated --symbol or backtest.symbols) needs strategy.exec, " +
				"or repeat --symbol without --config")
		}
	}
	return nil
}

// effectiveSymbols reconciles --symbol (repeatable, multi-instrument,
// issue #224) with backtest.symbol (single-instrument, issue #247) and
// backtest.symbols (a comma-separated universe, issue #469) from
// --config: explicit --symbol flags always win when present, and the
// config values are only a fallback when no --symbol flag was given at
// all. (Config validation already rejects setting both config keys.)
func effectiveSymbols(flagSymbols []string, configSymbol, configSymbols string) ([]string, error) {
	if len(flagSymbols) > 0 {
		return flagSymbols, nil
	}
	if configSymbols != "" {
		var out []string
		for _, s := range strings.Split(configSymbols, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if configSymbol != "" {
		return []string{configSymbol}, nil
	}
	return nil, fmt.Errorf("at least one --symbol, or backtest.symbol/backtest.symbols in --config, is required")
}

// runBacktest is newRunCmd's own RunE: resolve the effective
// configuration (flags, --config, environment, defaults; issue #247),
// open the market data and --journal this invocation uses, run the
// backtest through the shared composition (backtestcfg.Run, the same
// path trader-mcp's trader_run_backtest takes), then persist and render
// its report. No backtest orchestration lives here (issue #453,
// ADR-069).
func runBacktest(cmd *cobra.Command, flags runFlags) error {
	ctx := cmd.Context()
	logger := clictx.LoggerFromContext(ctx)

	if err := validateStrategyFlags(cmd, flags); err != nil {
		return err
	}
	cfg, err := buildRunConfig(cmd, flags)
	if err != nil {
		return err
	}
	if cfg.Backtest.DataRawRoot == "" {
		return fmt.Errorf("backtest.data_raw_root is required (set it in --config or pass --data-raw-root)")
	}
	symbols, err := effectiveSymbols(flags.symbols, cfg.Backtest.Symbol, cfg.Backtest.Symbols)
	if err != nil {
		return err
	}
	if err := validateStrategySelection(cmd, flags, cfg, symbols); err != nil {
		return err
	}
	outputDir, err := backtestcfg.LoadOutputDir(os.Environ(), flags.config, changedFlag(cmd, "output-dir", flags.outputDir))
	if err != nil {
		return err
	}

	storeRoot := cfg.Backtest.DataStoreRoot
	if storeRoot == "" {
		dir, err := os.MkdirTemp("", "trader-backtest-store-")
		if err != nil {
			return fmt.Errorf("creating temporary data store: %w", err)
		}
		defer func() { _ = os.RemoveAll(dir) }()
		storeRoot = dir
	}
	data, err := marketdatacfg.New(marketdatacfg.Config{
		StoreRoot: storeRoot, RawRoot: cfg.Backtest.DataRawRoot, Provider: cfg.Backtest.Provider,
	}, logger)
	if err != nil {
		return err
	}

	var ext *backtestcfg.ExternalStrategy
	if cfg.Strategy.Exec != "" {
		ext = &backtestcfg.ExternalStrategy{
			Exec: cfg.Strategy.Exec, Args: flags.strategyArgs, Config: cfg.Strategy.Config, Environ: os.Environ(),
		}
	}

	var jrnl journal.Recorder
	var journalWriter *jsonl.Writer
	if flags.journal != "" {
		w, err := jsonl.NewWriter(flags.journal)
		if err != nil {
			return fmt.Errorf("opening --journal: %w", err)
		}
		// Close is idempotent, so this deferred call only does real work
		// on an early return; the explicit Close below is what reports a
		// failure, since jsonl.Writer fsyncs only in Close (PR #267
		// review).
		defer func() { _ = w.Close() }()
		journalWriter = w
		jrnl = w
	}

	// A model (study) run trades nothing: count its signals so the summary
	// printed in place of the backtest report can show them.
	var counter *signalCounter
	if cfg.IsModel() {
		counter = &signalCounter{inner: jrnl}
		jrnl = counter
	}

	rep, err := backtestcfg.Run(ctx, backtestcfg.Request{
		Config:      cfg,
		Symbols:     symbols,
		WarmupBars:  flags.warmupBars,
		External:    ext,
		Data:        data,
		PrepareData: true,
		Journal:     jrnl,
		Logger:      logger,
	})
	if err != nil {
		return err
	}
	if journalWriter != nil {
		if err := journalWriter.Close(); err != nil {
			return fmt.Errorf("closing --journal: %w", err)
		}
	}

	if err := svcbacktest.NewRunStore(outputDir).Save(rep); err != nil {
		return err
	}
	if counter != nil {
		return renderStudy(cmd.OutOrStdout(), flags.format, newStudySummary(rep, counter.signals, flags.journal))
	}
	return render(cmd.OutOrStdout(), flags.format, rep)
}

// changedFlag returns value if the flag name was set on cmd, else "".
func changedFlag(cmd *cobra.Command, name, value string) string {
	if cmd.Flags().Changed(name) {
		return value
	}
	return ""
}
