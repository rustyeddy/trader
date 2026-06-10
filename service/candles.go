package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader"
)

// CandlesCSVRequest describes a local candle CSV export request.
type CandlesCSVRequest struct {
	Instrument string
	Timeframe  string
	From       string
	To         string
	Source     string
}

// CandlesCSVResult contains canonical candle CSV plus request metadata.
type CandlesCSVResult struct {
	Instrument string
	Timeframe  string
	From       string
	To         string
	Source     string
	Count      int
	CSV        string
}

// CandlesCSV reads local candles and returns them in the canonical candle CSV
// format used by the store.
func (s *Service) CandlesCSV(ctx context.Context, req CandlesCSVRequest) (*CandlesCSVResult, error) {
	instrument := trader.NormalizeInstrument(strings.TrimSpace(req.Instrument))
	if instrument == "" {
		return nil, fmt.Errorf("instrument is required")
	}
	if strings.TrimSpace(req.From) == "" {
		return nil, fmt.Errorf("from is required")
	}
	timeframe := strings.TrimSpace(req.Timeframe)
	if timeframe == "" {
		return nil, fmt.Errorf("timeframe is required")
	}
	tf, err := trader.ParseTimeframe(timeframe)
	if err != nil {
		return nil, err
	}

	from, err := parseCandleDate(req.From)
	if err != nil {
		return nil, fmt.Errorf("bad from date %q: %w", req.From, err)
	}
	to, effectiveTo, err := candleToTime(req.To)
	if err != nil {
		return nil, err
	}
	if !from.Before(to) {
		return nil, fmt.Errorf("invalid date range: from %s must be before to %s", req.From, effectiveTo)
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = trader.SourceOanda
	}

	dm := trader.NewDataManager([]string{instrument}, from, to)
	iter, err := dm.Candles(ctx, trader.CandleRequest{
		Source:     source,
		Instrument: instrument,
		Range: trader.TimeRange{
			Start: trader.FromTime(from),
			End:   trader.FromTime(to),
			TF:    tf,
		},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	var buf bytes.Buffer
	count, err := WriteCandlesCSV(&buf, CandleCSVMetadata{
		Source:     source,
		Instrument: instrument,
		Timeframe:  tf.String(),
		Scale:      trader.PriceScale,
	}, iter)
	if err != nil {
		return nil, err
	}

	return &CandlesCSVResult{
		Instrument: instrument,
		Timeframe:  tf.String(),
		From:       from.Format("2006-01-02"),
		To:         effectiveTo,
		Source:     source,
		Count:      count,
		CSV:        buf.String(),
	}, nil
}

// CandleCSVMetadata contains fields for the canonical candle CSV metadata row.
type CandleCSVMetadata struct {
	Source     string
	Instrument string
	Timeframe  string
	Scale      trader.Scale6
}

// WriteCandlesCSV writes the canonical candle CSV format and returns the row
// count written.
func WriteCandlesCSV(buf *bytes.Buffer, meta CandleCSVMetadata, iter trader.CandleIterator) (int, error) {
	if iter == nil {
		return 0, fmt.Errorf("nil candle iterator")
	}
	if meta.Scale == 0 {
		meta.Scale = trader.PriceScale
	}
	if _, err := fmt.Fprintf(buf, "# schema=v1 source=%s instrument=%s tf=%s scale=%d\n",
		meta.Source, meta.Instrument, meta.Timeframe, meta.Scale); err != nil {
		return 0, err
	}

	w := csv.NewWriter(buf)
	if err := w.Write([]string{"Timestamp", "High", "Open", "Low", "Close", "avgspread", "maxspread", "ticks", "flags"}); err != nil {
		return 0, err
	}

	count := 0
	for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
		c := ct.Candle
		if err := w.Write([]string{
			strconv.FormatInt(int64(ct.Timestamp), 10),
			strconv.FormatInt(int64(c.High), 10),
			strconv.FormatInt(int64(c.Open), 10),
			strconv.FormatInt(int64(c.Low), 10),
			strconv.FormatInt(int64(c.Close), 10),
			strconv.FormatInt(int64(c.AvgSpread), 10),
			strconv.FormatInt(int64(c.MaxSpread), 10),
			strconv.FormatInt(int64(c.Ticks), 10),
			"0x0001",
		}); err != nil {
			return count, err
		}
		count++
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return count, err
	}
	if err := iter.Err(); err != nil {
		return count, err
	}
	return count, nil
}

func parseCandleDate(value string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(value), time.UTC)
}

func candleToTime(value string) (time.Time, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		now := time.Now().UTC()
		return now, now.Format(time.RFC3339), nil
	}
	toDate, err := parseCandleDate(value)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("bad to date %q: %w", value, err)
	}
	return toDate.AddDate(0, 0, 1), value, nil
}
