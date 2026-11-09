package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type price int64

const priceScale = 100000

func (p price) Float64() float64 {
	return float64(p) / priceScale
}

type ohlc struct {
	o, h, l, c price
	v          int
}

// bar is one observation: an instrument's OHLCV for one interval. Time is when
// the interval opened. availableAt is when the bar is complete, the earliest a
// consumer may see it; whoever builds the bar sets it, and consumers only read it.
type bar struct {
	instrument string
	time.Time
	ohlc
	availableAt time.Time
}

type marketData struct {
	instrument string
	interval   string

	data []bar
}

// bars returns an iterator over the bars in order. If ctx is cancelled the
// iterator yields ctx.Err() once and stops, even after the last bar, so a
// consumer that reads ahead (frames) still learns of it before publishing what
// it holds. No goroutine is involved, so breaking out of the loop early leaks
// nothing.
func (md *marketData) bars(ctx context.Context) iter.Seq2[bar, error] {
	return func(yield func(bar, error) bool) {
		for _, b := range md.data {
			if err := ctx.Err(); err != nil {
				yield(bar{}, err)
				return
			}
			if !yield(b, nil) {
				return
			}
		}
		if err := ctx.Err(); err != nil {
			yield(bar{}, err)
		}
	}
}

// mergedBars delivers the bars of several instruments as one sequence, each bar
// once it is available: by availableAt, then by instrument name, so the order
// does not depend on the order of sets. A bar that opened first can arrive later
// when its interval is longer.
//
// A missing bar is simply not delivered: nothing is synthesized or carried
// forward. Sets must have different instruments and strictly increasing
// availability; anything else is reported as the sequence's only item.
func mergedBars(ctx context.Context, sets ...*marketData) iter.Seq2[bar, error] {
	merged, err := mergeSets(sets)
	if err != nil {
		return func(yield func(bar, error) bool) { yield(bar{}, err) }
	}
	return (&marketData{data: merged}).bars(ctx)
}

func mergeSets(sets []*marketData) ([]bar, error) {
	seen := make(map[string]bool, len(sets))
	var merged []bar
	for _, md := range sets {
		if seen[md.instrument] {
			return nil, fmt.Errorf("merge bars: instrument %q given twice", md.instrument)
		}
		seen[md.instrument] = true

		for i, b := range md.data {
			if b.availableAt.IsZero() {
				return nil, fmt.Errorf("merge bars: %s bar at %s has no availability", b.instrument, b.Format(barTimeLayout))
			}
			if i > 0 && !b.availableAt.After(md.data[i-1].availableAt) {
				return nil, fmt.Errorf("merge bars: %s bar at %s is not available after the one before it", b.instrument, b.Format(barTimeLayout))
			}
		}
		merged = append(merged, md.data...)
	}
	slices.SortFunc(merged, func(a, b bar) int {
		return cmp.Or(a.availableAt.Compare(b.availableAt), cmp.Compare(a.instrument, b.instrument))
	})
	return merged, nil
}

// frame is the bars that became available at one moment, one per instrument, in
// instrument order. It holds only fresh bars: an instrument with no bar at that
// moment is absent, never represented by an older value.
type frame struct {
	availableAt time.Time
	bars        []bar
}

// frames groups the sequence from mergedBars by availableAt. In a replay a frame
// is complete when the next bar is later, so it is delivered then, or when the
// sequence ends. On an error the frame still being built is dropped, since it
// may be incomplete.
func frames(bars iter.Seq2[bar, error]) iter.Seq2[frame, error] {
	return func(yield func(frame, error) bool) {
		var pending frame
		for b, err := range bars {
			if err != nil {
				yield(frame{}, err)
				return
			}
			if len(pending.bars) > 0 && !b.availableAt.Equal(pending.availableAt) {
				if !yield(pending, nil) {
					return
				}
				pending = frame{}
			}
			pending.availableAt = b.availableAt
			pending.bars = append(pending.bars, b)
		}
		if len(pending.bars) > 0 {
			yield(pending, nil)
		}
	}
}

const (
	barSchema     = "design-bars-v1"
	barTimeLayout = "2006-01-02T15:04:05Z" // always UTC
)

var barColumns = []string{"time", "open", "high", "low", "close", "volume"}

// barDurations gives how long a bar of each supported interval lasts. Weekly and
// session-based bars are absent on purpose: their end is not open plus a fixed
// duration.
var barDurations = map[string]time.Duration{
	"h1": time.Hour,
	"d1": 24 * time.Hour,
}

// loadBars reads the bars in the file at path; see readBars.
func loadBars(path string) (*marketData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read only: nothing to lose

	md, err := readBars(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return md, nil
}

// readBars parses a design-bars-v1 file: a "# schema=..." header naming the
// instrument and interval, the column names, then one row per bar. It refuses
// anything malformed rather than skipping it, including bars whose times do not
// strictly increase, since the broker fills an order only on a later bar.
func readBars(r io.Reader) (*marketData, error) {
	br := bufio.NewReader(r)
	header, err := br.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("bars header: %w", err)
	}
	fields, ok := strings.CutPrefix(header, "# ")
	if !ok {
		return nil, fmt.Errorf("bars header %q: want a comment", header)
	}
	meta := make(map[string]string)
	for _, f := range strings.Fields(fields) {
		k, v, _ := strings.Cut(f, "=")
		meta[k] = v
	}
	if meta["schema"] != barSchema {
		return nil, fmt.Errorf("bars schema %q: want %s", meta["schema"], barSchema)
	}
	if meta["instrument"] == "" || meta["tf"] == "" {
		return nil, errors.New("bars header needs an instrument and a tf")
	}
	lasts, ok := barDurations[meta["tf"]]
	if !ok {
		return nil, fmt.Errorf("bars header: unsupported tf %q", meta["tf"])
	}

	cr := csv.NewReader(br)
	cols, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("bars columns: %w", err)
	}
	// Compare fields, not joined text: a quoted comma would hide a missing column
	// and let shorter rows through to parseBar.
	if !slices.Equal(cols, barColumns) {
		return nil, fmt.Errorf("bars columns %q: want %q", cols, barColumns)
	}

	md := &marketData{instrument: meta["instrument"], interval: meta["tf"]}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bars: %w", err)
		}
		line, _ := cr.FieldPos(0)
		b, err := parseBar(md.instrument, rec, lasts)
		if err != nil {
			return nil, fmt.Errorf("bars line %d: %w", line, err)
		}
		if n := len(md.data); n > 0 && !b.After(md.data[n-1].Time) {
			return nil, fmt.Errorf("bars line %d: time %s is not after %s", line, rec[0], md.data[n-1].Format(barTimeLayout))
		}
		md.data = append(md.data, b)
	}
	if len(md.data) == 0 {
		return nil, errors.New("bars: no rows")
	}
	return md, nil
}

// parseBar parses one row, checked against barColumns by the caller. A bar that
// lasts the given duration is available when its interval ends. The file does
// not store that time: this is where the prototype builds a canonical bar, the
// counterpart of production's raw-to-canonical conversion, which owns the rule.
func parseBar(instrument string, rec []string, lasts time.Duration) (bar, error) {
	t, err := time.Parse(barTimeLayout, rec[0])
	if err != nil {
		return bar{}, err
	}
	var px [4]price
	for i := range px {
		if px[i], err = parsePrice(rec[i+1]); err != nil {
			return bar{}, err
		}
	}
	v, err := strconv.Atoi(rec[5])
	if err != nil {
		return bar{}, fmt.Errorf("volume: %w", err)
	}
	if v < 0 {
		return bar{}, fmt.Errorf("volume %d is negative", v)
	}
	o, h, l, c := px[0], px[1], px[2], px[3]
	if h < max(o, c) || l > min(o, c) {
		return bar{}, fmt.Errorf("high %d and low %d do not contain open %d and close %d", h, l, o, c)
	}
	return bar{instrument: instrument, Time: t, ohlc: ohlc{o: o, h: h, l: l, c: c, v: v}, availableAt: t.Add(lasts)}, nil
}

// parsePrice reads a positive price written with exactly five decimals, the
// resolution of priceScale, into points. It works on the digits, never through a
// float, so 1.10323 is exactly 110323.
func parsePrice(s string) (price, error) {
	whole, frac, ok := strings.Cut(s, ".")
	if !ok || whole == "" || len(frac) != 5 {
		return 0, fmt.Errorf("price %q: want five decimals", s)
	}
	n, err := strconv.ParseUint(whole+frac, 10, 63) // unsigned: refuses any sign
	if err != nil {
		return 0, fmt.Errorf("price %q: %w", s, err)
	}
	if n == 0 {
		return 0, fmt.Errorf("price %q: must be positive", s)
	}
	if n > uint64(maxPrice) {
		return 0, fmt.Errorf("price %q: above the maximum %d", s, maxPrice)
	}
	return price(n), nil
}
