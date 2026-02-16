package replay

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/pricing"
)

type EventRow struct {
	Tick  pricing.Tick
	Event string
	P1    string
	P2    string
	P3    string
	P4    string
}

type CSVEventsFeed struct {
	f    *os.File
	r    *csv.Reader
	from time.Time
	to   time.Time

	sawFirst bool
}

func NewCSVEventsFeed(path string, from, to time.Time) (*CSVEventsFeed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	return &CSVEventsFeed{f: f, r: r, from: from, to: to}, nil
}

func (f *CSVEventsFeed) Close() error {
	if f.f != nil {
		return f.f.Close()
	}
	return nil
}

// Expected columns:
// time,instrument,bid,ask,event,p1,p2,p3,p4
// Header allowed; missing event/params are treated as empty.
func (f *CSVEventsFeed) Next() (EventRow, bool, error) {
	for {
		row, err := f.r.Read()
		if err == io.EOF {
			return EventRow{}, false, nil
		}
		if err != nil {
			return EventRow{}, false, err
		}
		if len(row) == 0 {
			continue
		}

		if !f.sawFirst {
			f.sawFirst = true
			if len(row) > 0 && strings.EqualFold(strings.TrimSpace(row[0]), "time") {
				continue
			}
		}

		// Parse price via the shared backtest tick parser by reusing the first 4 cols.
		if len(row) < 4 {
			continue
		}

		p, ok, err := parseTickRowCompat(row)
		if err != nil {
			return EventRow{}, false, err
		}
		if !ok {
			continue
		}
		if !inRange(p.Time, f.from, f.to) {
			continue
		}

		ev := ""
		p1, p2, p3, p4 := "", "", "", ""
		if len(row) >= 5 {
			ev = strings.TrimSpace(row[4])
		}
		if len(row) >= 6 {
			p1 = strings.TrimSpace(row[5])
		}
		if len(row) >= 7 {
			p2 = strings.TrimSpace(row[6])
		}
		if len(row) >= 8 {
			p3 = strings.TrimSpace(row[7])
		}
		if len(row) >= 9 {
			p4 = strings.TrimSpace(row[8])
		}
		if len(row) > 9 {
			return EventRow{}, false, fmt.Errorf("too many columns (expected <=9): %v", row)
		}

		return EventRow{
			Tick:  p,
			Event: ev,
			P1:    p1,
			P2:    p2,
			P3:    p3,
			P4:    p4,
		}, true, nil
	}
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

// parseTickRowCompat duplicates minimal parsing soreplay doesn't import backtest.
// (Avoids internal package coupling.)
func parseTickRowCompat(row []string) (pricing.Tick, bool, error) {
	// time,instrument,bid,ask
	ts := strings.TrimSpace(row[0])
	if ts == "" {
		return pricing.Tick{}, false, nil
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339Nano, ts)
		if err2 != nil {
			return pricing.Tick{}, false, fmt.Errorf("bad time %q: %w", ts, err)
		}
		t = t2
	}

	inst := strings.TrimSpace(row[1])
	if inst == "" {
		return pricing.Tick{}, false, nil
	}

	// float parsing kept local to avoid import sprawl
	bid, err := parseFloat(row[2])
	if err != nil {
		return pricing.Tick{}, false, fmt.Errorf("bad bid %q: %w", row[2], err)
	}
	ask, err := parseFloat(row[3])
	if err != nil {
		return pricing.Tick{}, false, fmt.Errorf("bad ask %q: %w", row[3], err)
	}

	return pricing.Tick{Time: t, Instrument: inst, Bid: bid, Ask: ask}, true, nil
}

func parseFloat(s string) (float64, error) {
	// minimal, no locale; trims spaces
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}
