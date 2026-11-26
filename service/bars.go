package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	mdservice "github.com/rustyeddy/trader/marketdata/service"
)

// The bounds of one page of bars.
const (
	// DefaultBarsLimit is the page size when a request names none.
	DefaultBarsLimit = 1000
	// MaxBarsLimit is the largest page a request may ask for.
	MaxBarsLimit = 10000
)

// Errors of ReadBars, classifiable with errors.Is.
var (
	// ErrInvalidBarsRequest reports a request that cannot be run: an unknown source,
	// an invalid instrument or interval, a range that is empty, a page size out of
	// bounds, or a position outside the range. It is the caller's to fix.
	ErrInvalidBarsRequest = errors.New("service: invalid bars request")
	// ErrNoData reports that no month of the range has a canonical file at all. The
	// page returned with it is empty and its coverage says so.
	ErrNoData = errors.New("service: no data for the range")
	// ErrBarsIncomplete reports, for a strict request only, that the range is not
	// complete: a month is bad, missing or stale, or bars are missing that neither
	// the calendar nor the list of known provider holes explains. The page returned
	// with it holds what could be read, and its coverage lists what is missing.
	ErrBarsIncomplete = errors.New("service: the bars are incomplete")
	// ErrBarsMixed reports months of the range that are not one series, such as two
	// snapshots of trade data. Nothing is served until the series is rebuilt.
	ErrBarsMixed = errors.New("service: the range mixes series")
)

// BarsRequest asks for one page of one instrument's canonical bars whose opening
// time is in [From, To), in ascending order of opening time.
type BarsRequest struct {
	// Source names where the bars come from: "oanda" (the default when empty) or
	// "stooq", which has only SPY d1.
	Source     string
	Instrument string
	// Interval is the lower-case token, h1 or d1.
	Interval string
	// From and To bound the range by opening time, UTC: From is inclusive and To is
	// exclusive.
	From, To time.Time
	// AsOf, when not zero, keeps only the bars a consumer could have used at that
	// instant (AvailableAt not after it), so a replay sees no look-ahead.
	AsOf time.Time
	// After, when not zero, continues a previous page: the opening time of its last
	// bar, which must lie in [From, To). The page starts with the first bar opening
	// after it.
	After time.Time
	// Limit is the page size: DefaultBarsLimit when zero, at most MaxBarsLimit.
	Limit int
	// Strict makes an incomplete range an error (ErrBarsIncomplete). The default
	// serves what is there and describes what is not in the page's Coverage.
	Strict bool
}

// CoverageSummary counts what the loaded range came to, from the start of the page
// (the start of the range for the first page) to the end of the range.
type CoverageSummary struct {
	Months, Loaded, Bad, Missing, Stale              int
	GapsUnexplained, GapsKnownHole, GapsKnownClosure int
}

// BarsCoverage says how complete the data is, so a partial result can never pass for
// a complete one.
type BarsCoverage struct {
	// Complete is true when no month is bad, missing or stale and no gap is
	// unexplained, over the range from the start of the page (the gap just before
	// the page's first bar included) to the end of the range: the first page
	// describes the whole range, a later page only what remains. Known closures and known provider holes are reported and still
	// complete.
	Complete bool
	Summary  CoverageSummary
	// Months are the months overlapping this page's window that are not loaded
	// (bad, missing, stale), with their state.
	Months []MonthStatus
	// Gaps are the runs of missing bars that start in this page's window, of every
	// kind (closure, known hole, unexplained): the rows that are missing.
	Gaps []Gap
}

// DataInfo describes a trade-price series, as the domain service defines it: its
// price basis, adjustment, snapshot and availability policy.
type DataInfo = mdservice.DataInfo

// BarsPage is one page of bars with the coverage of its window.
type BarsPage struct {
	Source, Instrument, Interval string
	From, To                     time.Time
	// AsOf is the request's AsOf, zero when none.
	AsOf time.Time
	Bars []marketdata.Bar
	// NextAfter is the opening time of this page's last bar when more bars follow
	// (pass it as After for the next page), and zero on the last page.
	NextAfter time.Time
	Coverage  BarsCoverage
	// Data describes a trade-price series; nil for quote data (oanda). Compare it
	// between pages: a rebuild between requests changes the snapshot.
	Data *DataInfo
}

// ReadBars returns one page of canonical bars for the request, with the coverage of
// the page's window. It reads only what the market-data domain service reads
// (canonical files; never a provider, never the network) and applies its policy
// without failing on what is missing: bad months contribute their good bars, missing
// and stale months contribute none (a stale month's bars are never used), and every
// such month and every gap is reported in the coverage. Strict turns an incomplete
// range into ErrBarsIncomplete, and a range with no canonical file at all is
// ErrNoData; both come with the page that was read. Pages are deterministic: the
// same request returns the same bars, and consecutive pages (After set to the
// previous page's NextAfter) neither repeat nor omit a bar. Nothing is written.
func ReadBars(ctx context.Context, log *slog.Logger, req BarsRequest) (BarsPage, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	source := req.Source
	if source == "" {
		source = "oanda"
	}
	switch {
	case source != "oanda" && source != "stooq":
		return BarsPage{}, fmt.Errorf("%w: source %q (oanda or stooq)", ErrInvalidBarsRequest, req.Source)
	case source == "stooq" && (req.Instrument != "SPY" || req.Interval != "d1"):
		return BarsPage{}, fmt.Errorf("%w: stooq has only SPY d1", ErrInvalidBarsRequest)
	case req.Limit < 0 || req.Limit > MaxBarsLimit:
		return BarsPage{}, fmt.Errorf("%w: limit %d is not between 1 and %d", ErrInvalidBarsRequest, req.Limit, MaxBarsLimit)
	case !req.To.After(req.From):
		return BarsPage{}, fmt.Errorf("%w: the end must be after the start", ErrInvalidBarsRequest)
	case !req.After.IsZero() && (req.After.Before(req.From) || !req.After.Before(req.To)):
		return BarsPage{}, fmt.Errorf("%w: the position is outside the range", ErrInvalidBarsRequest)
	}
	limit := req.Limit
	if limit == 0 {
		limit = DefaultBarsLimit
	}
	from, to := req.From.UTC(), req.To.UTC()
	start := from // the load also takes the bar the page continues from, to see a gap after it
	if !req.After.IsZero() {
		start = req.After.UTC()
	}

	got, err := mdservice.Load(ctx, mdservice.LoadRequest{Source: source, Instrument: req.Instrument, Interval: req.Interval,
		From: start, To: to, AllowBad: true, AllowMissing: true, SkipStale: true})
	if cerr := ctx.Err(); cerr != nil {
		return BarsPage{}, cerr
	}
	if err != nil {
		if errors.Is(err, mdservice.ErrInvalidRequest) || errors.Is(err, mdservice.ErrUnsupportedInterval) {
			return BarsPage{}, fmt.Errorf("%w: %w", ErrInvalidBarsRequest, err)
		}
		if errors.Is(err, mdservice.ErrMixedSeries) {
			return BarsPage{}, fmt.Errorf("%w: %w", ErrBarsMixed, err)
		}
		return BarsPage{}, err
	}

	bars := got.Series.Bars
	for len(bars) > 0 && !req.After.IsZero() && !bars[0].Time.After(req.After.UTC()) {
		bars = bars[1:] // the bar the page continues from
	}
	if !req.AsOf.IsZero() {
		// AvailableAt rises with the opening time, so what a consumer could not yet
		// use is a suffix.
		n := len(bars)
		for i, b := range bars {
			if b.AvailableAt.After(req.AsOf) {
				n = i
				break
			}
		}
		bars = bars[:n]
	}

	page := BarsPage{Source: source, Instrument: req.Instrument, Interval: req.Interval, From: from, To: to, AsOf: req.AsOf.UTC(), Data: got.Data}
	end := to // the window of this page is [lo, end)
	if len(bars) > limit {
		page.NextAfter = bars[limit-1].Time
		end = bars[limit].Time
		bars = bars[:limit]
	}
	page.Bars = append([]marketdata.Bar(nil), bars...)
	// A page's window starts where the page before it ended: at its own first bar,
	// since that page took everything up to the first bar of the next. A gap starting
	// between the two belongs to the earlier page, so no gap is listed twice or not
	// at all.
	lo := from
	switch {
	case len(page.Bars) > 0 && !req.After.IsZero():
		lo = page.Bars[0].Time
	case !req.After.IsZero():
		lo = req.After.UTC().Add(time.Nanosecond)
	}
	page.Coverage = coverageOf(got, lo, end)

	switch {
	case onlyMissingMonths(got.Months):
		return page, fmt.Errorf("%w: %s %s %s to %s", ErrNoData, req.Instrument, req.Interval, from.Format(time.RFC3339), to.Format(time.RFC3339))
	case req.Strict && !page.Coverage.Complete:
		return page, fmt.Errorf("%w: %s %s: %d bad, %d missing and %d stale months, %d unexplained gaps",
			ErrBarsIncomplete, req.Instrument, req.Interval, page.Coverage.Summary.Bad, page.Coverage.Summary.Missing,
			page.Coverage.Summary.Stale, page.Coverage.Summary.GapsUnexplained)
	}
	log.Info("bars read", "symbol", req.Instrument, "interval", req.Interval, "bars", len(page.Bars), "complete", page.Coverage.Complete)
	return page, nil
}

// onlyMissingMonths reports whether the range touched months and none had a file.
func onlyMissingMonths(months []MonthStatus) bool {
	for _, m := range months {
		if m.State != MonthMissing {
			return false
		}
	}
	return len(months) > 0
}

// coverageOf summarizes the whole load and lists what falls in the window [lo, end).
func coverageOf(got mdservice.LoadResult, lo, end time.Time) BarsCoverage {
	var c BarsCoverage
	c.Summary.Months = len(got.Months)
	for _, m := range got.Months {
		switch m.State {
		case MonthLoaded:
			c.Summary.Loaded++
		case MonthBad:
			c.Summary.Bad++
		case MonthMissing:
			c.Summary.Missing++
		case MonthStale:
			c.Summary.Stale++
		}
		if m.State == MonthLoaded {
			continue
		}
		ms := time.Date(m.Year, m.Month, 1, 0, 0, 0, 0, time.UTC)
		if ms.Before(end) && ms.AddDate(0, 1, 0).After(lo) {
			c.Months = append(c.Months, m)
		}
	}
	for _, g := range got.Gaps {
		switch g.Kind {
		case GapUnexplained:
			c.Summary.GapsUnexplained++
		case GapKnownHole:
			c.Summary.GapsKnownHole++
		default:
			c.Summary.GapsKnownClosure++
		}
		if !g.Start.Before(lo) && g.Start.Before(end) {
			c.Gaps = append(c.Gaps, g)
		}
	}
	c.Complete = c.Summary.Bad == 0 && c.Summary.Missing == 0 && c.Summary.Stale == 0 && c.Summary.GapsUnexplained == 0
	return c
}
