package backtest

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/strategy"
)

// ErrInvalidSchedulerDeps marks a SchedulerDeps missing a required
// field.
var ErrInvalidSchedulerDeps = errors.New("backtest: invalid scheduler dependencies")

// InputBuilder turns one order.Intent a Strategy emitted — together
// with the strategy.BarEvent that triggered it and the account
// snapshot Scheduler is about to submit against — into the canonical
// pipeline.Input the M4 pipeline needs.
//
// This is a seam Scheduler injects rather than owns (issue #213
// review). Listing resolution and a triggering/reference price are
// mechanical runtime concerns, but RiskFraction and AdverseDistance
// are execution/risk *policy* — they belong to the concrete M5 run
// composition (and #215's run manifest) to configure, not to
// Scheduler's own public API. Scheduler asks Build to turn a valid
// emitted Intent plus its triggering observation into the canonical
// M4 input; the resulting pipeline.Input still goes through the
// unchanged pipeline.Pipeline — Build is not a second execution/risk
// path.
//
// Build must be deterministic: given the same intent, event, and
// account snapshot, it must always return the same pipeline.Input.
type InputBuilder interface {
	Build(ctx context.Context, intent order.Intent, event strategy.BarEvent, snapshot account.Snapshot) (pipeline.Input, error)
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
	return nil
}

// Scheduler is the deterministic event scheduler coordinating
// historical replay, strategy evaluation, and M4 execution/risk
// submission (issue #213, M5-05, ADR-035).
//
// # Same-timestamp batching
//
// Replay's own (timestamp, instrument ID, interval) order is a
// deterministic *serialization* of observations, not a claim about
// market-time precedence: two bars sharing one timestamp are logically
// simultaneous. Scheduler therefore groups every consecutive event
// Replay yields for one identical bar timestamp T into a single
// simulation step:
//
//  1. clock.AdvanceTo(T) once for the whole batch.
//  2. Capture one account snapshot — frozen for the whole batch.
//  3. Call Strategy.OnBar once per event in the batch, in Replay's own
//     canonical order, but every call sees the same frozen View: all
//     of T's bars are genuinely visible to every call in the batch —
//     not only the one bar that triggered it — via View's optional
//     BatchBars capability (Bars() []strategy.BarEvent), which returns
//     every event in the current batch regardless of which one is
//     "this" call's own BarEvent. What no call's View reflects is any
//     other T-batch call's account/execution effects. This is what
//     keeps Replay's incidental instrument-ID ordering from silently
//     becoming trading semantics: EURUSD sorting before GBPUSD at the
//     same T must never change GBPUSD's own strategy decision, while
//     GBPUSD's strategy can still see EURUSD's T bar if it needs to.
//  4. Only after every event in the batch has been evaluated are the
//     collected intents translated (via InputBuilder) and submitted
//     through Pipeline, in the same deterministic order the intents
//     were emitted in.
//
// BatchBars is intentionally not part of the base strategy.View
// contract: #214 (M5-06) owns defining the fuller no-lookahead/
// historical-bar-visibility API. BatchBars is the narrow, Scheduler-
// owned seam that makes true T-batch visibility real now, without
// Scheduler guessing at #214's eventual View method shape.
//
// One exception: if Strategy itself mutates its own internal state
// during one OnBar call, a later call within the same batch can
// observe that strategy-local state change — Scheduler does not, and
// cannot, isolate a Strategy value's own fields. What Scheduler
// guarantees is narrower and specific: account/execution state does
// not flow forward within a T batch.
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
// ctx is checked at the start of every batch, before each OnBar call,
// and before each pipeline submission. A cancellation observed mid-
// batch stops before the next step; whatever was already submitted
// remains committed — a canceled Run is a defined partial run, not a
// rollback.
type Scheduler struct {
	deps    SchedulerDeps
	pending *strategy.BarEvent
}

// NewScheduler returns a Scheduler over deps. Every field of deps must
// be set.
func NewScheduler(deps SchedulerDeps) (*Scheduler, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	return &Scheduler{deps: deps}, nil
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
// next batch (if any) in s.pending for the following call. It returns
// a nil, nil-error batch once Replay is exhausted with nothing
// buffered.
func (s *Scheduler) nextBatch(ctx context.Context) ([]strategy.BarEvent, error) {
	first := s.pending
	s.pending = nil
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
			s.pending = &ev
			break
		}
		batch = append(batch, ev)
	}
	return batch, nil
}

// emittedIntent pairs one order.Intent a Strategy emitted with the
// BarEvent that triggered it, so InputBuilder.Build knows what
// observation produced it.
type emittedIntent struct {
	intent order.Intent
	event  strategy.BarEvent
}

// runBatch executes one simulation step for every event sharing one
// timestamp: advance the clock once, evaluate the whole batch against
// one frozen account snapshot, then submit every collected intent, in
// order, each freshly snapshotted so risk admission reflects any prior
// submission within this same batch (submission ordering is not
// subject to the same no-lookahead concern the View freeze exists
// for — Pipeline's own risk admission is meant to see real, current
// account state, mirroring how a live session's account is always
// authoritative).
func (s *Scheduler) runBatch(ctx context.Context, batch []strategy.BarEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	t := batch[0].Bar.Time
	if err := s.deps.Clock.AdvanceTo(t); err != nil {
		return fmt.Errorf("backtest: scheduler: advancing clock to %s: %w", t, err)
	}

	frozen, err := s.deps.Account.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("backtest: scheduler: snapshotting account before %s: %w", t, err)
	}
	view := frozenView{snapshot: frozen, batch: batch}

	var toSubmit []emittedIntent
	for _, ev := range batch {
		if err := ctx.Err(); err != nil {
			return err
		}
		intents, err := s.deps.Strategy.OnBar(ctx, ev, view)
		if err != nil {
			return fmt.Errorf("backtest: scheduler: OnBar for %s %s at %s: %w", ev.Instrument, ev.Interval, t, err)
		}
		for _, in := range intents {
			toSubmit = append(toSubmit, emittedIntent{intent: in, event: ev})
		}
	}

	for _, e := range toSubmit {
		if err := ctx.Err(); err != nil {
			return err
		}

		snap, err := s.deps.Account.Snapshot(ctx)
		if err != nil {
			return fmt.Errorf("backtest: scheduler: snapshotting account before submitting intent %s: %w", e.intent.IntentID, err)
		}
		in, err := s.deps.Builder.Build(ctx, e.intent, e.event, snap)
		if err != nil {
			return fmt.Errorf("backtest: scheduler: building pipeline input for intent %s: %w", e.intent.IntentID, err)
		}
		if _, err := s.deps.Pipeline.Submit(ctx, in); err != nil {
			if errors.Is(err, pipeline.ErrRejected) {
				continue
			}
			return fmt.Errorf("backtest: scheduler: submitting intent %s: %w", e.intent.IntentID, err)
		}
	}
	return nil
}

// BatchBars is an optional capability a strategy.View handed to OnBar
// during a Scheduler-driven run implements: every strategy.BarEvent in
// the current same-timestamp batch, not only the one that triggered
// this particular OnBar call. This is what makes "all observations at
// T are visible before any T callback" (issue #213 review) real rather
// than only documented, without pulling #214's own historical-bar
// lookup API into Scheduler: a strategy that wants T's other
// instruments' bars type-asserts its View against BatchBars; a
// strategy that only needs its own triggering bar (View.Account plus
// the BarEvent OnBar already received) never needs to.
type BatchBars interface {
	// Bars returns every bar in the current batch, in the same
	// canonical order Replay yielded them — a defensive copy the
	// caller may freely retain or mutate.
	Bars() []strategy.BarEvent
}

// frozenView is a strategy.View backed by one fixed account.Snapshot
// and one fixed batch of BarEvents, both captured once per Scheduler
// batch — see Scheduler's own doc comment for why account state must
// not flow forward within a same-timestamp batch. frozenView also
// implements BatchBars.
type frozenView struct {
	snapshot account.Snapshot
	batch    []strategy.BarEvent
}

func (v frozenView) Account() account.Snapshot { return v.snapshot }

func (v frozenView) Bars() []strategy.BarEvent {
	return append([]strategy.BarEvent(nil), v.batch...)
}

var _ BatchBars = frozenView{}

var _ strategy.View = frozenView{}
