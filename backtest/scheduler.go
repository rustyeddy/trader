package backtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/strategy"
)

// ErrInvalidSchedulerDeps marks a SchedulerDeps missing a required
// field, a Strategy declaring a duplicate DataRequirement, or (at Run
// time) Replay producing an event for an (instrument, interval)
// Strategy never declared.
var ErrInvalidSchedulerDeps = errors.New("backtest: invalid scheduler dependencies")

// ErrEventReaderNotFinite reports that an Account's EventReader does
// not implement broker.FiniteEventReader — Scheduler requires it,
// since journaling this run's broker events deterministically needs a
// fixed, drainable boundary rather than racing the reader's ordinary
// live-stream blocking behavior (see drainAndJournal's own doc
// comment).
var ErrEventReaderNotFinite = errors.New("backtest: event reader does not support finite draining")

// InputBuilder turns one order.Intent a Strategy emitted — together
// with the strategy.BarEvent it is now eligible against and the
// account snapshot Scheduler is about to submit with — into the
// canonical pipeline.Input the M4 pipeline needs.
//
// event is the bar that made intent eligible to submit, per
// Scheduler's own next-bar-open fill-eligibility rule (issue #214
// review) — not necessarily the bar whose OnBar call emitted intent.
// A market order decided from a closed bar T is queued, not
// submitted, until that same DataRequirement's own next bar T+1
// arrives; Build then receives T+1 as event, since T+1 (specifically
// its Open) is the honest reference/fill-eligibility observation for
// that order, not T's own already-priced-in Close. See Scheduler's own
// doc comment for the full rule and why.
//
// This is a seam Scheduler injects rather than owns (issue #213
// review). Listing resolution and a reference price are mechanical
// runtime concerns, but RiskFraction and AdverseDistance are
// execution/risk *policy* — they belong to the concrete M5 run
// composition (and #215's run manifest) to configure, not to
// Scheduler's own public API. Scheduler asks Build to turn a valid
// emitted Intent plus its eligibility observation into the canonical
// M4 input; the resulting pipeline.Input still goes through the
// unchanged pipeline.Pipeline — Build is not a second execution/risk
// path.
//
// Build must be deterministic: given the same intent, event, and
// account snapshot, it must always return the same pipeline.Input.
type InputBuilder interface {
	Build(ctx context.Context, intent order.Intent, event strategy.BarEvent, snapshot account.Snapshot) (pipeline.Input, error)
}

// MarketObserver is a required backtest-runtime capability that lets
// Scheduler revalue open positions' marks from each bar's own close
// price between fills, keeping the equity curve genuinely mark-to-
// market (issue #219 follow-up) — #213/M5-05's own design note already
// anticipated this as "a simulation-facing capability owned by
// backtest... not by widening broker.Broker."
//
// ObserveMark deliberately does only this: it never evaluates resting
// Limit/Stop order triggers. That remains ADR-026's own separate,
// still-deferred concern (a broker-side Advance-shaped operation, not
// this one) — conflating the two here would silently change order-
// fill behavior as a side effect of fixing an equity-curve bug.
//
// Every RunnerParams.Account/SchedulerDeps.MarketObserver is required,
// never optional: silently degrading to a stale-mark equity curve when
// the capability happens to be absent would recreate the exact
// correctness bug this interface exists to prevent, so NewRunner and
// NewScheduler both reject a missing one rather than treating it as
// optional. An ObserveMark failure aborts Run, exactly like any other
// Scheduler-stage failure.
//
// The signature deliberately uses only already-shared primitive types
// (instrument.ID, num.Price, time.Time) rather than a backtest-defined
// struct: this lets a broker adapter's own account handle (see
// adapters/broker/sim) satisfy this interface structurally, without
// importing backtest — an adapter must never depend on an
// orchestration package.
type MarketObserver interface {
	ObserveMark(ctx context.Context, instrumentID instrument.ID, close num.Price, at time.Time) error
}

// SchedulerDeps are a Scheduler's injected dependencies. Every field
// is required.
type SchedulerDeps struct {
	// Replay is the deterministic, already-merged bar stream (#212)
	// Scheduler drains via Next.
	Replay *Replay
	// Strategy is the strategy runtime Scheduler drives via OnBar.
	// Scheduler assumes Strategy.Start has already been called by
	// whatever composed these deps (constructing Strategy's own
	// Environment — Clock, IntentFactory, Logger — is a run-composition
	// concern, not Scheduler's); Scheduler itself never calls Start.
	// NewScheduler calls Strategy.Describe() once, to learn the
	// declared DataRequirements warm-up and History gating need.
	Strategy strategy.Strategy
	// Clock is the simulation clock Scheduler advances via AdvanceTo
	// as replayed bars are processed. It is normally the same
	// *clock.Simulated a Strategy's own Environment.Clock was
	// constructed with, so both observe one consistent simulated
	// timeline.
	Clock *clock.Simulated
	// Pipeline is the unchanged M4 orchestration path (risk.Sizer,
	// execution.Planner, risk.Engine, broker.Broker) every emitted
	// intent is submitted through. Scheduler adds no parallel sizing
	// or risk logic of its own.
	Pipeline *pipeline.Pipeline
	// Account is the broker-scoped handle Scheduler snapshots for
	// View.Account() and for building each submitted pipeline.Input.
	Account broker.Account
	// Builder turns an emitted Intent into a pipeline.Input. See
	// InputBuilder's own doc comment.
	Builder InputBuilder
	// Journal receives every intent/proposal/decision/request Scheduler
	// submits, and every order/fill/account/status event the broker
	// produces in response, interleaved in true execution order (issue
	// #218, M5-10; ADR-036). Runner supplies journal.Discard() when the
	// caller configured no real journal — Journal is never nil here.
	Journal journal.Recorder
	// RunID identifies the run every Record Scheduler journals belongs
	// to.
	RunID id.RunID
	// MarketObserver revalues open positions' marks from each batch's
	// own bars, keeping the equity curve mark-to-market between fills.
	// See MarketObserver's own doc comment for why this is required,
	// not optional.
	MarketObserver MarketObserver
}

func (d SchedulerDeps) validate() error {
	if d.Replay == nil {
		return fmt.Errorf("%w: replay must be set", ErrInvalidSchedulerDeps)
	}
	if d.Strategy == nil {
		return fmt.Errorf("%w: strategy must be set", ErrInvalidSchedulerDeps)
	}
	if d.Clock == nil {
		return fmt.Errorf("%w: clock must be set", ErrInvalidSchedulerDeps)
	}
	if d.Pipeline == nil {
		return fmt.Errorf("%w: pipeline must be set", ErrInvalidSchedulerDeps)
	}
	if d.Account == nil {
		return fmt.Errorf("%w: account must be set", ErrInvalidSchedulerDeps)
	}
	if d.Builder == nil {
		return fmt.Errorf("%w: builder must be set", ErrInvalidSchedulerDeps)
	}
	if d.Journal == nil {
		return fmt.Errorf("%w: journal must be set", ErrInvalidSchedulerDeps)
	}
	if d.RunID.IsZero() {
		return fmt.Errorf("%w: run id must be set", ErrInvalidSchedulerDeps)
	}
	if d.MarketObserver == nil {
		return fmt.Errorf("%w: market observer must be set", ErrInvalidSchedulerDeps)
	}
	return nil
}

// Scheduler is the deterministic event scheduler coordinating
// historical replay, strategy evaluation, and M4 execution/risk
// submission (issue #213, M5-05; issue #214, M5-06; ADR-035).
//
// # Same-timestamp batching
//
// Replay's own (timestamp, instrument ID, interval) order is a
// deterministic *serialization* of observations, not a claim about
// market-time precedence: two bars sharing one timestamp are logically
// simultaneous. Scheduler therefore groups every consecutive event
// Replay yields for one identical bar timestamp T into a single
// simulation step — see runBatch's own doc comment for the exact
// phase order (fill eligibility, then strategy evaluation, then
// history growth).
//
// Every OnBar call within one T batch sees the same frozen View: all
// of T's bars are genuinely visible to every call in the batch — not
// only the one bar that triggered it — via View's optional BatchBars
// capability (Bars() []strategy.BarEvent). What no call's View
// reflects is any other T-batch call's account/execution effects, nor
// any bar at or after T (see History below). This is what keeps
// Replay's incidental instrument-ID ordering from silently becoming
// trading semantics: EURUSD sorting before GBPUSD at the same T must
// never change GBPUSD's own strategy decision, while GBPUSD's strategy
// can still see EURUSD's T bar if it needs to.
//
// BatchBars is intentionally not part of the base strategy.View
// contract — it is a narrow, Scheduler-owned seam (issue #213). One
// exception to View's own freeze: if Strategy itself mutates its own
// internal state during one OnBar call, a later call within the same
// batch can observe that strategy-local state change — Scheduler does
// not, and cannot, isolate a Strategy value's own fields. What
// Scheduler guarantees is narrower and specific: account/execution
// state does not flow forward within a T batch.
//
// # Historical visibility (History)
//
// strategy.History is the optional View capability (issue #214)
// exposing bars strictly before the current batch's own timestamp, for
// each of Strategy's own declared DataRequirements. Scheduler backs it
// with one append-only bar buffer per DataRequirement, growing by
// exactly one bar per requirement per batch that contains an event for
// it. Crucially, a batch's own bars are appended to these buffers only
// after every OnBar call in that batch has completed (runBatch phase
// 3) — so History can never expose the very batch currently being
// evaluated, only strictly earlier ones. BatchBars (== T) and History
// (< T) are deliberately disjoint: nothing > T is ever reachable
// through either.
//
// Because the buffers are append-only and every previously-written
// bar is never mutated in place, a View may safely read from them
// after Scheduler has appended more — the risk is not stale data, it
// is over-exposure. frozenView therefore captures, at construction
// time, each requirement's own buffer length as of just before this
// batch (its "cutoff"), and HistoryBars only ever reads
// buffer[:cutoff], returning a defensive copy. A View retained across
// batches and queried later is therefore permanently frozen at the
// cutoff it was built with — exactly the "immutable with respect to
// market-time visibility" invariant strategy.History's own doc comment
// requires, even though the buffer keeps growing underneath it.
//
// # Warm-up
//
// Descriptor.Requirements' own WarmupBars documents that warm-up bars
// are "replayed but not decided on." Scheduler implements this
// literally: OnBar is called for every bar from the very start of
// each requirement's own data, so a strategy's own indicator state
// accumulates normally throughout warm-up — but any intents OnBar
// returns are silently discarded (never queued, never submitted) until
// every one of Strategy's own declared DataRequirements has itself
// observed strictly more than WarmupBars closed bars — the
// (WarmupBars+1)th bar is the first one whose intents are honored.
// This is run-wide, not per-triggering-requirement (issue #214
// review): a strategy's own
// declared requirement set is what it says it needs to make a sound
// decision, so one requirement warming up before another does not
// entitle its own bar's intents through early — every declared
// requirement must be ready before any intent is honored. Warm-up
// readiness is monotonic and, once reached, never revisited.
//
// # Fill eligibility: next-bar-open, not same-bar-close
//
// A strategy decides at bar T's own close (T is already a complete
// OHLC record by the time OnBar sees it — Scheduler never exposes an
// open-only partial bar). Submitting a market order that fills at T's
// own close price would give the strategy an execution opportunity
// that does not causally exist: you cannot submit and fill inside the
// zero-duration instant a bar closes. Scheduler therefore treats every
// intent OnBar returns (once warm-up has cleared) as eligible only
// once its own DataRequirement's *next* bar arrives — not the next
// Scheduler batch generally, since different requirements advance at
// different rates, but specifically the next bar for that exact
// (instrument, interval).
//
// Mechanically: intents are queued per DataRequirement rather than
// submitted immediately. At the start of processing a new batch,
// before any OnBar call in it, Scheduler flushes every requirement
// whose next bar has just arrived — submitting each of that
// requirement's queued intents through InputBuilder/Pipeline using the
// newly arrived bar (specifically intended to represent its Open) as
// the eligibility event. Only after flushing does Scheduler evaluate
// the new batch's own OnBar calls.
//
// One consequence: intents emitted from the very last bar of a
// requirement's replayed data have no next bar to become eligible
// against, and are never submitted — a documented boundary of
// next-bar-open semantics, not a bug. A caller that needs to know
// about unrealized end-of-run intents is a journaling concern (a later
// issue), not Scheduler's.
//
// Building the *price* InputBuilder derives from the eligibility event
// (the wiring connecting a bar's Open to the broker's own
// FillPriceSource) is a run-composition concern, not Scheduler's own —
// see InputBuilder's doc comment.
//
// # Market-order-only execution
//
// Scheduler drives Pipeline.Submit only. It never calls a simulator-
// specific market-observation advancement operation (ADR-026) — that
// capability is deliberately not part of the public broker.Broker
// port (a real adapter has no simulation to drive), and Scheduler
// depends only on that port. This means a resting Limit or Stop order
// submitted during a Scheduler-driven run fills only if the broker
// fills it synchronously at submission time; it is never later
// triggered against a subsequent bar's OHLC. A strategy that relies on
// resting order triggering will appear to work at submission and then
// silently never fill. Closing this gap is deferred to a follow-up M5
// issue introducing a simulation-facing capability owned by backtest
// or another simulation-facing port — not by widening broker.Broker.
//
// # Cancellation
//
// ctx is checked at the start of every batch, before each flush/OnBar/
// submit step. A cancellation observed mid-batch stops before the next
// step; whatever was already submitted (including any flush already
// performed) remains committed — a canceled Run is a defined partial
// run, not a rollback.
type Scheduler struct {
	deps         SchedulerDeps
	bufferedNext *strategy.BarEvent

	// warmupRequired and barsSeen are keyed by every DataRequirement
	// Strategy.Describe() declared, captured once in NewScheduler.
	// warmupRequired also doubles as the "declared" set History
	// consults to answer HistoryBars' ok return.
	warmupRequired map[requirementKey]int
	barsSeen       map[requirementKey]int
	warmedUp       bool

	// history holds one append-only bar buffer per declared
	// requirement, grown only after a batch's OnBar calls complete.
	history map[requirementKey][]marketdata.Bar

	// queued holds intents awaiting their own requirement's next bar,
	// keyed the same way.
	queued map[requirementKey][]order.Intent

	// lastBrokerSeq is the highest broker.Event.Sequence already
	// journaled/collected, across the account's whole event stream —
	// see drainAndJournal's own doc comment.
	lastBrokerSeq uint64
	// fills accumulates every order.Fill observed via drainAndJournal,
	// in delivery order, for Fills' own caller (Runner) to hand to
	// DeriveTrades once Run completes.
	fills []order.Fill

	// equityCurve is one authoritative, mark-to-market EquityPoint per
	// batch — see runBatch's own Phase 3 for exactly when it is
	// appended (issue #219, M5-11).
	equityCurve []EquityPoint
}

// NewScheduler returns a Scheduler over deps. Every field of deps must
// be set. NewScheduler calls deps.Strategy.Describe() once to learn
// the DataRequirements warm-up and History gating are evaluated
// against.
func NewScheduler(deps SchedulerDeps) (*Scheduler, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}

	requirements := deps.Strategy.Describe().Requirements
	warmupRequired := make(map[requirementKey]int, len(requirements))
	for _, req := range requirements {
		key := requirementKey{instrument: req.Instrument, interval: req.Interval}
		if _, ok := warmupRequired[key]; ok {
			return nil, fmt.Errorf("%w: strategy declared duplicate requirement %s %s", ErrInvalidSchedulerDeps, req.Instrument, req.Interval)
		}
		warmupRequired[key] = req.WarmupBars
	}

	return &Scheduler{
		deps:           deps,
		warmupRequired: warmupRequired,
		barsSeen:       make(map[requirementKey]int, len(requirements)),
		history:        make(map[requirementKey][]marketdata.Bar, len(requirements)),
		queued:         make(map[requirementKey][]order.Intent),
	}, nil
}

// Run drains Replay to completion, driving Strategy and Pipeline as
// described in Scheduler's own doc comment. It returns nil once Replay
// is exhausted, or the first error encountered — a canceled ctx, a
// Replay/Strategy/Builder/Pipeline failure, or a clock error. A risk
// rejection (errors.Is(err, pipeline.ErrRejected)) is not such an
// error: it is an expected pipeline outcome, so Run continues with the
// next intent rather than aborting.
func (s *Scheduler) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		batch, err := s.nextBatch(ctx)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}

		if err := s.runBatch(ctx, batch); err != nil {
			return err
		}
	}
}

// nextBatch returns every consecutive strategy.BarEvent Replay yields
// sharing one identical Bar.Time, buffering the first event of the
// next batch (if any) in s.bufferedNext for the following call. It
// returns a nil, nil-error batch once Replay is exhausted with nothing
// buffered.
func (s *Scheduler) nextBatch(ctx context.Context) ([]strategy.BarEvent, error) {
	first := s.bufferedNext
	s.bufferedNext = nil
	if first == nil {
		ev, err := s.deps.Replay.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, nil
			}
			return nil, fmt.Errorf("backtest: scheduler: replay: %w", err)
		}
		first = &ev
	}

	batch := []strategy.BarEvent{*first}
	for {
		ev, err := s.deps.Replay.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("backtest: scheduler: replay: %w", err)
		}
		if !ev.Bar.Time.Equal(first.Bar.Time) {
			s.bufferedNext = &ev
			break
		}
		batch = append(batch, ev)
	}
	return batch, nil
}

// runBatch executes one simulation step for every event sharing one
// timestamp T, in five phases:
//
//  0. Validate: every event's own (instrument, interval) must be one
//     of Strategy's own declared DataRequirements — see
//     ErrInvalidSchedulerDeps's own doc comment for why.
//  1. Advance: clock.AdvanceTo(T) once. This happens before anything
//     else in the batch, including the flush below, so every clock
//     read during T's own execution (InputBuilder, Pipeline, the
//     broker/account) observes T, not the previous batch's time —
//     T-open execution and T-close strategy decisions must agree on
//     what time it now is.
//  2. Flush: for every event in the batch whose own requirement has
//     intents queued from an earlier bar, submit them now — this new
//     bar is that requirement's own next bar, satisfying next-bar-open
//     eligibility. Flushing happens before any OnBar call in this
//     batch, since a fill becoming eligible at T's open logically
//     precedes T's own strategy decisions.
//  3. Evaluate: capture one frozen account snapshot (post-flush, so
//     it reflects whatever this batch's own flush just committed) and
//     one frozen History cutoff per requirement, then call OnBar once
//     per event in the batch against that one frozen View, collecting
//     every requirement's own bars-seen increment and any emitted
//     intents. Warm-up readiness is decided exactly once for the
//     whole batch, after every event's own bars-seen increment has
//     already been applied — never per-event mid-batch, which would
//     make warm-up depend on Replay's own canonical ordering within
//     the batch. Once every declared requirement has cleared warm-up,
//     every collected intent from this batch is queued under its own
//     requirement key; before that, all of them are discarded.
//  4. Grow history: append this batch's own bars into the per-
//     requirement history buffers, now that every OnBar call in this
//     batch has completed — never before, so History can never expose
//     the batch currently being evaluated.
func (s *Scheduler) runBatch(ctx context.Context, batch []strategy.BarEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Phase 0: every event's own (instrument, interval) must be one of
	// Strategy's own declared DataRequirements (issue #214 review). An
	// undeclared stream would be invisible to History/warm-up
	// bookkeeping (which key strictly off the declared set) while
	// still reaching OnBar and being able to emit tradable intents —
	// exactly the gap warm-up's "run-wide across declared
	// requirements" guarantee exists to close. Replay producing more
	// data than Strategy declared is a configuration mismatch between
	// SchedulerDeps.Replay and SchedulerDeps.Strategy, not a runtime
	// condition Scheduler should absorb silently.
	for _, ev := range batch {
		key := requirementKey{instrument: ev.Instrument, interval: ev.Interval}
		if _, ok := s.warmupRequired[key]; !ok {
			return fmt.Errorf("%w: replay produced an event for %s %s, which Strategy.Describe().Requirements never declared",
				ErrInvalidSchedulerDeps, ev.Instrument, ev.Interval)
		}
	}

	// Phase 1: advance the clock to T before anything else observes it.
	t := batch[0].Bar.Time
	if err := s.deps.Clock.AdvanceTo(t); err != nil {
		return fmt.Errorf("backtest: scheduler: advancing clock to %s: %w", t, err)
	}

	// Phase 2: flush intents that are eligible now.
	for _, ev := range batch {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := requirementKey{instrument: ev.Instrument, interval: ev.Interval}
		intents := s.queued[key]
		if len(intents) == 0 {
			continue
		}
		delete(s.queued, key)
		for _, in := range intents {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.submit(ctx, in, ev); err != nil {
				return err
			}
		}
	}

	// Phase 3: revalue marks from this batch's own bars, then evaluate
	// against one frozen snapshot/history. Revaluation happens before
	// the snapshot so the snapshot's own Equity() reflects this batch's
	// price action, not last batch's stale marks — see MarketObserver's
	// own doc comment for why this is a required, mark-only step,
	// deliberately not evaluating resting-order triggers.
	for _, ev := range batch {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.deps.MarketObserver.ObserveMark(ctx, ev.Instrument, ev.Bar.Close, t); err != nil {
			return fmt.Errorf("backtest: scheduler: observing market for %s at %s: %w", ev.Instrument, t, err)
		}
	}

	frozen, err := s.deps.Account.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("backtest: scheduler: snapshotting account before %s: %w", t, err)
	}
	// Record one authoritative, mark-to-market equity observation per
	// batch (issue #219, M5-11): frozen is already the account's own
	// post-flush, post-revaluation snapshot for this exact batch
	// timestamp — the honest "equity as of right now" this batch's
	// strategy decisions are also based on — so this is retention of an
	// existing observation, not a second snapshot call.
	s.equityCurve = append(s.equityCurve, EquityPoint{Timestamp: t, Equity: frozen.Equity()})

	cutoffs := make(map[requirementKey]int, len(s.warmupRequired))
	for key := range s.warmupRequired {
		cutoffs[key] = len(s.history[key])
	}
	view := frozenView{
		snapshot: frozen,
		batch:    batch,
		history: historyView{
			buffers:  s.history,
			cutoffs:  cutoffs,
			declared: s.warmupRequired,
		},
	}

	type collected struct {
		key     requirementKey
		intents []order.Intent
	}
	var emitted []collected
	for _, ev := range batch {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := requirementKey{instrument: ev.Instrument, interval: ev.Interval}
		s.barsSeen[key]++

		intents, err := s.deps.Strategy.OnBar(ctx, ev, view)
		if err != nil {
			return fmt.Errorf("backtest: scheduler: OnBar for %s %s at %s: %w", ev.Instrument, ev.Interval, t, err)
		}
		if len(intents) > 0 {
			emitted = append(emitted, collected{key: key, intents: intents})
		}
	}

	if s.allWarm() {
		for _, c := range emitted {
			s.queued[c.key] = append(s.queued[c.key], c.intents...)
		}
	} // else: warm-up — this whole batch was replayed, not decided on.

	// Phase 4: grow history only now that this batch is fully evaluated.
	for _, ev := range batch {
		key := requirementKey{instrument: ev.Instrument, interval: ev.Interval}
		s.history[key] = append(s.history[key], ev.Bar)
	}

	return nil
}

// allWarm reports whether every one of Strategy's own declared
// DataRequirements has observed strictly more than its own WarmupBars
// closed bars — the (WarmupBars+1)th bar for a requirement is its
// first tradable one. It is monotonic — once true, it is cached and
// never recomputed. Callers must call allWarm only after every event
// in the current batch has already had its own bars-seen count
// incremented, so warm-up readiness never depends on which event
// within a batch happens to be evaluated first.
func (s *Scheduler) allWarm() bool {
	if s.warmedUp {
		return true
	}
	for key, need := range s.warmupRequired {
		if s.barsSeen[key] <= need {
			return false
		}
	}
	s.warmedUp = true
	return true
}

// submit builds and submits one intent via InputBuilder/Pipeline,
// using event as the eligibility observation (see InputBuilder's own
// doc comment). A risk rejection is not an error: Pipeline.ErrRejected
// is an expected outcome, not a Scheduler failure.
//
// Every stage of this one intent's lifecycle is journaled, in true
// causal order (issue #218, M5-10; ADR-036): Intent first, then
// Proposal and Decision — both populated whether or not risk allowed
// it, per pipeline.Result's own doc comment — and, only on approval,
// Request. The authoritative Order/Fill/Account/Status entries a
// successful submission produces are journaled by drainAndJournal
// immediately afterward, from the broker's own event stream, never
// reconstructed from pipeline.Result.Order — see drainAndJournal's own
// doc comment for why that is the sole authoritative source. A journal
// failure at any step aborts the run immediately, exactly like any
// other Scheduler failure: Runner never returns a successful Result
// alongside a silently incomplete journal.
func (s *Scheduler) submit(ctx context.Context, intent order.Intent, event strategy.BarEvent) error {
	if err := s.journalRecord(ctx, journal.Record{
		RunID:    s.deps.RunID,
		Metadata: id.Metadata{CorrelationID: intent.Metadata.CorrelationID, Timestamp: s.deps.Clock.Now()},
		Kind:     journal.KindIntent,
		Intent:   &intent,
	}); err != nil {
		return err
	}

	snap, err := s.deps.Account.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("backtest: scheduler: snapshotting account before submitting intent %s: %w", intent.IntentID, err)
	}
	in, err := s.deps.Builder.Build(ctx, intent, event, snap)
	if err != nil {
		return fmt.Errorf("backtest: scheduler: building pipeline input for intent %s: %w", intent.IntentID, err)
	}

	result, submitErr := s.deps.Pipeline.Submit(ctx, in)
	rejected := errors.Is(submitErr, pipeline.ErrRejected)
	if submitErr != nil && !rejected {
		return fmt.Errorf("backtest: scheduler: submitting intent %s: %w", intent.IntentID, submitErr)
	}

	corr := intent.Metadata.CorrelationID
	if err := s.journalRecord(ctx, journal.Record{
		RunID:    s.deps.RunID,
		Metadata: id.Metadata{CorrelationID: corr, Timestamp: s.deps.Clock.Now()},
		Kind:     journal.KindProposal,
		Proposal: &result.Proposal,
	}); err != nil {
		return err
	}
	if err := s.journalRecord(ctx, journal.Record{
		RunID:    s.deps.RunID,
		Metadata: id.Metadata{CorrelationID: corr, Timestamp: s.deps.Clock.Now()},
		Kind:     journal.KindDecision,
		Decision: &result.Decision,
	}); err != nil {
		return err
	}
	if rejected {
		return nil
	}

	if err := s.journalRecord(ctx, journal.Record{
		RunID:    s.deps.RunID,
		Metadata: id.Metadata{CorrelationID: corr, Timestamp: s.deps.Clock.Now()},
		Kind:     journal.KindRequest,
		Request:  &result.Request,
	}); err != nil {
		return err
	}

	return s.drainAndJournal(ctx)
}

// journalRecord validates and records rec, wrapping any failure as a
// Scheduler-level error so it aborts Run exactly like any other
// failure — see submit's own doc comment for why a journal failure is
// never silently absorbed.
func (s *Scheduler) journalRecord(ctx context.Context, rec journal.Record) error {
	if err := s.deps.Journal.Record(ctx, rec); err != nil {
		return fmt.Errorf("backtest: scheduler: journaling %s: %w", rec.Kind, err)
	}
	return nil
}

// drainAndJournal journals every broker.Event this run has produced
// since the last call, in delivery (Sequence) order, and accumulates
// each event's Fill payload into s.fills for DeriveTrades. It is the
// sole source of Order/Fill/Account/Status journal entries — never
// pipeline.Result.Order, which is only ever a mirror of the broker's
// own synchronous acceptance and would otherwise be journaled twice
// for the same underlying event.
//
// Called once per submit (so broker-side entries interleave with the
// Intent/Proposal/Decision/Request that caused them in true execution
// order, not batched at the end of the run), it reuses the same
// broker.FiniteEventReader capability Runner's trade derivation relies
// on elsewhere, opening a fresh reader from the beginning of the
// account's log every time and skipping anything at or below
// s.lastBrokerSeq — Sequence is public, non-opaque API (unlike
// EventCursor), so comparing against a remembered watermark is not
// interpreting broker-internal state. Re-scanning from the start on
// every call is O(n) per call given sim's lack of a real resumable
// cursor (account.Snapshot.Cursor is never set), so O(n^2) over a full
// run in the worst case — acceptable for realistic backtest sizes
// (hundreds of intents), not fixed in this issue.
func (s *Scheduler) drainAndJournal(ctx context.Context) error {
	reader, err := s.deps.Account.Events(ctx, "")
	if err != nil {
		return fmt.Errorf("backtest: scheduler: opening event stream: %w", err)
	}
	defer func() { _ = reader.Close() }()

	finite, ok := reader.(broker.FiniteEventReader)
	if !ok {
		return fmt.Errorf("backtest: scheduler: %w: %T", ErrEventReaderNotFinite, reader)
	}

	for !finite.AtEnd() {
		event, err := reader.Next(ctx)
		if err != nil {
			return fmt.Errorf("backtest: scheduler: draining event stream: %w", err)
		}
		if event.Sequence <= s.lastBrokerSeq {
			continue
		}

		rec := journal.Record{RunID: s.deps.RunID, Metadata: event.Metadata}
		switch event.Kind {
		case broker.EventKindOrder:
			rec.Kind, rec.Order = journal.KindOrder, event.Order
		case broker.EventKindFill:
			rec.Kind, rec.Fill = journal.KindFill, event.Fill
		case broker.EventKindAccount:
			rec.Kind, rec.Account = journal.KindAccount, event.Account
		case broker.EventKindStatus:
			rec.Kind, rec.Status = journal.KindStatus, event.Status
		default:
			// An event kind this journal has no representation for.
			// Still advance the watermark past it — there is nothing
			// to journal or collect, so there is nothing left pending
			// that a later retry could need to redo.
			s.lastBrokerSeq = event.Sequence
			continue
		}

		// Journal first; only advance the watermark and collect the
		// fill once that succeeds (issue #236 review). Committing
		// lastBrokerSeq/fills before the journal write is durable would
		// let Scheduler's own state claim an event was recorded when it
		// was not — harmless today because a journal failure aborts
		// Run immediately, but the invariant lastBrokerSeq documents
		// ("the highest event already journaled/collected") must stay
		// true regardless of what a future caller does with a partially
		// run Scheduler.
		if err := s.journalRecord(ctx, rec); err != nil {
			return err
		}
		s.lastBrokerSeq = event.Sequence
		if event.Kind == broker.EventKindFill {
			s.fills = append(s.fills, *event.Fill)
		}
	}
	return nil
}

// Fills returns every order.Fill observed over the course of Run, in
// delivery order — the input DeriveTrades needs. Call only after Run
// has returned successfully.
func (s *Scheduler) Fills() []order.Fill {
	return append([]order.Fill(nil), s.fills...)
}

// EquityCurve returns one authoritative, mark-to-market EquityPoint
// per batch observed over the course of Run, in chronological order —
// the input Runner passes to NewMetrics (issue #219, M5-11). Call only
// after Run has returned successfully.
func (s *Scheduler) EquityCurve() []EquityPoint {
	return append([]EquityPoint(nil), s.equityCurve...)
}

// BatchBars is an optional capability a strategy.View handed to OnBar
// during a Scheduler-driven run implements: every strategy.BarEvent in
// the current same-timestamp batch, not only the one that triggered
// this particular OnBar call. See Scheduler's own doc comment.
type BatchBars interface {
	// Bars returns every bar in the current batch, in the same
	// canonical order Replay yielded them — a defensive copy the
	// caller may freely retain or mutate.
	Bars() []strategy.BarEvent
}

// historyView is frozenView's strategy.History implementation. buffers
// is shared, mutable, append-only state owned by Scheduler; cutoffs
// and declared are frozen at construction time, which is what makes a
// retained View's visibility permanent regardless of how much buffers
// grows afterward.
type historyView struct {
	buffers  map[requirementKey][]marketdata.Bar
	cutoffs  map[requirementKey]int
	declared map[requirementKey]int
}

func (h historyView) HistoryBars(instID instrument.ID, interval marketdata.Interval, n int) ([]marketdata.Bar, bool) {
	key := requirementKey{instrument: instID, interval: interval}
	if _, ok := h.declared[key]; !ok {
		return nil, false
	}
	if n <= 0 {
		return []marketdata.Bar{}, true
	}

	cutoff := min(h.cutoffs[key], len(h.buffers[key]))
	full := h.buffers[key]
	start := max(cutoff-n, 0)

	out := make([]marketdata.Bar, cutoff-start)
	copy(out, full[start:cutoff])
	return out, true
}

// frozenView is a strategy.View backed by one fixed account.Snapshot,
// one fixed batch of BarEvents, and one fixed historyView — all
// captured once per Scheduler batch. See Scheduler's own doc comment
// for why account/history state must not flow forward within, or
// after, a same-timestamp batch. frozenView implements BatchBars and
// strategy.History.
type frozenView struct {
	snapshot account.Snapshot
	batch    []strategy.BarEvent
	history  historyView
}

func (v frozenView) Account() account.Snapshot { return v.snapshot }

func (v frozenView) Bars() []strategy.BarEvent {
	return append([]strategy.BarEvent(nil), v.batch...)
}

func (v frozenView) HistoryBars(instID instrument.ID, interval marketdata.Interval, n int) ([]marketdata.Bar, bool) {
	return v.history.HistoryBars(instID, interval, n)
}

var _ BatchBars = frozenView{}
var _ strategy.History = frozenView{}
var _ strategy.View = frozenView{}
