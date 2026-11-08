package main

import (
	"bufio"
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

// bar is one observation: an instrument's OHLCV at a point in time.
type bar struct {
	instrument string
	time.Time
	ohlc
}

type marketData struct {
	instrument string
	interval   string

	data []bar
}

// bars returns an iterator over the bars in order. If ctx is cancelled the
// iterator yields ctx.Err() once and stops. No goroutine is involved, so
// breaking out of the loop early leaks nothing.
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
	}
}

const (
	barSchema     = "design-bars-v1"
	barTimeLayout = "2006-01-02T15:04:05Z" // always UTC
)

var barColumns = []string{"time", "open", "high", "low", "close", "volume"}

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
		b, err := parseBar(md.instrument, rec)
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

// parseBar parses one row, checked against barColumns by the caller.
func parseBar(instrument string, rec []string) (bar, error) {
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
	return bar{instrument: instrument, Time: t, ohlc: ohlc{o: o, h: h, l: l, c: c, v: v}}, nil
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
