package backtest

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/market"
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
	from market.Timestamp
	to   market.Timestamp

	sawFirst bool
}

// NewCSVTicksFeed opens the CSV file at path and returns a feed that yields
// only ticks whose timestamp falls within [from, to). Pass zero Timestamps
// to disable filtering.
func NewCSVTicksFeed(path string, from, to market.Timestamp) (*CSVTicksFeed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	return &CSVTicksFeed{f: f, r: r, from: from, to: to}, nil
}

// Close releases the underlying file handle.
func (f *CSVTicksFeed) Close() error {
	if f.f != nil {
		return f.f.Close()
	}
	return nil
}

// Next advances the feed and returns the next in-range Tick.
// Returns (Tick{}, false, nil) at EOF and (Tick{}, false, err) on parse errors.
func (f *CSVTicksFeed) Next() (market.Tick, bool, error) {
	for {
		row, err := f.r.Read()
		if err == io.EOF {
			return market.Tick{}, false, nil
		}
		if err != nil {
			return market.Tick{}, false, err
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
			return market.Tick{}, false, err
		}
		if !ok {
			continue
		}
		if !inRange(p.Timestamp, f.from, f.to) {
			continue
		}
		return p, true, nil
	}
}

// parseTickRow parses one CSV row into a Tick. Returns (Tick{}, false, nil)
// for rows that are too short or have blank fields (silently skipped).
func parseTickRow(row []string) (market.Tick, bool, error) {
	// Need at least: time,instrument,bid,ask
	if len(row) < 4 {
		return market.Tick{}, false, nil
	}

	ts := strings.TrimSpace(row[0])
	if ts == "" {
		return market.Tick{}, false, nil
	}
	// Accept RFC3339 or RFC3339Nano.
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339Nano, ts)
		if err2 != nil {
			return market.Tick{}, false, fmt.Errorf("bad time %q: %w", ts, err)
		}
		t = t2
	}

	tstamp := market.Timestamp(t.Unix())
	inst := strings.TrimSpace(row[1])
	if inst == "" {
		return market.Tick{}, false, nil
	}

	bid, err := strconv.ParseFloat(strings.TrimSpace(row[2]), 64)
	if err != nil {
		return market.Tick{}, false, fmt.Errorf("bad bid %q: %w", row[2], err)
	}
	ask, err := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
	if err != nil {
		return market.Tick{}, false, fmt.Errorf("bad ask %q: %w", row[3], err)
	}

	tick := market.Tick{
		Timestamp:  tstamp,
		Instrument: inst,
		BA: market.BA{
			Bid: market.PriceFromFloat(bid),
			Ask: market.PriceFromFloat(ask),
		},
	}
	if err := tick.Validate(); err != nil {
		return market.Tick{}, false, err
	}
	return tick, true, nil
}

// inRange reports whether t falls within [from, to). A zero from or to
// disables the corresponding bound.
func inRange(t, from, to market.Timestamp) bool {
	if !from.IsZero() && t < from {
		return false
	}
	if !to.IsZero() && t >= to {
		return false
	}
	return true
}
