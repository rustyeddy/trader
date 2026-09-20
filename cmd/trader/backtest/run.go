package backtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/trader/internal/clictx"
	"github.com/rustyeddy/trader/instrument"
	simbroker "github.com/rustyeddy/trader/internal/adapters/broker/sim"
	"github.com/rustyeddy/trader/internal/adapters/journal/jsonl"
	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/internal/journal"
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

// strategyConfigPathEnv names the environment variable "trader
// backtest run" sets on an external strategy child process (alongside
// external.SocketPathEnv, which Launch always sets) when --strategy-
// config is given. This is a CLI-owned convention, not part of
// Strategy Protocol v1 itself (ADR-062 defines no config-handoff
// mechanism — a guest's own configuration schema is entirely its
// author's business); it exists only so an author who does want a
// config file does not have to invent their own argv convention.
// Named the same way external.SocketPathEnv is (an environment
// variable, not a CLI flag) for the identical reason that constant's
// own doc comment gives: no command-line argument-parsing convention
// can be assumed across every possible guest language/runtime.
const strategyConfigPathEnv = "TRADER_STRATEGY_CONFIG"

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
	allowedSide  string

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
			"--config supplies backtest/strategy parameters from a YAML file\n" +
			"(issue #247) and runs the real EMA crossover strategy\n" +
			"(issue #252) instead of the demo strategy, for a single\n" +
			"instrument; any explicit flag above still overrides its value.\n\n" +
			"--strategy-exec runs an out-of-tree strategy executable instead\n" +
			"(issue #382, ADR-062/ADR-063): trader launches it, completes\n" +
			"Strategy Protocol v1's Handshake, and drives it exactly like an\n" +
			"in-tree strategy for the rest of the run -- the executable's own\n" +
			"Descriptor determines the instrument/interval universe, so\n" +
			"--symbol/--interval must still be given to publish canonical\n" +
			"data for whatever it will actually request. --strategy-args\n" +
			"passes extra arguments to the executable unmodified;\n" +
			"--strategy-config forwards a config file path via the " + strategyConfigPathEnv + "\n" +
			"environment variable, never parsed by trader itself. Mutually\n" +
			"exclusive with --config: there is no strategy registry to\n" +
			"select an in-tree strategy and an external one at once.\n\n" +
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

	cmd.Flags().StringVar(&flags.config, "config", "", "YAML config file supplying backtest/strategy parameters (issue #247); explicit flags above always override it")
	cmd.Flags().StringVar(&flags.strategyName, "strategy-name", "", "must equal \"ema-cross\" when --config is used (there is no strategy registry to select from; any other value is rejected)")
	cmd.Flags().IntVar(&flags.fastPeriod, "fast-period", 0, "EMA fast period; only used when --config is also given")
	cmd.Flags().IntVar(&flags.slowPeriod, "slow-period", 0, "EMA slow period; only used when --config is also given")
	cmd.Flags().StringVar(&flags.allowedSide, "allowed-side", "", "restrict the EMA strategy to one position direction: both (default), long-only, or short-only; only used when --config is also given")

	cmd.Flags().StringVar(&flags.strategyExec, "strategy-exec", "", "path to an out-of-tree strategy executable, launched and driven over Strategy Protocol v1 (ADR-062/ADR-063) instead of an in-tree strategy; mutually exclusive with --config")
	cmd.Flags().StringArrayVar(&flags.strategyArgs, "strategy-args", nil, "extra argument passed to --strategy-exec's own executable, unmodified; repeatable, in order; requires --strategy-exec")
	cmd.Flags().StringVar(&flags.strategyConfig, "strategy-config", "", "path to a config file for --strategy-exec's own executable; forwarded as the "+strategyConfigPathEnv+" environment variable, never parsed by trader itself; requires --strategy-exec")

	cmd.Flags().StringVar(&flags.dataStoreRoot, "data-store-root", "", "canonical data store root (default: /srv/trading/data/canonical, per --config/config-file/env precedence; an explicit empty value opts back into a fresh temporary directory per run)")
	cmd.Flags().StringVar(&flags.dataRawRoot, "data-raw-root", "", "raw archive root (required)")
	cmd.Flags().StringVar(&flags.provider, "provider", "oanda", "market data provider name")

	cmd.Flags().StringVar(&flags.outputDir, "output-dir", "./backtest-runs", "directory run snapshots are written to and 'show' reads from")
	cmd.Flags().StringVar(&flags.format, "format", formatTable, "output format: "+formatTable+", "+formatJSON+", or "+formatOrg)
	cmd.Flags().StringVar(&flags.journal, "journal", "", "optional path to write a durable JSONL journal of this run (adapters/journal/jsonl); path must not already exist")

	// --symbol/--from/--to/--adverse-distance are no longer cobra-required:
	// each is also satisfiable from --config (issue #247), so their
	// presence is instead enforced uniformly by buildRunConfig's
	// config.Load call, which aggregates every missing/invalid field into
	// one error rather than cobra stopping at the first missing flag.
	_ = cmd.MarkFlagRequired("data-raw-root")

	return cmd
}

// validateStrategySelection enforces --strategy-exec's own mutual-
// exclusivity rules before runBacktest does anything else (issue
// #382's own "invalid combinations fail before run starts" acceptance
// criterion) — there is no strategy registry, so --strategy-exec and
// --config can never both select a strategy for the same run, and
// --strategy-args/--strategy-config are meaningless (and therefore
// rejected, rather than silently ignored) without --strategy-exec
// naming an executable for them to apply to.
func validateStrategySelection(cmd *cobra.Command, flags runFlags) error {
	if flags.strategyExec == "" {
		if cmd.Flags().Changed("strategy-args") {
			return fmt.Errorf("--strategy-args requires --strategy-exec")
		}
		if cmd.Flags().Changed("strategy-config") {
			return fmt.Errorf("--strategy-config requires --strategy-exec")
		}
		return nil
	}
	if flags.config != "" {
		return fmt.Errorf("--strategy-exec cannot be combined with --config: there is no strategy registry to select between an in-tree and an external strategy")
	}
	return nil
}

// externalStrategyParams is the report.BacktestReport-visible record
// of how --strategy-exec launched the external strategy (never its
// own runtime StrategyDescriptor — that already becomes the
// manifest's own StrategyName via strat.Describe(), the same as any
// in-tree strategy). This is deliberately the only strategy-specific
// state this command records for an external run: the config file
// itself, if any, is opaque to trader (see strategyConfigPathEnv's own
// doc comment), so there is nothing further here that could be
// meaningfully validated or replayed.
type externalStrategyParams struct {
	// Mode is always "external" — a fixed, explicit marker
	// distinguishing this run's own manifest provenance shape from
	// the in-tree EMA/demo paths' own StrategyParameters shapes at a
	// glance (issue #385).
	Mode string `json:"mode"`
	// StrategyName/StrategyVersion are the external strategy's own
	// Descriptor (ADR-062), received during Handshake — the actual
	// strategy identity that ran, independent of --strategy-exec's
	// own path/name. Manifest.StrategyName() also already carries
	// StrategyName (via strat.Describe(), the same as any in-tree
	// strategy); it is repeated here so it travels with the rest of
	// this run's own external-specific provenance in one place.
	StrategyName    string `json:"strategy_name"`
	StrategyVersion string `json:"strategy_version"`
	// ProtocolVersion is the Strategy Protocol version this run
	// actually negotiated (ADR-062) — recorded so a manifest from a
	// future, incompatible protocol revision is distinguishable at a
	// glance, without cross-referencing the trader binary's own build
	// version.
	ProtocolVersion string `json:"protocol_version"`
	// Transport is always "unix" for Strategy Protocol v1 (ADR-063) —
	// the endpoint/transport kind issue #385 asks be recorded. No
	// other transport exists yet, but naming it explicitly avoids a
	// silent assumption once one does. Never the ephemeral Unix-domain
	// socket *path* itself: Launch generates a fresh one per run
	// (external.LaunchConfig.SocketPath's own doc comment), and issue
	// #385 explicitly excludes any such machine-specific, ephemeral
	// value from a run's semantic identity.
	Transport string `json:"transport"`
	// Exec is the resolved absolute path to the launched executable —
	// never a relative spelling that would resolve differently from a
	// different working directory (the same reason Config, below, is
	// already resolved to an absolute path).
	Exec string `json:"exec"`
	// ExecDigest is a "sha256:<hex>" content digest of the executable
	// at Exec, computed at launch time — matching backtest.Manifest.
	// ConfigDigest's own convention. Exec's path/name alone cannot
	// distinguish two different builds behind the identical
	// --strategy-exec path (issue #385's own explicit acceptance
	// criterion: "executable identity/digest is stable enough to
	// distinguish different builds").
	ExecDigest string   `json:"exec_digest"`
	Args       []string `json:"args,omitempty"`
	Config     string   `json:"config,omitempty"`
	// ConfigDigest is a "sha256:<hex>" content digest of the exact
	// bytes at Config, computed before launch (review finding: Config
	// alone records only a *path*, not the bytes at that path — a
	// config file edited in place between two runs using the identical
	// path would otherwise let both runs record the same provenance
	// despite the guest actually consuming different configuration,
	// defeating this issue's own reproducibility goal). Empty exactly
	// when Config is empty (no --strategy-config given).
	ConfigDigest string `json:"config_digest,omitempty"`
}

// fileContentDigest returns a deterministic "sha256:<hex>" content
// digest of the file at path — streamed via io.Copy into the hash
// rather than os.ReadFile (review finding: provenance hashing has no
// need to allocate an executable's entire content into memory at
// once) — matching backtest.Manifest.ConfigDigest's own "sha256:<hex>"
// convention. Shared by both the executable and the strategy config's
// own digest.
// ctx bounds this operation per the architecture document's own
// "Use context.Context on operations that may block, perform I/O, or
// span a use case" convention, matching every other I/O call in this
// file (src.load, nextBarOpenAfterEntry). Checked once, before
// opening the file: a canceled/expired ctx fails fast rather than
// still reading and hashing a potentially large executable no caller
// will use the result of. The read itself is local disk I/O expected
// to complete quickly, so io.Copy is not further split into a
// ctx-selecting loop mid-stream (review finding raised the missing
// parameter; this is the proportionate response for a bounded local
// file read, not network I/O).
func fileContentDigest(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("computing content digest: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("computing content digest: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// resolveStrategyExecutable resolves flags.strategyExec to the exact
// absolute path external.Launch will actually execute (review
// finding): filepath.Abs(name) alone is not equivalent to what
// os/exec does with a bare command name containing no path
// separator, which searches PATH (exec.LookPath) rather than
// resolving relative to the current directory — using the wrong one
// could hash and record a different file than the one that actually
// ran. A name that does contain a path separator is never looked up
// on PATH by os/exec either way, so it is resolved with plain
// filepath.Abs, matching os/exec's own literal-path handling exactly.
func resolveStrategyExecutable(name string) (string, error) {
	resolved := name
	if !strings.ContainsRune(name, os.PathSeparator) {
		p, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("resolving --strategy-exec: %w", err)
		}
		resolved = p
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolving --strategy-exec: %w", err)
	}
	return abs, nil
}

// buildExternalLaunchConfig assembles the external.LaunchConfig
// --strategy-exec launches with (LaunchConfig.Command set to the
// exact resolved executable path resolveStrategyExecutable returns —
// review finding: an earlier version passed flags.strategyExec
// verbatim, which could resolve to a different file than whatever
// this function's own caller separately hashed for provenance), and
// the resolved absolute --strategy-config path (empty if none was
// given) to record in externalStrategyParams. Pulled out as its own
// pure function, rather than inlined in runBacktest's own strategy-
// selection branch, so its env-inheritance and path-resolution
// behavior can be unit tested directly against a synthetic environ,
// without spawning a real process (review finding: an earlier version
// of this command left Env nil/near-empty instead of starting from
// the operator's own inherited environment — see filterOutEnvKey's
// own doc comment for why environ is filtered before
// strategyConfigPathEnv is appended).
func buildExternalLaunchConfig(flags runFlags, environ []string, logger *slog.Logger) (cfg external.LaunchConfig, resolvedExec, configAbs string, err error) {
	resolvedExec, err = resolveStrategyExecutable(flags.strategyExec)
	if err != nil {
		return external.LaunchConfig{}, "", "", err
	}
	cfg = external.LaunchConfig{
		Command: resolvedExec,
		Args:    flags.strategyArgs,
		Env:     filterOutEnvKey(environ, strategyConfigPathEnv),
		Logger:  logger,
	}
	if flags.strategyConfig == "" {
		return cfg, resolvedExec, "", nil
	}
	abs, err := filepath.Abs(flags.strategyConfig)
	if err != nil {
		return external.LaunchConfig{}, "", "", fmt.Errorf("resolving --strategy-config: %w", err)
	}
	cfg.Env = append(cfg.Env, strategyConfigPathEnv+"="+abs)
	return cfg, resolvedExec, abs, nil
}

// filterOutEnvKey returns env with every entry naming key removed —
// used so a caller-controlled Env value (os.Environ(), here) can never
// end up with two entries for the same key once this command's own
// value is appended, whose effective value at runtime is undefined;
// mirrors external.Launch's own identical filter for its socket
// variable (adapters/strategy/external/process.go).
func filterOutEnvKey(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// processMonitor is the *external.Process surface
// runWithExternalProcessMonitor needs — narrowed to an interface so a
// deterministic fake can exercise the exact simultaneous-readiness
// race below without timing luck (review finding).
type processMonitor interface {
	Done() <-chan struct{}
	Err() error
}

// runWithExternalProcessMonitor calls svc.Run, concurrently watching
// process.Done() (a nil process — every non-external-strategy path —
// disables this and simply calls svc.Run directly). If the external
// process exits before svc.Run itself returns, the run's own context
// is canceled and the process's exit is reported as the failure
// reason: a guest that crashes (or exits cleanly but unexpectedly)
// between callbacks, or during a replay that never calls back into it
// at all, must not let an unaffected Scheduler finish "successfully"
// with no idea the strategy driving it is already gone (review
// finding).
//
// A successful resultCh receive is not, by itself, proof the guest
// was still alive when the run finished: Go's select chooses
// pseudo-randomly among simultaneously ready cases, so a process that
// exits at (or just before) the exact instant svc.Run completes can
// still be the resultCh arm selected, silently accepting success from
// a guest that already crashed (second-round review finding — this is
// exactly the failure class this function exists to close). A nil-err
// result is therefore always followed by one more non-blocking check
// of process.Done() before it is accepted.
func runWithExternalProcessMonitor(ctx context.Context, svc *svcbacktest.Service, req svcbacktest.RunRequest, process processMonitor) (svcbacktest.RunResponse, error) {
	if process == nil {
		return svc.Run(ctx, req)
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	resultCh := make(chan runResult, 1)
	go func() {
		resp, err := svc.Run(runCtx, req)
		resultCh <- runResult{resp, err}
	}()

	return awaitRunWithProcessMonitor(cancelRun, resultCh, process)
}

// runResult is svc.Run's own return pair, carried over resultCh.
type runResult struct {
	resp svcbacktest.RunResponse
	err  error
}

// awaitRunWithProcessMonitor is runWithExternalProcessMonitor's own
// select/race-handling logic, extracted so it can be exercised
// directly against a synthetic resultCh and a fake processMonitor —
// runWithExternalProcessMonitor itself needs a real *svcbacktest.
// Service, too heavy to construct just to prove this race is closed
// (review finding: a deterministic test, not timing luck).
func awaitRunWithProcessMonitor(cancelRun context.CancelFunc, resultCh <-chan runResult, process processMonitor) (svcbacktest.RunResponse, error) {
	select {
	case r := <-resultCh:
		if r.err != nil {
			return r.resp, r.err
		}
		return r.resp, checkProcessStillAlive(process)
	case <-process.Done():
		cancelRun()
		r := <-resultCh
		if procErr := process.Err(); procErr != nil {
			return r.resp, fmt.Errorf("external strategy process exited unexpectedly before the run completed: %w", procErr)
		}
		if r.err == nil {
			return r.resp, fmt.Errorf("external strategy process exited before the run completed")
		}
		return r.resp, r.err
	}
}

// checkProcessStillAlive reports the process's own exit as an error
// if it has already exited by the time this is called (a non-blocking
// check: Done() open means "still running," not "will never exit"),
// nil otherwise. Called only after svc.Run itself already reported
// success — see runWithExternalProcessMonitor's own doc comment for
// why this second check exists.
func checkProcessStillAlive(process processMonitor) error {
	select {
	case <-process.Done():
		if err := process.Err(); err != nil {
			return fmt.Errorf("external strategy process exited unexpectedly before the run completed: %w", err)
		}
		return fmt.Errorf("external strategy process exited before the run completed")
	default:
		return nil
	}
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

	if err := validateStrategySelection(cmd, flags); err != nil {
		return err
	}

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

	storeRoot := cfg.Backtest.DataStoreRoot
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

	manager, err := marketruntime.New(marketruntime.Config{
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
		plan, err := manager.Plan(ctx, marketruntime.BarQuery{Instrument: instrumentID, Interval: interval, Range: span})
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
	// externalProcess is set only on the --strategy-exec path, below —
	// used after svc.Run to distinguish a genuinely successful run from
	// one where the external process exited (crashed or otherwise) at
	// some point during it (review finding: a guest that exits between
	// Start and its first/next callback, or during an empty replay that
	// never calls back into it at all, would otherwise let Scheduler
	// finish "successfully" without ever noticing).
	var externalProcess *external.Process

	if flags.config != "" {
		// There is no strategy registry: strategy.name (--strategy-name)
		// does not select anything, since this path only ever
		// constructs strategy/emacross. Rejecting any other name here
		// (rather than silently running EMA crossover under an
		// unrelated label) keeps the manifest's StrategyName truthful
		// against what the config actually claimed (PR #263 review).
		if cfg.Strategy.Name != emacross.Name {
			return fmt.Errorf("strategy.name %q is not supported: --config only runs %q (there is no strategy registry)",
				cfg.Strategy.Name, emacross.Name)
		}

		// --config describes a single-instrument EMA crossover
		// experiment (buildRunConfig already rejected combining it
		// with more than one --symbol), so instruments.ids has exactly
		// one entry here.
		instID := instruments.ids[0]
		listing := instruments.simListing[instID.String()]

		emaStrategy, err := emacross.New(instID, interval, emacross.Config{
			FastPeriod:  cfg.Strategy.FastPeriod,
			SlowPeriod:  cfg.Strategy.SlowPeriod,
			AllowedSide: cfg.Strategy.AllowedSide,
		})
		if err != nil {
			return err
		}

		src := newNextBarOpenPriceSource()
		if err := src.load(ctx, manager, listing.Symbol(), marketruntime.BarQuery{Instrument: instID, Interval: interval, Range: span}); err != nil {
			return fmt.Errorf("loading canonical prices for %s: %w", instID, err)
		}

		strat = emaStrategy
		strategyParams = emaStrategy.Config()
		prices = src
	} else if flags.strategyExec != "" {
		// The external strategy's own Descriptor (received during its
		// Handshake, below) — not --symbol/--interval — is the actual
		// replay universe (ADR-042's own "Strategy.Describe().
		// Requirements is the universe boundary"); --symbol/--interval
		// above only controls what canonical data this command
		// publishes before launching it, which must still cover
		// whatever the executable will actually request (documented on
		// the --strategy-exec flag itself).
		//
		launchCfg, execAbs, strategyConfigAbs, err := buildExternalLaunchConfig(flags, os.Environ(), clictx.LoggerFromContext(ctx))
		if err != nil {
			return err
		}

		// Both digests are computed from the exact resolved paths
		// buildExternalLaunchConfig itself will hand to external.Launch
		// below, and before Launch ever runs (review finding: an
		// earlier version hashed the executable only after the child
		// had already started and completed Handshake, so a file
		// replaced/removed during startup could make the recorded
		// digest describe different bytes than the process actually
		// executed) — this is the closest practical guarantee against
		// that TOCTOU race without launching from an already-open file
		// descriptor.
		execDigest, err := fileContentDigest(ctx, execAbs)
		if err != nil {
			return err
		}
		var configDigest string
		if strategyConfigAbs != "" {
			configDigest, err = fileContentDigest(ctx, strategyConfigAbs)
			if err != nil {
				return err
			}
		}

		process, err := external.Launch(ctx, launchCfg)
		if err != nil {
			return fmt.Errorf("launching --strategy-exec %s: %w", flags.strategyExec, err)
		}
		// Process.Stop is idempotent-safe to call once at the end of this
		// run regardless of how runBacktest returns from here on
		// (success or any later error); a background context, not ctx,
		// bounds it so a canceled/expiring command context cannot skip
		// straight to SIGKILL instead of Process's own configured
		// graceful-shutdown grace (ADR-063). This always runs; the
		// graceful strategy-level Close below, called explicitly right
		// before svc.Run returns on the success path, is the additional
		// step ADR-063's own Process docs ask a caller to perform first
		// when it wants a normal Strategy Protocol v1 SessionEnd instead
		// of Stop's own SIGTERM-based teardown (review finding).
		defer func() { _ = process.Stop(context.Background()) }()

		src := newNextBarOpenPriceSource()
		for _, instrumentID := range instruments.ids {
			listing := instruments.simListing[instrumentID.String()]
			if err := src.load(ctx, manager, listing.Symbol(), marketruntime.BarQuery{Instrument: instrumentID, Interval: interval, Range: span}); err != nil {
				return fmt.Errorf("loading canonical prices for %s: %w", instrumentID, err)
			}
		}

		strat = process.Strategy()
		descriptor := strat.Describe()

		// strategyConfigAbs, not flags.strategyConfig verbatim: the
		// manifest must record the exact path actually handed to the
		// child (via strategyConfigPathEnv, above), not the CLI's own
		// possibly-relative spelling, or a later replay from a
		// different working directory would not match what this run
		// actually consumed (review finding).
		strategyParams = externalStrategyParams{
			Mode:            "external",
			StrategyName:    descriptor.Name,
			StrategyVersion: descriptor.Version,
			ProtocolVersion: strategyv1.ProtocolVersion,
			Transport:       "unix",
			Exec:            execAbs,
			ExecDigest:      execDigest,
			ConfigDigest:    configDigest,
			Args:            flags.strategyArgs,
			Config:          strategyConfigAbs,
		}
		prices = src
		externalProcess = process
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

	var jrnl journal.Recorder
	var journalWriter *jsonl.Writer
	if flags.journal != "" {
		w, err := jsonl.NewWriter(flags.journal)
		if err != nil {
			return fmt.Errorf("opening --journal: %w", err)
		}
		// jsonl.Writer.Close is idempotent (its own doc comment: a
		// second Close call returns nil rather than re-syncing/
		// re-closing), so this deferred call is safe whether or not
		// the explicit Close below on the success path already ran —
		// it only ever does real work on an early-return path (svc
		// construction or svc.Run failing). The explicit Close below,
		// not this deferred one, is what propagates a Close failure:
		// jsonl.Writer only fsyncs in Close (its own doc comment), so
		// silently discarding its error here would let this command
		// report success while --journal's own advertised durability
		// guarantee silently did not hold (PR #267 review).
		defer func() { _ = w.Close() }()
		journalWriter = w
		jrnl = w
	}

	factory := environmentFactory{prices: prices, journal: jrnl}

	svc, err := svcbacktest.New(manager, simResolver, factory, clictx.LoggerFromContext(ctx))
	if err != nil {
		return err
	}

	// externalProcess is a concrete *external.Process, possibly nil;
	// assigning a nil *external.Process directly to a processMonitor
	// interface variable would produce a non-nil interface wrapping a
	// nil pointer (Go's classic "typed nil" pitfall), defeating
	// runWithExternalProcessMonitor's own "if process == nil" check —
	// this explicit conversion keeps monitor a true nil interface on
	// every non-external-strategy path.
	var monitor processMonitor
	if externalProcess != nil {
		monitor = externalProcess
	}
	resp, err := runWithExternalProcessMonitor(ctx, svc, svcbacktest.RunRequest{
		Strategy:           strat,
		StrategyParameters: strategyParams,
		Span:               span,
		StartingCapital:    startingCash,
		RiskFraction:       riskFraction,
		AdverseDistance:    adverseDistance,
	}, monitor)
	if err != nil {
		return err
	}

	// A successful run gets the external strategy's own graceful
	// Strategy Protocol v1 shutdown (a normal-completion SessionEnd)
	// before the deferred Process.Stop's SIGTERM-based teardown runs —
	// ADR-063's own Process docs ask a caller wanting this to call the
	// strategy's Close before Stop, rather than let every run end by
	// SIGTERM regardless of outcome (review finding). Best-effort: a
	// Close failure here does not fail an otherwise-successful backtest,
	// since Process.Stop still guarantees the child is terminated and
	// its socket cleaned up either way.
	if externalProcess != nil {
		if closer, ok := externalProcess.Strategy().(interface{ Close(context.Context) error }); ok {
			closeCtx, cancel := context.WithTimeout(context.Background(), external.DefaultShutdownGrace)
			if err := closer.Close(closeCtx); err != nil {
				clictx.LoggerFromContext(ctx).Warn("external strategy: graceful close failed, falling back to process termination", "error", err)
			}
			cancel()
		}
	}

	if journalWriter != nil {
		if err := journalWriter.Close(); err != nil {
			return fmt.Errorf("closing --journal: %w", err)
		}
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
func nextBarOpenAfterEntry(ctx context.Context, manager *marketruntime.Manager, instrumentID instrument.ID, interval marketdata.Interval, span marketdata.TimeRange, warmupBars int) (num.Price, error) {
	reader, err := manager.Bars(ctx, marketruntime.BarQuery{Instrument: instrumentID, Interval: interval, Range: span})
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
