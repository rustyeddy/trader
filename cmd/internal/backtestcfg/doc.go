// Package backtestcfg is the shared backtest composition (issue #453,
// ADR-069): everything a transport needs to run a backtest beyond parsing
// its own request and rendering the result. Both "trader backtest run"
// and trader-mcp's trader_run_backtest call Run, so a backtest composes
// identically whichever transport asks for it.
//
// It owns the run configuration model (RunConfig, loaded by the CLI from
// flags, --config, and the environment), strategy construction, the
// fill-price sources, and the simulated broker/pipeline environment
// (environmentFactory). Those are concrete adapters, which ADR-039 keeps
// out of service/backtest; this package is where they are wired, once.
// Data access goes through service/marketdata (instrument resolution and
// canonical-data preparation), and run persistence and report assembly
// through service/backtest (RunStore, NewReport).
//
// # Strategy selection paths
//
// Run selects one of these strategies: an external executable when
// Request.External is set, otherwise the in-process strategy named by
// RunConfig.Strategy.Name (the CLI's --config strategy.name, or MCP's
// strategy input):
//
//   - buy-and-hold (the default name) without a quantity: an unexported demoStrategy (demo_strategy.go)
//     that enters long once per requested instrument, on that
//     instrument's own first bar, and never trades that instrument
//     again. --symbol may be repeated (issue #224, M5-16) to run a
//     multi-instrument portfolio backtest with it — one Scheduler and
//     one shared account/pipeline still replay every requested
//     instrument, never a per-symbol engine. demoStrategy exists
//     solely so this command is genuinely executable end to end
//     without any real strategy configured; it is not a real trading
//     strategy.
//   - ema-cross (strategy/emacross), or buy-and-hold in quantity mode
//     (issue #417); strategy-specific fields are interpreted only by
//     the selected strategy. Its FillPriceSource (nextBarOpenPriceSource,
//     environment.go) is a general per-bar-lookup implementation, unlike
//     demoStrategy's precomputed single-fill price, because a
//     crossover strategy enters, exits, and re-enters at run-dependent
//     bars.
//   - An external strategy (issue #382, the CLI's --strategy-exec): an out-of-tree strategy
//     executable, launched via adapters/strategy/external.Launch
//     (ADR-063) and driven over Strategy Protocol v1 (ADR-062) exactly
//     like any other strategy.Strategy from here on — buildStrategy (run.go) is the composition root that owns
//     Process construction and its Stop-on-return lifecycle; Scheduler
//     and the rest of the M5 pipeline never know the strategy they are
//     driving is out-of-process. Its own Descriptor (received at
//     Handshake), not --symbol, determines the replay universe;
//     --symbol/--interval still control what canonical data this
//     command publishes beforehand, and must cover whatever the
//     executable will actually request.
//     The executable, its forwarded config file and a multi-instrument
//     universe may all come from the run config instead of flags
//     (strategy.exec, strategy.config, backtest.symbols; issue #469). Like nextBarOpenPriceSource
//     above, it gets a general per-bar-lookup FillPriceSource, since an
//     external strategy's entry/exit timing is exactly as
//     run-dependent as EMA crossover's.
//
// service/backtest.RunRequest.Strategy remains the real application
// contract either way; this command constructs a concrete value for
// it, never a second orchestration path.
//
// # Market data
//
// A run reads the market data the caller supplies (Request.Data, built by
// marketdatacfg with the provider's calendar). With Request.PrepareData,
// Run builds any canonical data the plan says is needed first, as the CLI
// always has; without it, data that is not all current fails with
// ErrDataNotReady rather than being built.
package backtestcfg
