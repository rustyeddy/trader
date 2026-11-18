package canonical

import (
	"bufio"
	"bytes"
	_ "embed" // knownHolesCSV
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// knownHolesCSV is the reviewed list of runs of bars a provider does not have
// (known-provider-holes-v1). See the file's own header for what belongs in it.
//
//go:embed known_holes.csv
var knownHolesCSV []byte

// hole is one run [start, end) of missing bars of a series.
type hole struct{ start, end time.Time }

type holeKey struct {
	source, instrument string
	interval           Interval
}

// holes maps a series to its runs, sorted by start and not overlapping.
var holes = sync.OnceValues(func() (map[holeKey][]hole, error) { return parseHoles(knownHolesCSV) })

// parseHoles reads the list: comment lines starting with '#', one header line, then
// source,instrument,interval,start,end. It is strict, since a wrong entry would
// silently excuse a real gap: every field must be present, the interval supported,
// the times UTC and on the interval's boundary, start before end, lines sorted, and
// no run overlapping another of the same series.
func parseHoles(data []byte) (map[holeKey][]hole, error) {
	out := map[holeKey][]hole{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	var prev string
	sawHeader := false
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !sawHeader {
			if line != "source,instrument,interval,start,end" {
				return nil, fmt.Errorf("known holes line %d: want the header source,instrument,interval,start,end", n)
			}
			sawHeader = true
			continue
		}
		f := strings.Split(line, ",")
		if len(f) != 5 {
			return nil, fmt.Errorf("known holes line %d: want 5 fields, got %d", n, len(f))
		}
		for _, v := range f {
			if v == "" || v != strings.TrimSpace(v) {
				return nil, fmt.Errorf("known holes line %d: an empty or padded field", n)
			}
		}
		interval := Interval(f[2])
		if !interval.Valid() {
			return nil, fmt.Errorf("known holes line %d: interval %q is not h1 or d1", n, f[2])
		}
		start, err := parseTime(f[3])
		if err != nil {
			return nil, fmt.Errorf("known holes line %d: start: %w", n, err)
		}
		end, err := parseTime(f[4])
		if err != nil {
			return nil, fmt.Errorf("known holes line %d: end: %w", n, err)
		}
		if !end.After(start) {
			return nil, fmt.Errorf("known holes line %d: the end is not after the start", n)
		}
		for _, t := range []time.Time{start, end} {
			if ok, err := Aligned(interval, t); err != nil || !ok {
				return nil, fmt.Errorf("known holes line %d: %s is not on a %s boundary", n, t.Format(time.RFC3339), string(interval))
			}
		}
		if prev != "" && line <= prev {
			return nil, fmt.Errorf("known holes line %d: not sorted after the line before it", n)
		}
		prev = line
		k := holeKey{source: f[0], instrument: f[1], interval: interval}
		if rs := out[k]; len(rs) > 0 && start.Before(rs[len(rs)-1].end) {
			return nil, fmt.Errorf("known holes line %d: overlaps the run before it", n)
		}
		out[k] = append(out[k], hole{start, end})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !sawHeader {
		return nil, fmt.Errorf("known holes: no header")
	}
	return out, nil
}

// InKnownHole reports whether the bar of the series that would open at open lies in a
// run the provider is known not to have. The series is named by its source (for
// example "oanda"), instrument and interval; an unknown series has no holes. The
// list is embedded and checked when first used: a list that does not parse is a
// build defect, reported as an error here.
func InKnownHole(source, instrument string, i Interval, open time.Time) (bool, error) {
	m, err := holes()
	if err != nil {
		return false, fmt.Errorf("canonical: known holes: %w", err)
	}
	rs := m[holeKey{source: source, instrument: instrument, interval: i}]
	t := open.UTC()
	// The first run that ends after t is the only one that can contain it.
	k := sort.Search(len(rs), func(j int) bool { return rs[j].end.After(t) })
	return k < len(rs) && !t.Before(rs[k].start), nil
}
