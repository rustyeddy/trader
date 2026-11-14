package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/rustyeddy/trader/internal/marketdata/oanda"
)

// candleFetcher is the part of the OANDA client the download needs.
type candleFetcher interface {
	FetchCandles(ctx context.Context, req oanda.CandleRequest) ([]oanda.Record, error)
}

// downloadRequest is what one run asks for: every symbol over [From, To), UTC.
type downloadRequest struct {
	Symbols  []string
	Interval oanda.RawInterval
	From, To time.Time
}

// partitionResult is what one month partition came to.
type partitionResult struct {
	Key        oanda.PartitionKey
	Path       string
	Fetched    int // candles the provider returned for the month's window
	Incomplete int // candles still forming, never stored
	Added      int // rows written that the file did not have
	Kept       int // rows already on file, left exactly as they were
	Differing  int // of Kept, rows whose provider values differ from the file's
}

// errNothingToAdd ends a merge that would change nothing, so the file is not
// rewritten (or created) for no reason.
var errNothingToAdd = errors.New("nothing to add")

// downloadMonths fetches req one UTC month partition at a time and merges each
// into root through UpdatePartition. The raw archive is immutable (ADR-020): a
// row already on file is never changed or removed, even when the provider now
// reports different values (that is counted and logged, not applied), and a
// file that would gain nothing is left untouched. A candle the provider still
// reports as forming is not stored, since its values will change. report is
// called after each partition, in order. On error the partitions already done
// stay done.
func downloadMonths(ctx context.Context, log *slog.Logger, f candleFetcher, root string, req downloadRequest, report func(partitionResult)) error {
	if !req.To.After(req.From) {
		return errors.New("download: --to must be after --from")
	}
	for _, symbol := range req.Symbols {
		for ms := monthStart(req.From); ms.Before(req.To); ms = ms.AddDate(0, 1, 0) {
			res, err := downloadMonth(ctx, log, f, root, symbol, req, ms)
			if err != nil {
				return fmt.Errorf("%s %04d-%02d: %w", symbol, ms.Year(), int(ms.Month()), err)
			}
			if report != nil {
				report(res)
			}
		}
	}
	return nil
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func downloadMonth(ctx context.Context, log *slog.Logger, f candleFetcher, root, symbol string, req downloadRequest, ms time.Time) (partitionResult, error) {
	key := oanda.PartitionKey{Symbol: symbol, Interval: req.Interval, Year: ms.Year(), Month: ms.Month()}
	path, err := key.Path(root)
	if err != nil {
		return partitionResult{}, err
	}
	res := partitionResult{Key: key, Path: path}

	from, to := later(req.From.UTC(), ms), earlier(req.To.UTC(), ms.AddDate(0, 1, 0))
	fetched, err := f.FetchCandles(ctx, oanda.CandleRequest{Symbol: symbol, Interval: req.Interval, From: from, To: to})
	if err != nil {
		return res, err
	}
	res.Fetched = len(fetched)
	// The durable-storage boundary checks what the fetcher returned rather than
	// trusting it: every candle opens inside the requested window, hence inside
	// this partition's UTC month.
	for _, r := range fetched {
		if t := r.Time.UTC(); t.Before(from) || !t.Before(to) {
			return res, fmt.Errorf("provider returned a candle opening %s, outside the requested window [%s, %s)",
				t.Format(time.RFC3339), from.Format(time.RFC3339), to.Format(time.RFC3339))
		}
	}
	var fresh []oanda.Record
	for _, r := range fetched {
		if !r.Complete {
			res.Incomplete++
			continue
		}
		fresh = append(fresh, r)
	}

	err = oanda.UpdatePartition(ctx, root, key, func(existing []oanda.Record) ([]oanda.Record, error) {
		have := make(map[int64]oanda.Record, len(existing))
		for _, r := range existing {
			have[r.Time.UnixNano()] = r
		}
		merged := existing
		for _, r := range fresh {
			if old, ok := have[r.Time.UnixNano()]; ok {
				res.Kept++
				if !sameCandle(old, r) {
					res.Differing++
					log.Warn("provider values differ from the stored row; the stored row is kept",
						"symbol", symbol, "time", r.Time.UTC().Format(time.RFC3339))
				}
				continue
			}
			res.Added++
			merged = append(merged, r)
		}
		if res.Added == 0 {
			return nil, errNothingToAdd
		}
		return merged, nil
	})
	if err != nil && !errors.Is(err, errNothingToAdd) {
		return res, err
	}
	log.Info("partition done", "path", path, "fetched", res.Fetched, "added", res.Added,
		"kept", res.Kept, "incomplete", res.Incomplete)
	return res, nil
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// sameCandle reports whether two records are the same candle, ignoring the
// location the time is expressed in.
func sameCandle(a, b oanda.Record) bool {
	at, bt := a.Time, b.Time
	a.Time, b.Time = time.Time{}, time.Time{}
	return at.Equal(bt) && a == b
}
