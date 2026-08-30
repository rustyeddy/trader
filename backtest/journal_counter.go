package backtest

import (
	"context"

	"github.com/rustyeddy/trader/journal"
)

// countingRecorder wraps a journal.Recorder, counting every record
// successfully written so Runner can report an accurate EntryCount on
// the closing journal.KindRunCompleted record — the total is naturally
// shared state across every caller of one run's Recorder (both
// Scheduler and Runner itself record to it), so it is tracked once
// here rather than reconstructed after the fact from Sequence values a
// concrete Recorder implementation may or may not expose a way to
// query.
type countingRecorder struct {
	journal.Recorder
	count uint64
}

func newCountingRecorder(inner journal.Recorder) *countingRecorder {
	return &countingRecorder{Recorder: inner}
}

func (c *countingRecorder) Record(ctx context.Context, rec journal.Record) error {
	if err := c.Recorder.Record(ctx, rec); err != nil {
		return err
	}
	c.count++
	return nil
}

var _ journal.Recorder = (*countingRecorder)(nil)
