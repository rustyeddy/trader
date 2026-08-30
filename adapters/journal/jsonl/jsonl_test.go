package jsonl_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/adapters/journal/jsonl"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/risk"
)

func intentPtr(v order.Intent) *order.Intent       { return &v }
func proposalPtr(v order.Proposal) *order.Proposal { return &v }
func decisionPtr() *risk.Decision {
	return &risk.Decision{
		Allowed:    false,
		Violations: []risk.Violation{{Rule: "max-exposure", Message: "too big", Measured: "5000", Limit: "1000"}},
		Warnings:   []risk.Warning{{Rule: "spread", Message: "wide spread"}},
		RuleResults: []risk.RuleResult{
			{Rule: "max-exposure", Violations: []risk.Violation{{Rule: "max-exposure", Message: "too big", Measured: "5000", Limit: "1000"}}},
			{Rule: "spread", Warnings: []risk.Warning{{Rule: "spread", Message: "wide spread"}}},
		},
	}
}
func requestPtr(v order.Request) *order.Request       { return &v }
func orderPtr(v order.Order) *order.Order             { return &v }
func fillPtr(v order.Fill) *order.Fill                { return &v }
func accountPtr(v account.Snapshot) *account.Snapshot { return &v }
func statusPtr(v broker.Status) *broker.Status        { return &v }
func tradePtr(v order.Trade) *order.Trade             { return &v }

func mustWriter(t *testing.T) (*jsonl.Writer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.jsonl")
	w, err := jsonl.NewWriter(path)
	require.NoError(t, err)
	return w, path
}

func readAll(t *testing.T, path string) []journal.Entry {
	t.Helper()
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
	return entries
}

func TestWriterReaderRoundTripsEveryKind(t *testing.T) {
	w, path := mustWriter(t)
	runID := mustRunID(t)
	now := time.Now()

	records := []journal.Record{
		{RunID: runID, Metadata: id.Metadata{Timestamp: now}, Kind: journal.KindRunStarted, RunStarted: &journal.RunStarted{RunID: runID, Header: []byte(`{"strategy":"test"}`)}},
		{RunID: runID, Metadata: id.Metadata{EventID: mustEventID(t), CorrelationID: mustCorrelationID(t), Timestamp: now}, Kind: journal.KindIntent, Intent: intentPtr(mustIntent(t))},
		{RunID: runID, Metadata: id.Metadata{Timestamp: now}, Kind: journal.KindProposal, Proposal: proposalPtr(mustProposal(t))},
		{RunID: runID, Metadata: id.Metadata{Timestamp: now}, Kind: journal.KindDecision, Decision: decisionPtr()},
		{RunID: runID, Metadata: id.Metadata{Timestamp: now}, Kind: journal.KindRequest, Request: requestPtr(mustRequest(t))},
		{RunID: runID, Metadata: id.Metadata{Timestamp: now}, Kind: journal.KindOrder, Order: orderPtr(mustWorkingOrder(t))},
		{RunID: runID, Metadata: id.Metadata{Timestamp: now}, Kind: journal.KindFill, Fill: fillPtr(mustFill(t))},
		{RunID: runID, Metadata: id.Metadata{Timestamp: now}, Kind: journal.KindAccount, Account: accountPtr(mustSnapshot(t))},
		{RunID: runID, Metadata: id.Metadata{Timestamp: now}, Kind: journal.KindStatus, Status: statusPtr(mustStatus())},
		{RunID: runID, Metadata: id.Metadata{Timestamp: now}, Kind: journal.KindTrade, Trade: tradePtr(mustTrade(t))},
		{RunID: runID, Metadata: id.Metadata{Timestamp: now}, Kind: journal.KindRunCompleted, RunCompleted: &journal.RunCompleted{RunID: runID, EntryCount: 10}},
	}

	for _, rec := range records {
		require.NoError(t, w.Record(context.Background(), rec))
	}
	require.NoError(t, w.Close())

	entries := readAll(t, path)
	require.Len(t, entries, len(records))

	for i, e := range entries {
		assert.Equal(t, uint64(i+1), e.Sequence, "sequence must be assigned in write order, starting at 1")
		assert.Equal(t, records[i].Kind, e.Kind)
	}

	// Spot-check a few fields survive the round trip faithfully.
	assert.Equal(t, records[1].Intent.IntentID, entries[1].Intent.IntentID)
	assert.Equal(t, records[1].Intent.Instrument, entries[1].Intent.Instrument)
	assert.True(t, records[6].Fill.Price.Equal(entries[6].Fill.Price))
	assert.Equal(t, records[6].Fill.Listing.Symbol(), entries[6].Fill.Listing.Symbol())
	assert.True(t, records[7].Account.Equity().Equal(entries[7].Account.Equity()))
	assert.Equal(t, records[8].Status.State, entries[8].Status.State)
	assert.Equal(t, records[9].Trade.Listing.InstrumentID().String(), entries[9].Trade.Listing.InstrumentID().String())
}

func TestWriterReaderRoundTripsRejectedOrder(t *testing.T) {
	w, path := mustWriter(t)
	runID := mustRunID(t)
	rejected := mustRejectedOrder(t)

	require.NoError(t, w.Record(context.Background(), journal.Record{
		RunID: runID, Metadata: id.Metadata{Timestamp: time.Now()}, Kind: journal.KindOrder, Order: orderPtr(rejected),
	}))
	require.NoError(t, w.Close())

	entries := readAll(t, path)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].Order.Rejection)
	assert.Equal(t, rejected.Rejection.Reason, entries[0].Order.Rejection.Reason)
	assert.Equal(t, rejected.Rejection.Detail, entries[0].Order.Rejection.Detail)
	assert.Equal(t, rejected.Rejection.BrokerCode, entries[0].Order.Rejection.BrokerCode)
}

func TestWriterAssignsStrictlyIncreasingSequence(t *testing.T) {
	w, path := mustWriter(t)
	runID := mustRunID(t)

	for range 5 {
		rec := journal.Record{RunID: runID, Metadata: id.Metadata{Timestamp: time.Now()}, Kind: journal.KindTrade, Trade: tradePtr(mustTrade(t))}
		require.NoError(t, w.Record(context.Background(), rec))
	}
	require.NoError(t, w.Close())

	entries := readAll(t, path)
	require.Len(t, entries, 5)
	for i, e := range entries {
		assert.Equal(t, uint64(i+1), e.Sequence)
	}
}

func TestWriterRejectsInvalidRecord(t *testing.T) {
	w, _ := mustWriter(t)
	defer func() { _ = w.Close() }()

	err := w.Record(context.Background(), journal.Record{})
	require.ErrorIs(t, err, journal.ErrInvalidRecord)
}

func TestWriterRecordAfterCloseFails(t *testing.T) {
	w, _ := mustWriter(t)
	require.NoError(t, w.Close())

	err := w.Record(context.Background(), journal.Record{RunID: mustRunID(t), Metadata: id.Metadata{Timestamp: time.Now()}, Kind: journal.KindTrade, Trade: tradePtr(mustTrade(t))})
	require.ErrorIs(t, err, journal.ErrClosed)
}

func TestWriterCloseIsIdempotent(t *testing.T) {
	w, _ := mustWriter(t)
	require.NoError(t, w.Close())
	require.NoError(t, w.Close())
}

func TestReaderDetectsCorruptFinalLine(t *testing.T) {
	w, path := mustWriter(t)
	require.NoError(t, w.Record(context.Background(), journal.Record{RunID: mustRunID(t), Metadata: id.Metadata{Timestamp: time.Now()}, Kind: journal.KindTrade, Trade: tradePtr(mustTrade(t))}))
	require.NoError(t, w.Close())

	appendRaw(t, path, `{"run_id":"not even close to valid json`)

	r, err := jsonl.OpenReader(path)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	_, err = r.Next(context.Background())
	require.NoError(t, err, "first (valid) line must still read fine")

	_, err = r.Next(context.Background())
	require.ErrorIs(t, err, jsonl.ErrCorruptEntry)
}

func TestReaderRejectsUnrecognizedKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.jsonl")
	appendRaw(t, path, `{"run_id":"x","sequence":1,"metadata":{"timestamp":"2024-01-01T00:00:00Z"},"kind":"not-a-real-kind"}`)

	r, err := jsonl.OpenReader(path)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	_, err = r.Next(context.Background())
	require.ErrorIs(t, err, jsonl.ErrCorruptEntry)
}

func appendRaw(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(line + "\n")
	require.NoError(t, err)
}
