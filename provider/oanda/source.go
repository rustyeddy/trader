package oanda

import (
	"context"

	"github.com/rustyeddy/trader/marketdata"
)

// ReadSourcePartition reads the raw partition for key through the store and maps
// it to the provider-neutral marketdata.SourcePartition the market-data service
// converts to canonical bars. It parses and maps only: each row becomes a
// marketdata.SourceRow carrying the provider's bid and ask exactly as stored, its
// 1-based position among the data rows, and OANDA's complete flag. It derives no
// mid, sets no AvailableAt and rejects nothing: a crossed or oddly shaped row is
// passed through for the domain to judge, and only a row that cannot be parsed
// (ErrMalformedData) or a missing file is an error. The fingerprint is of the
// exact bytes the rows were parsed from, read once (ReadPartitionSnapshot).
func ReadSourcePartition(ctx context.Context, key PartitionKey) (marketdata.SourcePartition, error) {
	snap, err := ReadPartitionSnapshot(ctx, key)
	if err != nil {
		return marketdata.SourcePartition{}, err
	}
	rows := make([]marketdata.SourceRow, 0, len(snap.Records))
	for i, r := range snap.Records {
		rows = append(rows, marketdata.SourceRow{
			Row:      i + 1,
			Complete: r.Complete,
			Bar: marketdata.Bar{
				Time:   r.Time.UTC(),
				Volume: r.Volume,
				Bid:    marketdata.OHLC{Open: r.BidOpen, High: r.BidHigh, Low: r.BidLow, Close: r.BidClose},
				Ask:    marketdata.OHLC{Open: r.AskOpen, High: r.AskHigh, Low: r.AskLow, Close: r.AskClose},
			},
		})
	}
	return marketdata.SourcePartition{
		Source:      source,
		Instrument:  key.Symbol,
		Interval:    string(key.Interval),
		Year:        key.Year,
		Month:       key.Month,
		Fingerprint: snap.Fingerprint,
		Rows:        rows,
	}, nil
}
