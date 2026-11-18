package oanda

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// CandleFetcher is the part of the Client that DownloadPartition needs; a test
// supplies a fake.
type CandleFetcher interface {
	FetchCandles(ctx context.Context, req CandleRequest) ([]Record, error)
}

// DownloadResult is what one partition's download came to.
type DownloadResult struct {
	Key PartitionKey
	// Path is the partition file's path relative to the data root.
	Path       string
	Fetched    int // candles the provider returned for the window
	Incomplete int // candles still forming, never stored
	Added      int // rows written that the file did not have
	Replaced   int // stored rows that were still forming, replaced by the closed candle
	Kept       int // complete rows already on file, left exactly as they were
	Differing  int // of Kept, rows whose provider values differ from the file's
}

// errNothingToAdd ends a merge that would change nothing, so the file is not
// rewritten (or created) for no reason.
var errNothingToAdd = errors.New("nothing to add")

// DownloadPartition fetches the candles of key's partition for the window
// [from, to), which the caller keeps inside key's UTC month, and merges them into
// the raw store through UpdatePartition.
//
// A complete row already on file is never changed or removed (ADR-020), even when
// the provider now reports different values (that is counted and logged, not
// applied): a closed candle is a fact. A candle the provider still reports as
// forming (Record.Complete false) is not stored, since its values will change; and
// a forming row that is already on file (written before that rule) is not a fact
// either, so when the provider reports its candle closed the stored row is replaced
// by it, in place (counted as Replaced and logged with both values). A file that
// would gain and change nothing is left untouched, and is not created. A candle
// opening outside [from, to) fails the download before anything is written: the
// fetcher is checked rather than trusted at the durable-storage boundary. On
// error the file is untouched. log may be nil.
func DownloadPartition(ctx context.Context, log *slog.Logger, f CandleFetcher, key PartitionKey, from, to time.Time) (DownloadResult, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	rel, err := key.Path()
	if err != nil {
		return DownloadResult{}, err
	}
	res := DownloadResult{Key: key, Path: rel}

	from, to = from.UTC(), to.UTC()
	fetched, err := f.FetchCandles(ctx, CandleRequest{Symbol: key.Symbol, Interval: key.Interval, From: from, To: to})
	if err != nil {
		return res, err
	}
	res.Fetched = len(fetched)
	for _, r := range fetched {
		if t := r.Time.UTC(); t.Before(from) || !t.Before(to) {
			return res, fmt.Errorf("provider returned a candle opening %s, outside the requested window [%s, %s)",
				t.Format(time.RFC3339), from.Format(time.RFC3339), to.Format(time.RFC3339))
		}
	}
	var fresh []Record
	for _, r := range fetched {
		if !r.Complete {
			res.Incomplete++
			continue
		}
		fresh = append(fresh, r)
	}

	err = UpdatePartition(ctx, key, func(existing []Record) ([]Record, error) {
		merged := append([]Record(nil), existing...)
		at := make(map[int64]int, len(existing)) // where each time sits in merged
		for i, r := range existing {
			at[r.Time.UnixNano()] = i
		}
		for _, r := range fresh {
			if i, ok := at[r.Time.UnixNano()]; ok {
				old := merged[i]
				if !old.Complete {
					res.Replaced++
					merged[i] = r
					log.Info("replaced a stored row that was still forming with the closed candle",
						"symbol", key.Symbol, "time", r.Time.UTC().Format(time.RFC3339),
						"was_bid_close", old.BidClose.String(), "now_bid_close", r.BidClose.String(),
						"was_volume", old.Volume, "now_volume", r.Volume)
					continue
				}
				res.Kept++
				if !sameCandle(old, r) {
					res.Differing++
					log.Warn("provider values differ from the stored row; the stored row is kept",
						"symbol", key.Symbol, "time", r.Time.UTC().Format(time.RFC3339))
				}
				continue
			}
			res.Added++
			merged = append(merged, r)
			at[r.Time.UnixNano()] = len(merged) - 1
		}
		if res.Added == 0 && res.Replaced == 0 {
			return nil, errNothingToAdd
		}
		return merged, nil
	})
	if err != nil && !errors.Is(err, errNothingToAdd) {
		return res, err
	}
	log.Info("partition done", "path", rel, "fetched", res.Fetched, "added", res.Added,
		"replaced", res.Replaced, "kept", res.Kept, "incomplete", res.Incomplete)
	return res, nil
}

// sameCandle reports whether two records are the same candle, ignoring the
// location the time is expressed in.
func sameCandle(a, b Record) bool {
	at, bt := a.Time, b.Time
	a.Time, b.Time = time.Time{}, time.Time{}
	return at.Equal(bt) && a == b
}
