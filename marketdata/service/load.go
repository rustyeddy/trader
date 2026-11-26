package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/canonical"
)

// ErrIncomplete reports that a range cannot be loaded as asked: a month is stale,
// or missing or bad and not allowed, or bars are missing inside the range that the
// market calendar does not explain and the request does not allow. The error is
// an *IncompleteError, which carries the offending months or gaps; the LoadResult
// returned with it still has every month's status, so a caller can show the whole
// picture.
var ErrIncomplete = errors.New("service: series not loadable")

// ErrInvalidRequest reports a LoadRequest that cannot be run.
var ErrInvalidRequest = errors.New("service: invalid request")

// ErrMixedSeries reports months of one range that are not one series: they differ
// in schema or in what a trade file says about its data (snapshot, currency,
// adjustment, calendar and the rest). No request option allows it (ADR-095);
// rebuild the series from one snapshot.
var ErrMixedSeries = errors.New("service: the months are not one series")

// DataInfo is what the files of a trade-price series say about their data
// (ADR-095). Every month of a loaded series shares it.
type DataInfo struct {
	AvailabilityPolicy string
	Calendar           string
	// PriceBasis is "trade": the bars have no bid, ask or midpoint.
	PriceBasis string
	Currency   string
	VolumeUnit string
	// Adjustment says how past prices were rewritten for corporate actions, and
	// AdjustmentEvidence how that is known ("observed" or "documented").
	Adjustment         string
	AdjustmentEvidence string
	// SnapshotFingerprint and SnapshotAsOf identify the one input the series was
	// built from and the date it was obtained.
	SnapshotFingerprint string
	SnapshotAsOf        string
}

// MonthState is what one month's canonical file came to.
type MonthState string

// The month states (ADR-090).
const (
	// Loaded: a valid file from the build in force with no bad records.
	Loaded MonthState = "loaded"
	// Bad: the file has bad records (bad_count > 0). Its good bars are not
	// complete data, so the month is refused unless the caller allows known-bad
	// months.
	Bad MonthState = "bad"
	// Missing: there is no canonical file for the month. Refused unless the
	// caller allows a gap.
	Missing MonthState = "missing"
	// Stale: the file is not from the build in force (another availability policy,
	// builder or validator version) or is not readable as a canonical file. It is
	// reported and never used: rebuild it.
	Stale MonthState = "stale"
)

// LoadRequest asks for one instrument's bars at one interval whose opening time is
// in [From, To), from the canonical files.
type LoadRequest struct {
	Source, Instrument, Interval string
	// From and To bound the range by opening time, UTC; To is exclusive.
	From, To time.Time
	// AllowBad accepts months that have bad records: their good bars are returned
	// and the month is still reported as Bad. AllowMissing accepts months with no
	// file: the series has a gap there. AllowMissing also accepts interior gaps
	// the calendar does not explain, whether a missing month or bars missing from
	// a month that loaded. Stale months are never accepted.
	AllowBad, AllowMissing bool
	// SkipStale lets a load carry on past a Stale month: its bars are never used (a
	// stale file is not from the build in force) but it no longer fails the load; it
	// is still reported as Stale in the months and its bars are a gap in the series.
	// For a reader that must serve what it can and say what it could not.
	SkipStale bool
}

// MonthStatus is what one UTC month of the range came to.
type MonthStatus struct {
	Year  int
	Month time.Month
	State MonthState
	// Path is the canonical file's path relative to the data root.
	Path string
	// Good and Bad count the bars and the kept bad records of a file that was read.
	Good, Bad int
	// Reason says why a month is stale or bad.
	Reason string
}

// GapKind says whether the market calendar explains a gap in a series.
type GapKind string

// The gap kinds. A weekend gap is neither: every week has one, so it is not
// reported.
const (
	// KnownClosure: the gap lies inside a year-end holiday window. Reported,
	// never an error.
	KnownClosure GapKind = "closure"
	// KnownHole: the provider does not have these bars and never will; the run is
	// on the reviewed list of known provider holes. Reported, never an error.
	KnownHole GapKind = "known_hole"
	// Unexplained: bars are missing that neither the calendar nor the list of known
	// provider holes explains. An error unless the request allows missing data.
	Unexplained GapKind = "unexplained"
)

// Gap is a run of missing bars between two bars of a series. A series is only
// judged between its first and last bar: a last bar that is absent may be a candle
// still forming, which a download never stores (ADR-090).
type Gap struct {
	Kind GapKind
	// Start is when the first missing bar opens; End is when the bar after the last
	// missing one would open, so the gap is [Start, End), UTC.
	Start, End time.Time
	// Bars counts the missing bars.
	Bars int
}

func (g Gap) String() string {
	return fmt.Sprintf("%s %s %s, %d bars", g.Kind, g.Start.UTC().Format("2006-01-02T15:04Z"), g.End.UTC().Format("2006-01-02T15:04Z"), g.Bars)
}

// LoadResult is the loaded series, the status of every month the range touches, in
// order, and the interior gaps of the series: holiday closures and unexplained
// ones, never the weekly close. When Load fails with ErrIncomplete the Series is
// empty and Months is still complete; Gaps is complete when the months were
// loadable and the gaps are what failed it.
type LoadResult struct {
	Series marketdata.Series
	Months []MonthStatus
	Gaps   []Gap
	// Data describes a trade-price series, from its files; nil for quote data and
	// when no file was read. It is set on a failure after the files agreed.
	Data *DataInfo
}

// IncompleteError is the ErrIncomplete error: the months or the gaps that stopped
// the load.
type IncompleteError struct {
	Instrument string
	Months     []MonthStatus
	Gaps       []Gap
}

// maxGapsShown bounds how many gaps an error message lists.
const maxGapsShown = 5

func (e *IncompleteError) Error() string {
	parts := make([]string, 0, len(e.Months)+len(e.Gaps))
	for _, m := range e.Months {
		parts = append(parts, fmt.Sprintf("%04d-%02d %s", m.Year, int(m.Month), m.State))
	}
	for i, g := range e.Gaps {
		if i == maxGapsShown {
			parts = append(parts, fmt.Sprintf("and %d more gaps", len(e.Gaps)-maxGapsShown))
			break
		}
		parts = append(parts, "gap "+g.String())
	}
	return fmt.Sprintf("%s: %s: %s", ErrIncomplete, e.Instrument, strings.Join(parts, ", "))
}

// Is makes errors.Is(err, ErrIncomplete) true.
func (e *IncompleteError) Is(target error) bool { return target == ErrIncomplete }

// Load reads the canonical files for the months the range touches and returns the
// bars as one Series, in time order, cut to [From, To) by opening time. Each
// month is classified (Loaded, Bad, Missing, Stale). The default is strict: a
// Stale month always fails the load, and a Missing or Bad month fails it unless
// the request allows it (SkipStale lets the load carry on past a Stale month, whose
// bars are still never used); the failure is an *IncompleteError naming the months, and
// the result still carries every month's status. A bar's AvailableAt is the one
// stored in its file, never recomputed. Only a Loaded month, and a Bad month when
// allowed, contribute bars.
//
// All the files read must be of one series (ErrMixedSeries): one schema, and for
// trade data one snapshot with one description, which is returned as Data.
//
// A trade-price series (canonical-trade-v1) must have every session its calendar
// lists between its first and last bar (canonical.USEquityGaps): weekends and
// listed closures are not gaps and are not reported, and a missing session is
// Unexplained.
//
// Between its first and last bar a quote series must have every bar the FX market calendar
// expects (canonical.ClosureAt): a missing interior bar is a gap, and an
// Unexplained one fails the load like a missing month unless the request allows
// missing data. Weekends are not reported, year-end holidays are reported as
// KnownClosure, and a run on the reviewed list of known provider holes (bars the
// provider does not have) is reported as KnownHole and is not an error. The ends of the range are not judged. Each month's file is
// validated as it is read (canonical.File.Validate), and months are read in order
// and hold only bars of their own month, so the series is in ascending order by
// time and availability without a second pass over every bar.
func Load(ctx context.Context, req LoadRequest) (LoadResult, error) {
	if err := ctx.Err(); err != nil {
		return LoadResult{}, err
	}
	interval := canonical.Interval(req.Interval)
	switch {
	case req.Source == "" || req.Instrument == "":
		return LoadResult{}, fmt.Errorf("%w: a source and an instrument are required", ErrInvalidRequest)
	case !interval.Valid():
		return LoadResult{}, fmt.Errorf("%w: %q (h1 and d1 only)", ErrUnsupportedInterval, req.Interval)
	case !req.To.After(req.From):
		return LoadResult{}, fmt.Errorf("%w: the end must be after the start", ErrInvalidRequest)
	}
	from, to := req.From.UTC(), req.To.UTC()

	result := LoadResult{Series: marketdata.Series{Source: req.Source, Instrument: req.Instrument, Interval: req.Interval}}
	var bars []marketdata.Bar
	var stopped []MonthStatus
	var first *canonical.File // the first file read: the series the others must be
	for ms := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); ms.Before(to); ms = ms.AddDate(0, 1, 0) {
		if err := ctx.Err(); err != nil {
			return LoadResult{}, err
		}
		st, f, err := loadMonth(ctx, req, interval, ms)
		if err != nil {
			return LoadResult{}, err
		}
		result.Months = append(result.Months, st)
		if f.Meta.AvailabilityPolicy != "" { // a file was read
			if first == nil {
				read := f
				first, result.Data = &read, dataInfo(f.Meta)
			} else if diff := seriesDifference(first.Meta, f.Meta); diff != "" {
				return LoadResult{}, fmt.Errorf("%w: %s %04d-%02d and %04d-%02d differ in %s", ErrMixedSeries, req.Instrument,
					first.Key.Year, int(first.Key.Month), ms.Year(), int(ms.Month()), diff)
			}
		}
		usable := st.State == Loaded || (st.State == Bad && req.AllowBad)
		gapAllowed := (st.State == Missing && req.AllowMissing) || (st.State == Stale && req.SkipStale)
		if !usable && !gapAllowed {
			stopped = append(stopped, st)
		}
		if !usable {
			continue
		}
		for _, b := range f.Bars {
			if !b.Time.Before(from) && b.Time.Before(to) {
				bars = append(bars, b)
			}
		}
	}
	if len(stopped) > 0 {
		return result, &IncompleteError{Instrument: req.Instrument, Months: stopped}
	}
	var gaps []canonical.Gap
	var err error
	if result.Data != nil {
		gaps, err = canonical.USEquityGaps(bars)
	} else {
		gaps, err = canonical.GapsWith(interval, bars, func(open time.Time) (bool, error) {
			return canonical.InKnownHole(req.Source, req.Instrument, interval, open)
		})
	}
	if err != nil {
		return LoadResult{Months: result.Months, Data: result.Data}, err
	}
	var unexplained []Gap
	for _, g := range gaps {
		kind := KnownClosure
		switch g.Closure {
		case canonical.Open:
			kind = Unexplained
		case canonical.KnownHole:
			kind = KnownHole
		}
		gap := Gap{Kind: kind, Start: g.Start.UTC(), End: g.End.UTC(), Bars: g.Bars}
		result.Gaps = append(result.Gaps, gap)
		if kind == Unexplained && !req.AllowMissing {
			unexplained = append(unexplained, gap)
		}
	}
	if len(unexplained) > 0 {
		return result, &IncompleteError{Instrument: req.Instrument, Gaps: unexplained}
	}
	result.Series.Bars = bars
	return result, nil
}

// dataInfo describes a trade file's data, or nil for a quote file.
func dataInfo(m canonical.Meta) *DataInfo {
	t := m.Trade
	if t == nil {
		return nil
	}
	return &DataInfo{
		AvailabilityPolicy: m.AvailabilityPolicy, Calendar: t.Calendar, PriceBasis: t.PriceBasis, Currency: t.Currency,
		VolumeUnit: t.VolumeUnit, Adjustment: string(t.Adjustment), AdjustmentEvidence: string(t.AdjustmentEvidence),
		SnapshotFingerprint: t.SnapshotFingerprint, SnapshotAsOf: t.SnapshotAsOf,
	}
}

// seriesDifference names what makes two files parts of different series, or ""
// when they can be joined.
func seriesDifference(a, b canonical.Meta) string {
	if (a.Trade == nil) != (b.Trade == nil) {
		return "schema"
	}
	if a.Trade == nil {
		return ""
	}
	x, y := *a.Trade, *b.Trade
	switch {
	case x.SnapshotFingerprint != y.SnapshotFingerprint:
		return "snapshot"
	case x.Currency != y.Currency:
		return "currency"
	case x.Adjustment != y.Adjustment:
		return "adjustment"
	case x.Calendar != y.Calendar:
		return "calendar"
	case x != y:
		return "data description"
	}
	return ""
}

// loadMonth reads and classifies one month. The file is returned whenever one was
// read, usable or not. Only a failure to read that is not about the file's content
// (an I/O error, cancellation) is an error.
func loadMonth(ctx context.Context, req LoadRequest, interval canonical.Interval, ms time.Time) (MonthStatus, canonical.File, error) {
	key := canonical.Key{Source: req.Source, Symbol: req.Instrument, Interval: interval, Year: ms.Year(), Month: ms.Month()}
	st := MonthStatus{Year: ms.Year(), Month: ms.Month()}
	path, err := key.Path()
	if err != nil {
		return st, canonical.File{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	st.Path = path
	f, err := canonical.Read(ctx, key)
	switch {
	case errors.Is(err, canonical.ErrNotFound):
		st.State = Missing
		return st, canonical.File{}, nil
	case errors.Is(err, canonical.ErrMalformedData):
		schema := canonical.Schema
		if req.Source == "stooq" {
			schema = canonical.SchemaTrade
		}
		st.State, st.Reason = Stale, "not readable as "+schema+": "+firstLine(err)
		return st, canonical.File{}, nil
	case err != nil:
		return st, canonical.File{}, err
	}
	st.Good, st.Bad = len(f.Bars), len(f.Bad)
	if reason := staleReason(f.Meta); reason != "" {
		st.State, st.Reason = Stale, reason
		return st, f, nil
	}
	if st.Bad > 0 {
		st.State, st.Reason = Bad, fmt.Sprintf("%d bad records", st.Bad)
		return st, f, nil
	}
	st.State = Loaded
	return st, f, nil
}

// staleReason says why a file's build is not the one in force for its schema, or
// "" when it is.
func staleReason(m canonical.Meta) string {
	policy, builder, validator := canonical.PolicyScheduledCloseV1, BuilderVersion, ValidatorVersion
	if m.Trade != nil {
		policy, builder, validator = canonical.PolicyUSEquityCloseV2, TradeBuilderVersion, TradeValidatorVersion
	}
	switch {
	case m.AvailabilityPolicy != policy:
		return fmt.Sprintf("availability policy %q, in force %q", m.AvailabilityPolicy, policy)
	case m.BuilderVersion != builder:
		return fmt.Sprintf("builder %q, in force %q", m.BuilderVersion, builder)
	case m.ValidatorVersion != validator:
		return fmt.Sprintf("validator %q, in force %q", m.ValidatorVersion, validator)
	}
	return ""
}

// firstLine keeps an error to one line for a status reason.
func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return s
}
