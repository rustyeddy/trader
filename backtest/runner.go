package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/strategy"
)

// ErrInvalidRunnerParams marks a RunnerParams missing a required
// field.
var ErrInvalidRunnerParams = errors.New("backtest: invalid runner params")

// ErrRunnerAlreadyUsed marks a second call to (*Runner).Run. See
// Runner's own doc comment for why a Runner is single-use.
var ErrRunnerAlreadyUsed = errors.New("backtest: runner already used")

// runnerIDSource is the id.Source every ID Runner's strategy
// IntentFactory attributes its generated IntentID/EventID/
// CorrelationID values to — a fixed provenance label, not a per-run
// policy choice.
const runnerIDSource = id.Source("backtest")

// RunnerParams are a Runner's complete, run-scoped execution
// environment (issue #216, M5-08 review). Every field describes
// either a dependency needed to drive one backtest, or the exact
// metadata that describes it — never a separately supplied claim
// about a component the caller could configure differently.
//
// In particular, RiskRules/FillModel/SlippageModel/CommissionModel
// are bundled here alongside Pipeline/Account, not accepted as
// independent input: they must describe the actual configured
// Pipeline/broker, and keeping them in the same construction path a
// composition root already controls (rather than a separate later
// input) removes the channel through which a Manifest could otherwise
// describe something other than what actually ran. For the same
// reason, RunnerParams carries no separate "starting capital" field —
// Run reads it directly from Account's own initial snapshot.
//
// Every field here is expected to be freshly constructed for this one
// run: Clock, IDs, Account, and the broker behind Pipeline are all
// stateful participants that accumulate run-specific state (simulated
// time, consumed identifiers, positions, fills). Reusing any of them
// across two runs would make the second run's own determinism and
// reproducibility depend on exactly what state the first run left
// behind — which is why Runner itself is single-use (see Runner's own
// doc comment) rather than a reusable object callers invoke Run on
// repeatedly with fresh dependencies each time.
type RunnerParams struct {
	// Manager is the canonical historical data source Replay is built
	// from.
	Manager *marketdata.Manager
	// Resolver resolves an instrument.ID to the broker-side Listing
	// the default InputBuilder submits orders against.
	Resolver instrument.Resolver

	// Clock is the simulation clock driving this one run. It must be
	// freshly constructed for this run.
	Clock *clock.Simulated
	// IDs generates every identifier this run needs (RunID, and every
	// IntentID/EventID/CorrelationID the strategy's own IntentFactory
	// produces). It must be freshly constructed for this run.
	IDs *id.Generator

	// Pipeline is the configured M4 execution/risk path this run
	// submits every intent through.
	Pipeline *pipeline.Pipeline
	// Account is the broker-scoped handle Scheduler drives. It must
	// be freshly opened for this run, at whatever starting balance the
	// resulting Manifest should describe — Run reads that balance
	// directly from Account's own initial snapshot.
	Account broker.Account
	// RiskFraction and AdverseDistance are this run's fixed position-
	// sizing policy, passed to the default InputBuilder and recorded
	// in the resulting Manifest.
	RiskFraction    num.Rate
	AdverseDistance num.Price
	// RiskRules, FillModel, SlippageModel, and CommissionModel
	// describe the risk rules and models Pipeline/Account were
	// actually configured with. See RunnerParams's own doc comment
	// for why these travel with Pipeline/Account rather than arriving
	// as separate, independently suppliable input.
	RiskRules       []ComponentInfo
	FillModel       ComponentInfo
	SlippageModel   ComponentInfo
	CommissionModel ComponentInfo

	// Strategy is the strategy this run drives. Describe() supplies
	// its own DataRequirements (Replay's universe) and warm-up needs.
	Strategy strategy.Strategy
	// StrategyParameters is canonically marshaled into the resulting
	// Manifest. Pass nil for a strategy with no parameters.
	StrategyParameters any

	// Span is the half-open time range this run replays.
	Span marketdata.TimeRange
	// TraderVersion is an optional caller-supplied build/version
	// string, recorded in the resulting Manifest.
	TraderVersion string

	// Logger receives Runner's own structured records. A nil Logger
	// is replaced with logging.Discard(), matching every other Trader
	// composition-root convention.
	Logger *slog.Logger

	// Journal receives a durable, replayable record of this run (issue
	// #218, M5-10; ADR-036): every strategy intent, proposal, risk
	// decision, request, order, fill, account event, and derived trade,
	// in true execution order. A nil Journal is replaced with
	// journal.Discard(), matching Logger's own convention — Runner
	// never requires a caller to configure a real journal.
	//
	// Runner never closes an externally supplied Journal: it is an
	// injected dependency like Account or Pipeline, and the composition
	// root that constructed it owns its lifecycle, exactly as for every
	// other RunnerParams dependency Runner does not itself construct.
	Journal journal.Recorder
}

func (p RunnerParams) validate() error {
	if p.Manager == nil {
		return fmt.Errorf("%w: manager must be set", ErrInvalidRunnerParams)
	}
	if p.Resolver == nil {
		return fmt.Errorf("%w: resolver must be set", ErrInvalidRunnerParams)
	}
	if p.Clock == nil {
		return fmt.Errorf("%w: clock must be set", ErrInvalidRunnerParams)
	}
	if p.IDs == nil {
		return fmt.Errorf("%w: ids must be set", ErrInvalidRunnerParams)
	}
	if p.Pipeline == nil {
		return fmt.Errorf("%w: pipeline must be set", ErrInvalidRunnerParams)
	}
	if p.Account == nil {
		return fmt.Errorf("%w: account must be set", ErrInvalidRunnerParams)
	}
	if p.Strategy == nil {
		return fmt.Errorf("%w: strategy must be set", ErrInvalidRunnerParams)
	}
	return nil
}

// Runner provides the high-level orchestration for exactly one
// complete backtest run (issue #216, M5-08). Construct it with
// NewRunner and call Run exactly once — a second call returns
// ErrRunnerAlreadyUsed without doing any work. This is deliberate, not
// a limitation to work around by retrying on the same Runner: the
// stateful participants RunnerParams bundles (Clock, IDs, Account, the
// broker behind Pipeline) accumulate run-specific state as Run
// executes, so a second run through the same Runner could not be an
// independent deterministic backtest. A caller that wants to run many
// backtests constructs a fresh RunnerParams (with fresh Clock/IDs/
// Account/Pipeline) and a fresh Runner for each one — that composition
// responsibility belongs to a factory one tier up (service/backtest or
// cmd/trader/backtest), not to Runner itself.
//
// Runner delegates every domain behavior it coordinates: Replay owns
// historical merge ordering, Scheduler owns event timing/warm-up/
// fill-eligibility, Pipeline owns execution/risk, and NewManifest owns
// reproducibility-record validation and canonical ordering. Runner's
// own job is purely sequencing: build Replay, build the Manifest from
// Replay's own dataset provenance plus RunnerParams, start Strategy,
// build and run Scheduler, and report the outcome.
type Runner struct {
	params RunnerParams
	used   atomic.Bool
}

// NewRunner validates params and returns a Runner. It performs no I/O;
// Run does all the work.
func NewRunner(params RunnerParams) (*Runner, error) {
	if err := params.validate(); err != nil {
		return nil, err
	}
	if params.Logger == nil {
		params.Logger = logging.Discard()
	}
	if params.Journal == nil {
		params.Journal = journal.Discard()
	}
	return &Runner{params: params}, nil
}

// Run drives one complete, deterministic backtest and returns its
// Result. It may be called exactly once per Runner; a second call
// returns ErrRunnerAlreadyUsed. ctx is checked before any work begins,
// so an already-canceled ctx has no side effects (no Replay is opened,
// no Account is snapshotted).
//
// A returned error is always a normal Go error a caller can classify
// with errors.Is/errors.As against the sentinel of whichever component
// failed (marketdata.ErrDataUnavailable, ErrInvalidManifest,
// ErrInvalidSchedulerDeps, and so on) — Run never panics, never
// silently discards a failure, and introduces no parallel error
// taxonomy of its own beyond ErrInvalidRunnerParams/
// ErrRunnerAlreadyUsed. A Result is only ever returned alongside a nil
// error: a canceled or failed run returns the zero Result, so there is
// never a partial Result for a caller to mistake for a completed one.
func (r *Runner) Run(ctx context.Context) (result Result, err error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !r.used.CompareAndSwap(false, true) {
		return Result{}, ErrRunnerAlreadyUsed
	}

	p := r.params
	descriptor := p.Strategy.Describe()

	startSnapshot, err := p.Account.Snapshot(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("backtest: runner: snapshotting starting account state: %w", err)
	}

	replay, err := NewReplay(ctx, p.Manager, descriptor.Requirements, p.Span)
	if err != nil {
		return Result{}, fmt.Errorf("backtest: runner: opening replay: %w", err)
	}
	defer func() {
		if cerr := replay.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("backtest: runner: closing replay: %w", cerr)
			result = Result{}
		}
	}()

	runID, err := id.GenerateRunID(p.IDs)
	if err != nil {
		return Result{}, fmt.Errorf("backtest: runner: generating run id: %w", err)
	}

	manifest, err := NewManifest(ManifestParams{
		RunID:              runID,
		StrategyName:       descriptor.Name,
		StrategyVersion:    descriptor.Version,
		StrategyParameters: p.StrategyParameters,
		Universe:           descriptor.Requirements,
		Span:               p.Span,
		StartingCapital:    startSnapshot.Equity(),
		RiskFraction:       p.RiskFraction,
		AdverseDistance:    p.AdverseDistance,
		RiskRules:          p.RiskRules,
		FillModel:          p.FillModel,
		SlippageModel:      p.SlippageModel,
		CommissionModel:    p.CommissionModel,
		Dataset:            replay.Manifests(),
		TraderVersion:      p.TraderVersion,
	})
	if err != nil {
		return Result{}, fmt.Errorf("backtest: runner: building manifest: %w", err)
	}

	jrnl := newCountingRecorder(p.Journal)
	header, err := json.Marshal(manifest)
	if err != nil {
		return Result{}, fmt.Errorf("backtest: runner: encoding run-started header: %w", err)
	}
	if err := jrnl.Record(ctx, journal.Record{
		RunID:      runID,
		Metadata:   id.Metadata{Timestamp: p.Clock.Now()},
		Kind:       journal.KindRunStarted,
		RunStarted: &journal.RunStarted{RunID: runID, Header: header},
	}); err != nil {
		return Result{}, fmt.Errorf("backtest: runner: journaling run start: %w", err)
	}

	env := strategy.Environment{
		Clock:   p.Clock,
		Intents: strategy.NewIntentFactory(p.Clock, p.IDs, runnerIDSource),
		Logger:  p.Logger,
	}
	if err := p.Strategy.Start(ctx, env); err != nil {
		return Result{}, fmt.Errorf("backtest: runner: starting strategy: %w", err)
	}

	builder, err := NewResolverInputBuilder(p.Resolver, p.RiskFraction, p.AdverseDistance)
	if err != nil {
		return Result{}, fmt.Errorf("backtest: runner: constructing input builder: %w", err)
	}
	sched, err := NewScheduler(SchedulerDeps{
		Replay:   replay,
		Strategy: p.Strategy,
		Clock:    p.Clock,
		Pipeline: p.Pipeline,
		Account:  p.Account,
		Builder:  builder,
		Journal:  jrnl,
		RunID:    runID,
	})
	if err != nil {
		return Result{}, fmt.Errorf("backtest: runner: constructing scheduler: %w", err)
	}
	if err := sched.Run(ctx); err != nil {
		return Result{}, fmt.Errorf("backtest: runner: running scheduler: %w", err)
	}

	finalSnapshot, err := p.Account.Snapshot(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("backtest: runner: snapshotting final account state: %w", err)
	}

	trades, err := DeriveTrades(sched.Fills())
	if err != nil {
		return Result{}, fmt.Errorf("backtest: runner: deriving trades: %w", err)
	}
	if err := journalTrades(ctx, jrnl, runID, trades); err != nil {
		return Result{}, fmt.Errorf("backtest: runner: journaling trades: %w", err)
	}

	if err := jrnl.Record(ctx, journal.Record{
		RunID:        runID,
		Metadata:     id.Metadata{Timestamp: p.Clock.Now()},
		Kind:         journal.KindRunCompleted,
		RunCompleted: &journal.RunCompleted{RunID: runID, EntryCount: jrnl.count},
	}); err != nil {
		return Result{}, fmt.Errorf("backtest: runner: journaling run completion: %w", err)
	}

	return Result{Manifest: manifest, Account: finalSnapshot, Trades: trades.Closed, OpenTrades: trades.Open}, nil
}

// journalTrades records every derived trade — closed and still open —
// as its own journal.KindTrade entry (issue #218, M5-10): "derived
// trades where appropriate" per #218's own scope, using each Trade's
// own OpenedAt/ClosedAt as the record's timestamp since a derived
// trade carries no Metadata of its own to reuse.
func journalTrades(ctx context.Context, j journal.Recorder, runID id.RunID, trades TradeSet) error {
	record := func(t order.Trade) error {
		ts := t.ClosedAt
		if ts.IsZero() {
			ts = t.OpenedAt
		}
		return j.Record(ctx, journal.Record{
			RunID:    runID,
			Metadata: id.Metadata{Timestamp: ts},
			Kind:     journal.KindTrade,
			Trade:    &t,
		})
	}
	for _, t := range trades.Closed {
		if err := record(t); err != nil {
			return err
		}
	}
	for _, t := range trades.Open {
		if err := record(t); err != nil {
			return err
		}
	}
	return nil
}

// Result is what a successful Run produces: the immutable Manifest
// describing exactly what ran, the account's final state once the run
// completed, and the trades derived from that run's own fills (issue
// #217, M5-09). Trades holds only fully closed round trips; OpenTrades
// holds any position still open when the run ended, kept separate so a
// caller iterating Trades never has to remember that some entries are
// not actually completed — see DeriveTrades' own doc comment for the
// full grouping and cost-attribution rules.
type Result struct {
	Manifest   Manifest
	Account    account.Snapshot
	Trades     []order.Trade
	OpenTrades []order.Trade
}
