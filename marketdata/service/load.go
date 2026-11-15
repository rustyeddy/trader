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
// or missing or bad and not allowed. The error is an *IncompleteError, which
// carries the offending months; the LoadResult returned with it still has every
// month's status, so a caller can show the whole picture.
var ErrIncomplete = errors.New("service: series not loadable")

// ErrInvalidRequest reports a LoadRequest that cannot be run.
var ErrInvalidRequest = errors.New("service: invalid request")

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
	// builder or validator version) or is not readable as canonical-v2. It is
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
	// file: the series has a gap there. Stale months are never accepted.
	AllowBad, AllowMissing bool
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

// LoadResult is the loaded series and the status of every month the range touches,
// in order. When Load fails with ErrIncomplete the Series is empty and Months is
// still complete.
type LoadResult struct {
	Series marketdata.Series
	Months []MonthStatus
}

// IncompleteError is the ErrIncomplete error: the months that stopped the load.
type IncompleteError struct {
	Instrument string
	Months     []MonthStatus
}

func (e *IncompleteError) Error() string {
	parts := make([]string, 0, len(e.Months))
	for _, m := range e.Months {
		parts = append(parts, fmt.Sprintf("%04d-%02d %s", m.Year, int(m.Month), m.State))
	}
	return fmt.Sprintf("%s: %s: %s", ErrIncomplete, e.Instrument, strings.Join(parts, ", "))
}

// Is makes errors.Is(err, ErrIncomplete) true.
func (e *IncompleteError) Is(target error) bool { return target == ErrIncomplete }

// Load reads the canonical files for the months the range touches and returns the
// bars as one Series, in time order, cut to [From, To) by opening time. Each
// month is classified (Loaded, Bad, Missing, Stale). The default is strict: a
// Stale month always fails the load, and a Missing or Bad month fails it unless
// the request allows it; the failure is an *IncompleteError naming the months, and
// the result still carries every month's status. A bar's AvailableAt is the one
// stored in its file, never recomputed. Only a Loaded month, and a Bad month when
// allowed, contribute bars.
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
	for ms := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); ms.Before(to); ms = ms.AddDate(0, 1, 0) {
		if err := ctx.Err(); err != nil {
			return LoadResult{}, err
		}
		st, f, err := loadMonth(ctx, req, interval, ms)
		if err != nil {
			return LoadResult{}, err
		}
		result.Months = append(result.Months, st)
		usable := st.State == Loaded || (st.State == Bad && req.AllowBad)
		gapAllowed := st.State == Missing && req.AllowMissing
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
	result.Series.Bars = bars
	if err := result.Series.Validate(); err != nil {
		return LoadResult{Months: result.Months}, err
	}
	return result, nil
}

// loadMonth reads and classifies one month. Only a failure to read that is not
// about the file's content (an I/O error, cancellation) is an error.
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
		st.State, st.Reason = Stale, "not readable as canonical-v2: "+firstLine(err)
		return st, canonical.File{}, nil
	case err != nil:
		return st, canonical.File{}, err
	}
	st.Good, st.Bad = len(f.Bars), len(f.Bad)
	if reason := staleReason(f.Meta); reason != "" {
		st.State, st.Reason = Stale, reason
		return st, canonical.File{}, nil
	}
	if st.Bad > 0 {
		st.State, st.Reason = Bad, fmt.Sprintf("%d bad records", st.Bad)
		return st, f, nil
	}
	st.State = Loaded
	return st, f, nil
}

// staleReason says why a file's build is not the one in force, or "" when it is.
func staleReason(m canonical.Meta) string {
	switch {
	case m.AvailabilityPolicy != canonical.PolicyScheduledCloseV1:
		return fmt.Sprintf("availability policy %q, in force %q", m.AvailabilityPolicy, canonical.PolicyScheduledCloseV1)
	case m.BuilderVersion != BuilderVersion:
		return fmt.Sprintf("builder %q, in force %q", m.BuilderVersion, BuilderVersion)
	case m.ValidatorVersion != ValidatorVersion:
		return fmt.Sprintf("validator %q, in force %q", m.ValidatorVersion, ValidatorVersion)
	}
	return ""
}

// firstLine keeps an error to one line for a status reason.
func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return s
}
