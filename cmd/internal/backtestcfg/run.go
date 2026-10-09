package backtestcfg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/rustyeddy/trader/cmd/internal/marketdatacfg"
	simbroker "github.com/rustyeddy/trader/internal/adapters/broker/sim"
	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
	"github.com/rustyeddy/trader/internal/journal"
	"github.com/rustyeddy/trader/internal/logging"
	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	"github.com/rustyeddy/trader/internal/report"
	svcbacktest "github.com/rustyeddy/trader/internal/service/backtest"
	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/internal/strategy/emacross"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	strategyv1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// ErrDataNotReady reports a run that may not build canonical data
// (Request.PrepareData false) over a range whose canonical data is not
// all current.
var ErrDataNotReady = errors.New("canonical market data is not ready for this backtest")

// ErrInvalidRun marks a run rejected for its input: an invalid
// configuration, symbol set, or value. Errors carrying it keep their own
// message (errors.Is matches; the text is unchanged), so a transport can
// show them to its caller as describing only what was asked for.
var ErrInvalidRun = errors.New("invalid backtest request")

// inputError marks err with ErrInvalidRun without changing its message.
type inputError struct{ err error }

func (e inputError) Error() string        { return e.err.Error() }
func (e inputError) Unwrap() error        { return e.err }
func (e inputError) Is(target error) bool { return target == ErrInvalidRun }

func invalid(err error) error {
	if err == nil {
		return nil
	}
	return inputError{err: err}
}

// Run stages reported through Request.Progress.
const (
	StagePreparingData = "preparing market data"
	StageRunning       = "running backtest"
)

// Request is one backtest run, as any transport describes it.
type Request struct {
	// Config is the run configuration. Run validates it
	// (RunConfig.Validate) before doing any work.
	Config RunConfig
	// Symbols is the run's instrument universe: at least one symbol,
	// with no duplicates.
	Symbols []string
	// WarmupBars applies to the buy-and-hold demo strategy only.
	WarmupBars int
	// External, if set, runs an out-of-tree strategy executable instead
	// of an in-process one (ADR-062/ADR-063).
	External *ExternalStrategy
	// Data is the market data the run reads, for Config.Backtest.Provider.
	Data marketdatacfg.Bundle
	// PrepareData builds missing or stale canonical data before the run.
	// Without it, data that is not all current fails the run with
	// ErrDataNotReady.
	PrepareData bool
	// Journal optionally records the run durably.
	Journal journal.Recorder
	// Logger receives the run's logs; nil discards them.
	Logger *slog.Logger
	// Progress, if set, is called as the run enters each stage
	// (StagePreparingData, StageRunning), so a transport can report
	// progress on a long run.
	Progress func(stage string)
}

// Run composes and runs one backtest — resolve instruments, ensure
// canonical data, construct the strategy and its fill-price source,
// build the simulated environment, and run it through service/backtest —
// and returns the run's report model (svcbacktest.NewReport). It neither
// persists nor renders the report; transports do (svcbacktest.RunStore,
// their own renderers).
func Run(ctx context.Context, req Request) (report.BacktestReport, error) {
	resp, err := run(ctx, req)
	if err != nil {
		return report.BacktestReport{}, err
	}
	return svcbacktest.NewReport(resp), nil
}

func run(ctx context.Context, req Request) (svcbacktest.RunResponse, error) {
	cfg := req.Config
	// Run owns its input contract rather than trusting every transport
	// to have validated: an unregistered strategy name must fail here,
	// never fall through to the default strategy.
	if err := cfg.Validate(); err != nil {
		return svcbacktest.RunResponse{}, invalid(err)
	}
	// strategy.exec names an executable, but launching it is the caller's
	// job (it owns Environ and --strategy-args): without an ExternalStrategy
	// the run would silently use an in-process strategy instead.
	if cfg.Strategy.Exec != "" && req.External == nil {
		return svcbacktest.RunResponse{}, invalid(fmt.Errorf("strategy.exec is set but the request carries no ExternalStrategy"))
	}
	progress := req.Progress
	if progress == nil {
		progress = func(string) {}
	}
	logger := req.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	if req.Data.Manager == nil || req.Data.Service == nil {
		return svcbacktest.RunResponse{}, fmt.Errorf("backtestcfg: market data is not configured")
	}

	interval, err := svcmarketdata.ParseInterval(cfg.Backtest.Interval)
	if err != nil {
		return svcbacktest.RunResponse{}, invalid(err)
	}
	span, err := svcmarketdata.ParseRange(cfg.Backtest.From, cfg.Backtest.To)
	if err != nil {
		return svcbacktest.RunResponse{}, invalid(fmt.Errorf("invalid backtest.from/backtest.to range: %w", err))
	}
	currency, err := num.ParseCurrency(cfg.Backtest.Currency)
	if err != nil {
		return svcbacktest.RunResponse{}, invalid(fmt.Errorf("invalid backtest.currency: %w", err))
	}
	startingCash, err := num.ParseMoney(cfg.Backtest.StartingCapital, currency)
	if err != nil {
		return svcbacktest.RunResponse{}, invalid(fmt.Errorf("invalid backtest.starting_capital: %w", err))
	}
	if sign, err := startingCash.Cmp(num.MustParseMoney("0", currency)); err != nil || sign <= 0 {
		return svcbacktest.RunResponse{}, invalid(fmt.Errorf("backtest.starting_capital must be positive, got %s", cfg.Backtest.StartingCapital))
	}
	if req.WarmupBars < 0 {
		return svcbacktest.RunResponse{}, invalid(fmt.Errorf("warmup bars must not be negative, got %d", req.WarmupBars))
	}

	// ema-cross and quantity mode each trade exactly one instrument;
	// reject more before any data is prepared rather than silently
	// running only the first.
	if req.External == nil && cfg.Strategy.Name == emacross.Name && len(req.Symbols) != 1 {
		return svcbacktest.RunResponse{}, invalid(fmt.Errorf("%s trades exactly one instrument; got %d symbols", emacross.Name, len(req.Symbols)))
	}
	if cfg.Strategy.quantityMode() {
		if req.External != nil {
			return svcbacktest.RunResponse{}, invalid(fmt.Errorf("--quantity applies to the in-process buy-and-hold strategy, not --strategy-exec"))
		}
		if len(req.Symbols) != 1 {
			return svcbacktest.RunResponse{}, invalid(fmt.Errorf("buy-and-hold quantity mode trades exactly one instrument; got %d symbols", len(req.Symbols)))
		}
	}

	simResolver := newSimResolver()
	instruments, err := resolveInstrumentSet(req.Symbols, req.Data.Provider, req.Data.Resolver, simResolver)
	if err != nil {
		return svcbacktest.RunResponse{}, invalid(err)
	}
	manager := req.Data.Manager

	progress(StagePreparingData)
	if err := ensureCanonicalData(ctx, req.Data.Service, instruments, interval, span, req.PrepareData); err != nil {
		return svcbacktest.RunResponse{}, err
	}

	strat, strategyParams, prices, externalProcess, err := buildStrategy(ctx, req, instruments, interval, span, manager, logger)
	if err != nil {
		return svcbacktest.RunResponse{}, err
	}
	if externalProcess != nil {
		// Stop is safe whatever happens below; a background context lets
		// the process's own graceful-shutdown grace run even if ctx is
		// already done (ADR-063).
		defer func() { _ = externalProcess.Stop(context.Background()) }()
	}

	factory := environmentFactory{prices: prices, journal: req.Journal, initialMarginRatio: cfg.Backtest.InitialMarginRatio}
	svc, err := svcbacktest.New(manager, simResolver, factory, logger)
	if err != nil {
		return svcbacktest.RunResponse{}, err
	}

	progress(StageRunning)
	// A nil *external.Process assigned to the interface would be a
	// non-nil interface wrapping nil; keep monitor a true nil instead.
	var monitor processMonitor
	if externalProcess != nil {
		monitor = externalProcess
	}
	resp, err := runWithExternalProcessMonitor(ctx, svc, svcbacktest.RunRequest{
		Strategy:           strat,
		StrategyParameters: strategyParams,
		Span:               span,
		StartingCapital:    startingCash,
		RiskFraction:       cfg.Backtest.RiskFraction,
		AdverseDistance:    cfg.Backtest.AdverseDistance,
	}, monitor)
	if errors.Is(err, svcbacktest.ErrInvalidRequest) {
		// The service's own request validation: still the caller's input.
		return svcbacktest.RunResponse{}, invalid(err)
	}
	if err != nil {
		return svcbacktest.RunResponse{}, err
	}

	// A successful run gets the external strategy's graceful Strategy
	// Protocol v1 shutdown before the deferred Stop's SIGTERM teardown
	// (ADR-063). Best-effort: Stop still terminates the child either way.
	if externalProcess != nil {
		if closer, ok := externalProcess.Strategy().(interface{ Close(context.Context) error }); ok {
			closeCtx, cancel := context.WithTimeout(context.Background(), external.DefaultShutdownGrace)
			if err := closer.Close(closeCtx); err != nil {
				logger.Warn("external strategy: graceful close failed, falling back to process termination", "error", err)
			}
			cancel()
		}
	}
	return resp, nil
}

// ensureCanonicalData makes sure canonical data covers span for every
// instrument. With prepare, it builds whatever the plan says is needed,
// through service/marketdata. Without it, it requires every partition in
// span to be current already.
func ensureCanonicalData(ctx context.Context, data *svcmarketdata.Service, instruments instrumentSet, interval marketdata.Interval, span marketdata.TimeRange, prepare bool) error {
	for _, instrumentID := range instruments.ids {
		dataset := svcmarketdata.DatasetRequest{Instrument: instrumentID, Interval: interval, Range: span}
		if !prepare {
			cov, err := data.Coverage(ctx, svcmarketdata.CoverageRequest{DatasetRequest: dataset})
			if err != nil {
				return err
			}
			for _, p := range cov.Coverage.Partitions {
				if p.Status != marketruntime.PartitionCoverageCurrent {
					return fmt.Errorf("%w: %s %s %04d-%02d is %s; canonicalize it first",
						ErrDataNotReady, instruments.symbol(instrumentID), interval, p.Year, int(p.Month), p.Status)
				}
			}
			continue
		}
		plan, err := data.Plan(ctx, svcmarketdata.PlanRequest{DatasetRequest: dataset})
		if err != nil {
			return err
		}
		if len(plan.Plan.Actions) > 0 {
			if _, err := data.Build(ctx, svcmarketdata.BuildRequest{DatasetRequest: dataset}); err != nil {
				return err
			}
		}
	}
	return nil
}

// buildStrategy selects and constructs the run's strategy and its
// fill-price source: an external executable, ema-cross, buy-and-hold in
// quantity mode, or the buy-and-hold demo.
func buildStrategy(ctx context.Context, req Request, instruments instrumentSet, interval marketdata.Interval, span marketdata.TimeRange, manager *marketruntime.Manager, logger *slog.Logger) (strategy.Strategy, any, simbroker.FillPriceSource, *external.Process, error) {
	cfg := req.Config
	loadPrices := func(ids ...string) (*nextBarOpenPriceSource, error) {
		src := newNextBarOpenPriceSource()
		for _, key := range ids {
			instID := instruments.byKey[key]
			listing := instruments.simListing[key]
			if err := src.load(ctx, manager, listing.Symbol(), marketruntime.BarQuery{Instrument: instID, Interval: interval, Range: span}); err != nil {
				return nil, fmt.Errorf("loading canonical prices for %s: %w", instID, err)
			}
		}
		return src, nil
	}
	firstKey := instruments.ids[0].String()

	switch {
	case req.External != nil:
		// The external strategy's own Descriptor, received in its
		// Handshake, is the replay universe (ADR-042); Symbols and
		// Interval only decide what canonical data was prepared for it.
		launchCfg, execAbs, strategyConfigAbs, err := buildExternalLaunchConfig(*req.External, logger)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		// Digest the exact resolved paths before Launch, so a file
		// replaced during startup cannot make the recorded digest
		// describe different bytes than what ran.
		execDigest, err := fileContentDigest(ctx, execAbs)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		var configDigest string
		if strategyConfigAbs != "" {
			if configDigest, err = fileContentDigest(ctx, strategyConfigAbs); err != nil {
				return nil, nil, nil, nil, err
			}
		}
		process, err := external.Launch(ctx, launchCfg)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("launching --strategy-exec %s: %w", req.External.Exec, err)
		}
		keys := make([]string, len(instruments.ids))
		for i, id := range instruments.ids {
			keys[i] = id.String()
		}
		src, err := loadPrices(keys...)
		if err != nil {
			_ = process.Stop(context.Background())
			return nil, nil, nil, nil, err
		}
		strat := process.Strategy()
		descriptor := strat.Describe()
		params := externalStrategyParams{
			Mode:            "external",
			StrategyName:    descriptor.Name,
			StrategyVersion: descriptor.Version,
			ProtocolVersion: strategyv1.ProtocolVersion,
			Transport:       "unix",
			Exec:            execAbs,
			ExecDigest:      execDigest,
			ConfigDigest:    configDigest,
			Args:            req.External.Args,
			Config:          strategyConfigAbs,
		}
		return strat, params, src, process, nil

	case cfg.Strategy.Name == emacross.Name:
		// EMA crossover is a single-instrument strategy.
		instID := instruments.ids[0]
		ema, err := emacross.New(instID, interval, emacross.Config{
			FastPeriod: cfg.Strategy.FastPeriod, SlowPeriod: cfg.Strategy.SlowPeriod, AllowedSide: cfg.Strategy.AllowedSide,
		})
		if err != nil {
			return nil, nil, nil, nil, err
		}
		src, err := loadPrices(firstKey)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		return ema, ema.Config(), src, nil, nil

	case cfg.Strategy.quantityMode():
		// Buy-and-hold quantity mode (issue #417): one instrument, an
		// exact quantity, filled from the per-bar next-open source so an
		// exit on sell_date fills at its own bar.
		settings, err := cfg.Strategy.parseBuyHold(span.Start())
		if err != nil {
			return nil, nil, nil, nil, err
		}
		src, err := loadPrices(firstKey)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		bh := newBuyHoldQuantity(instruments.ids[0], interval, settings.quantity, settings.buyDate, settings.sellDate, span.Start())
		return bh, bh.params, src, nil, nil

	default:
		// One precomputed next-bar-open fill price per instrument (see
		// simPriceSource for why that suffices for the demo strategy).
		precomputed := make(map[string]num.Price, len(instruments.ids))
		for _, instrumentID := range instruments.ids {
			fillPrice, err := nextBarOpenAfterEntry(ctx, manager, instrumentID, interval, span, req.WarmupBars)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("computing %s's next-bar-open fill price: %w", instrumentID, err)
			}
			precomputed[instruments.simListing[instrumentID.String()].Symbol()] = fillPrice
		}
		return newDemoStrategy(instruments.ids, interval, req.WarmupBars), nil, simPriceSource(precomputed), nil, nil
	}
}
