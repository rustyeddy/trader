package backtest_test

// This file is PR #337 review's own regression: Scheduler.submit
// unconditionally journaled Proposal/Decision/Request after every
// Pipeline.Submit call, which — for an order.IntentAdjustStop that
// took the replacement path — meant journaling three hollow,
// zero-valued records and silently discarding the actual
// ReplaceRequest. This proves the fix with a real end-to-end run
// through a real JSONLWriter, mirroring
// TestRunner_JournalsFullRunInCausalOrder's own pattern exactly.

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/journal/jsonl"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy"
)

// enterThenRatchetStopStrategy drives EUR/USD through exactly the
// sequence this file's test needs: enter once flat, place the initial
// protective stop once long with no resting stop yet, then ratchet it
// upward once a resting stop exists — ignoring GBP/USD (the fixture's
// other, unrelated instrument) entirely.
type enterThenRatchetStopStrategy struct {
	requirements []strategy.DataRequirement
	intents      strategy.IntentFactory
	eurusd       instrument.ID
	ratcheted    bool
}

func (s *enterThenRatchetStopStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "enter-then-ratchet-stop", Version: "test", Requirements: s.requirements}
}

func (s *enterThenRatchetStopStrategy) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	return nil
}

func (s *enterThenRatchetStopStrategy) OnBar(ctx context.Context, ev strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	if !ev.Instrument.Equal(s.eurusd) {
		return nil, nil
	}

	var hasPosition, hasRestingStop bool
	for _, p := range view.Account().Positions() {
		if p.Listing.InstrumentID().Equal(s.eurusd) {
			hasPosition = true
		}
	}
	for _, o := range view.Account().OpenOrders() {
		if o.Request.Type == order.Stop && o.Request.Listing.InstrumentID().Equal(s.eurusd) && !o.Status.Terminal() {
			hasRestingStop = true
		}
	}

	switch {
	case !hasPosition:
		in, err := s.intents.Enter(s.eurusd, order.Buy)
		if err != nil {
			return nil, err
		}
		return []order.Intent{in}, nil
	case !hasRestingStop:
		in, err := s.intents.AdjustStop(s.eurusd, num.MustParsePrice("1.05000"))
		if err != nil {
			return nil, err
		}
		return []order.Intent{in}, nil
	case !s.ratcheted:
		s.ratcheted = true
		in, err := s.intents.AdjustStop(s.eurusd, num.MustParsePrice("1.06000"))
		if err != nil {
			return nil, err
		}
		return []order.Intent{in}, nil
	default:
		return nil, nil
	}
}

// TestRunner_ReplaceJournalsReplaceRequestNotHollowRecords is PR #337
// review's own regression test: a real run whose only IntentAdjustStop
// ratchet takes the replacement path must journal a real, populated
// journal.KindReplaceRequest entry — never a KindProposal/KindDecision/
// KindRequest triple of zero-valued records for that same intent.
func TestRunner_ReplaceJournalsReplaceRequestNotHollowRecords(t *testing.T) {
	strat := &enterThenRatchetStopStrategy{requirements: bothInstrumentsRequirements(t), eurusd: eurusdID(t)}
	params := mustRunnerParams(t, strat)

	path := filepath.Join(t.TempDir(), "run.jsonl")
	w, err := jsonl.NewWriter(path)
	require.NoError(t, err)
	params.Journal = w

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)
	_, err = runner.Run(context.Background())
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r, err := jsonl.OpenReader(path)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	var entries []journal.Entry
	for {
		e, err := r.Next(context.Background())
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		entries = append(entries, e)
	}
	require.NotEmpty(t, entries)

	var replaceRequests []journal.Entry
	for _, e := range entries {
		if e.Kind == journal.KindReplaceRequest {
			replaceRequests = append(replaceRequests, e)
		}
	}
	require.Len(t, replaceRequests, 1, "exactly one IntentAdjustStop in this run took the replacement path")

	rr := replaceRequests[0]
	require.NotNil(t, rr.ReplaceRequest)
	require.NotNil(t, rr.ReplaceRequest.NewStopPrice)
	assert.Equal(t, "1.06", rr.ReplaceRequest.NewStopPrice.String(), "the journaled replace request carries the real ratcheted price, not a zero value")
	assert.False(t, rr.ReplaceRequest.OrderID.IsZero(), "the journaled replace request names the real resting order, not a zero value")

	// The regression this test guards against: no KindProposal/
	// KindDecision/KindRequest sharing this replace's own CorrelationID
	// was journaled as a hollow, zero-valued record standing in for it.
	for _, e := range entries {
		if e.Metadata.CorrelationID != rr.Metadata.CorrelationID {
			continue
		}
		switch e.Kind {
		case journal.KindProposal, journal.KindDecision, journal.KindRequest:
			t.Fatalf("intent %s's replacement was also journaled as hollow %s, kind=%v", rr.Metadata.CorrelationID, e.Kind, e.Kind)
		}
	}
}
