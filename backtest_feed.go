package trader

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
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
	from Timestamp
	to   Timestamp

	sawFirst bool
}

func NewCSVTicksFeed(path string, from, to Timestamp) (*CSVTicksFeed, error) {
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

func (f *CSVTicksFeed) Next() (Tick, bool, error) {
	for {
		row, err := f.r.Read()
		if err == io.EOF {
			return Tick{}, false, nil
		}
		if err != nil {
			return Tick{}, false, err
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
			return Tick{}, false, err
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

func parseTickRow(row []string) (Tick, bool, error) {
	// Need at least: time,instrument,bid,ask
	if len(row) < 4 {
		return Tick{}, false, nil
	}

	ts := strings.TrimSpace(row[0])
	if ts == "" {
		return Tick{}, false, nil
	}
	// Accept RFC3339 or RFC3339Nano.
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339Nano, ts)
		if err2 != nil {
			return Tick{}, false, fmt.Errorf("bad time %q: %w", ts, err)
		}
		t = t2
	}

	tstamp := Timestamp(t.Unix())
	inst := strings.TrimSpace(row[1])
	if inst == "" {
		return Tick{}, false, nil
	}

	bid, err := strconv.ParseFloat(strings.TrimSpace(row[2]), 64)
	if err != nil {
		return Tick{}, false, fmt.Errorf("bad bid %q: %w", row[2], err)
	}
	ask, err := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
	if err != nil {
		return Tick{}, false, fmt.Errorf("bad ask %q: %w", row[3], err)
	}

	return Tick{
		Timestamp:  tstamp,
		Instrument: inst,
		BA: BA{
			Bid: PriceFromFloat(bid),
			Ask: PriceFromFloat(ask),
		},
	}, true, nil
}

func inRange(t, from, to Timestamp) bool {
	if !from.IsZero() && t < from {
		return false
	}
	if !to.IsZero() && t >= to {
		return false
	}
	return true
}
