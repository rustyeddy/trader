package backtest

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/broker"
)

// CSVTicksFeed reads canonical tick CSV rows:
//
//	time,instrument,bid,ask[,event...]
//
// where time is RFC3339 or RFC3339Nano.
//
// It optionally filters ticks to [From, To) if provided.
// Header row ("time,...") is allowed.
// Empty/short rows are skipped.
type CSVTicksFeed struct {
	f    *os.File
	r    *csv.Reader
	from time.Time
	to   time.Time

	sawFirst bool
}

func NewCSVTicksFeed(path string, from, to time.Time) (*CSVTicksFeed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	return &CSVTicksFeed{f: f, r: r, from: from, to: to}, nil
}

func (f *CSVTicksFeed) Close() error {
	if f.f != nil {
		return f.f.Close()
	}
	return nil
}

func (f *CSVTicksFeed) Next() (broker.Price, bool, error) {
	for {
		row, err := f.r.Read()
		if err == io.EOF {
			return broker.Price{}, false, nil
		}
		if err != nil {
			return broker.Price{}, false, err
		}
		if len(row) == 0 {
			continue
		}

		// Allow a single header row
		if !f.sawFirst {
			f.sawFirst = true
			if len(row) > 0 && strings.EqualFold(strings.TrimSpace(row[0]), "time") {
				continue
			}
		}

		p, ok, err := parseTickRow(row)
		if err != nil {
			return broker.Price{}, false, err
		}
		if !ok {
			continue
		}
		if !inRange(p.Time, f.from, f.to) {
			continue
		}
		return p, true, nil
	}
}

func parseTickRow(row []string) (broker.Price, bool, error) {
	// Need at least: time,instrument,bid,ask
	if len(row) < 4 {
		return broker.Price{}, false, nil
	}

	ts := strings.TrimSpace(row[0])
	if ts == "" {
		return broker.Price{}, false, nil
	}
	// Accept RFC3339 or RFC3339Nano.
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339Nano, ts)
		if err2 != nil {
			return broker.Price{}, false, fmt.Errorf("bad time %q: %w", ts, err)
		}
		t = t2
	}

	inst := strings.TrimSpace(row[1])
	if inst == "" {
		return broker.Price{}, false, nil
	}

	bid, err := strconv.ParseFloat(strings.TrimSpace(row[2]), 64)
	if err != nil {
		return broker.Price{}, false, fmt.Errorf("bad bid %q: %w", row[2], err)
	}
	ask, err := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
	if err != nil {
		return broker.Price{}, false, fmt.Errorf("bad ask %q: %w", row[3], err)
	}

	return broker.Price{Time: t, Instrument: inst, Bid: bid, Ask: ask}, true, nil
}

func inRange(t, from, to time.Time) bool {
	if !from.IsZero() && t.Before(from) {
		return false
	}
	if !to.IsZero() && !t.Before(to) {
		return false
	}
	return true
}
