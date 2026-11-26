package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/canonical"
)

// Versions of the trade-series conversion, with the meaning of BuilderVersion and
// ValidatorVersion. They are separate, so a change here leaves the quote files
// current.
const (
	TradeBuilderVersion   = "trade-builder-v1"
	TradeValidatorVersion = "trade-validator-v1"
)

var (
	// ErrUnsupportedSeries reports a series BuildSeries has no market rules for.
	ErrUnsupportedSeries = errors.New("service: unsupported series")
	// ErrSnapshotConflict reports a snapshot that cannot replace what is on disk:
	// canonical months exist that it does not cover, and a series is one snapshot.
	ErrSnapshotConflict = errors.New("service: the snapshot does not cover the series on disk")
	// ErrSnapshotDate reports a snapshot date that is missing, malformed or
	// earlier than the data.
	ErrSnapshotDate = errors.New("service: unusable snapshot date")
)

// seriesProfile is what is known about a trade-price series that its rows do not
// say. The builder records it in every file; a reader never infers it.
type seriesProfile struct {
	currency   string
	adjustment canonical.Adjustment
	evidence   canonical.Evidence
}

// seriesID names a series BuildSeries supports.
type seriesID struct{ source, instrument, interval string }

// profiles lists the supported series (ADR-095). Stooq's daily history is
// dividend-adjusted, as observed by comparison with unadjusted-for-dividends
// prices of each symbol (ADR-073, #728, #740).
var profiles = map[seriesID]seriesProfile{
	{"stooq", "QQQ", "d1"}: stooqTotalReturn,
	{"stooq", "SPY", "d1"}: stooqTotalReturn,
}

var stooqTotalReturn = seriesProfile{currency: "USD", adjustment: canonical.AdjustmentTotalReturn, evidence: canonical.EvidenceObserved}

// SupportsSeries reports whether BuildSeries has the market rules of a series.
func SupportsSeries(source, instrument, interval string) bool {
	_, ok := profiles[seriesID{source, instrument, interval}]
	return ok
}

// Snapshot is the whole history of one series from one input, as BuildSeries
// takes it.
type Snapshot struct {
	// Data is the exact input bytes. It is archived unchanged and its hash
	// identifies the series.
	Data []byte
	// AsOf is the date the bytes were obtained, YYYY-MM-DD. It is required the
	// first time the bytes are built; afterwards the recorded date stands and AsOf
	// may be empty.
	AsOf string
	// File is the input's file name; Member and ArchiveSHA256 describe the ZIP the
	// bytes were a member of, empty otherwise.
	File, Member, ArchiveSHA256 string
	// Partitions are the provider's reading of Data: one per month, in month
	// order.
	Partitions []marketdata.SourcePartition
}

// SeriesBuild is what one BuildSeries came to.
type SeriesBuild struct {
	// Fingerprint and AsOf identify the snapshot, as recorded in every file.
	Fingerprint, AsOf string
	// SnapshotPath is the archived input, relative to the data root.
	SnapshotPath string
	// Months are the partitions published, in order. After a failure they are the
	// ones that were published before it.
	Months []Result
}

// BuildSeries converts one snapshot of a trade-price daily series to
// canonical-trade-v1 files, all months at once (ADR-095): a series is one
// snapshot, so there is no building of a single month or range.
//
// Each row becomes a bar opening and available as its exchange session does
// (canonical.USEquitySession), or a bad record kept in its month's file with its
// reason and raw row: incomplete, non-session (a weekend or listed closure),
// invalid (unrepresentable, quoted prices where there should be none, or a bar
// that fails marketdata.Bar.Validate), duplicate or out of order, judged over the
// whole history. A date outside the calendar fails the build.
//
// Everything is converted and checked before anything is written. Then the bytes
// are archived with their record (never overwritten), and each month is published
// atomically, leaving alone those already built from the same input. A rebuild
// that stops part way leaves months of two snapshots, which Load refuses, until it
// is run again. A snapshot that lacks a month the series has on disk is refused
// (ErrSnapshotConflict) and nothing is written. builtAt is provenance only.
func BuildSeries(ctx context.Context, s Snapshot, builtAt time.Time) (SeriesBuild, error) {
	if err := ctx.Err(); err != nil {
		return SeriesBuild{}, err
	}
	id, profile, err := checkSnapshot(s)
	if err != nil {
		return SeriesBuild{}, err
	}
	fingerprint := canonical.Fingerprint(s.Data)
	asOf, archived, err := canonical.SnapshotDate(id.source, id.instrument, s.Data)
	if err != nil {
		return SeriesBuild{}, err
	}
	if !archived {
		asOf = s.AsOf
	}
	files, last, err := convertSeries(ctx, id, s.Partitions)
	if err != nil {
		return SeriesBuild{}, err
	}
	day, err := time.Parse(time.DateOnly, asOf)
	if err != nil {
		return SeriesBuild{}, fmt.Errorf("%w: %q: declare the date the input was obtained, as YYYY-MM-DD", ErrSnapshotDate, asOf)
	}
	if day.Before(last) {
		return SeriesBuild{}, fmt.Errorf("%w: %s is before the last row, dated %s", ErrSnapshotDate, asOf, last.Format(time.DateOnly))
	}
	meta := canonical.TradeMeta{
		PriceBasis: canonical.PriceBasisTrade, Currency: profile.currency, VolumeUnit: canonical.VolumeShares,
		Adjustment: profile.adjustment, AdjustmentEvidence: profile.evidence,
		SnapshotFingerprint: fingerprint, SnapshotAsOf: asOf, Calendar: canonical.CalendarUSEquityV2,
	}
	for i := range files {
		trade := meta
		files[i].Meta.Trade, files[i].Meta.BuiltAt = &trade, builtAt.UTC()
		if err := files[i].Validate(); err != nil {
			return SeriesBuild{}, err
		}
	}
	if err := covers(id, files); err != nil {
		return SeriesBuild{}, err
	}

	rec, path, err := canonical.ArchiveSnapshot(ctx, id.source, id.instrument, s.Data, canonical.SnapshotRecord{
		AsOf: asOf, File: s.File, Member: s.Member, ArchiveSHA256: s.ArchiveSHA256,
	})
	if err != nil {
		return SeriesBuild{}, err
	}
	if rec.AsOf != asOf {
		return SeriesBuild{}, fmt.Errorf("%w: another build recorded %s for these bytes; run the build again", ErrSnapshotDate, rec.AsOf)
	}
	out := SeriesBuild{Fingerprint: fingerprint, AsOf: asOf, SnapshotPath: path}
	for _, f := range files {
		outcome, err := canonical.Publish(ctx, f)
		if err != nil {
			return out, fmt.Errorf("%04d-%02d: %w", f.Key.Year, int(f.Key.Month), err)
		}
		rel, err := f.Key.Path()
		if err != nil {
			return out, err
		}
		out.Months = append(out.Months, Result{
			Source: id.source, Instrument: id.instrument, Interval: id.interval, Year: f.Key.Year, Month: f.Key.Month,
			Path: rel, Action: Action(outcome), Good: len(f.Bars), Bad: len(f.Bad),
		})
	}
	return out, nil
}

// checkSnapshot holds the shape rules of a Snapshot and finds its profile.
func checkSnapshot(s Snapshot) (seriesID, seriesProfile, error) {
	var none seriesID
	if len(s.Data) == 0 || len(s.Partitions) == 0 {
		return none, seriesProfile{}, fmt.Errorf("%w: a snapshot needs its bytes and its partitions", ErrInvalidRequest)
	}
	first := s.Partitions[0]
	id := seriesID{first.Source, first.Instrument, first.Interval}
	profile, ok := profiles[id]
	if !ok {
		return none, seriesProfile{}, fmt.Errorf("%w: %s %s %s", ErrUnsupportedSeries, id.source, id.instrument, id.interval)
	}
	var prev time.Time
	for _, p := range s.Partitions {
		if err := p.Validate(); err != nil {
			return none, seriesProfile{}, err
		}
		if (seriesID{p.Source, p.Instrument, p.Interval}) != id {
			return none, seriesProfile{}, fmt.Errorf("%w: partitions of more than one series", marketdata.ErrInvalidSource)
		}
		month := time.Date(p.Year, p.Month, 1, 0, 0, 0, 0, time.UTC)
		if !month.After(prev) {
			return none, seriesProfile{}, fmt.Errorf("%w: partitions must be one per month, in month order", marketdata.ErrInvalidSource)
		}
		prev = month
	}
	return id, profile, nil
}

// convertSeries classifies every row, in raw-file order across the months, and
// returns one file per partition (without TradeMeta and build time) and the
// latest date of any row.
func convertSeries(ctx context.Context, id seriesID, partitions []marketdata.SourcePartition) ([]canonical.File, time.Time, error) {
	type placed struct {
		row   marketdata.SourceRow
		month int // index into files
	}
	files := make([]canonical.File, len(partitions))
	var rows []placed
	for i, p := range partitions {
		files[i] = canonical.File{
			Key: canonical.Key{Source: id.source, Symbol: id.instrument, Interval: canonical.Interval(id.interval), Year: p.Year, Month: p.Month},
			Meta: canonical.Meta{RawFingerprint: p.Fingerprint, BuilderVersion: TradeBuilderVersion,
				ValidatorVersion: TradeValidatorVersion, AvailabilityPolicy: canonical.PolicyUSEquityCloseV1},
		}
		for _, r := range p.Rows {
			rows = append(rows, placed{r, i})
		}
	}
	slices.SortStableFunc(rows, func(a, b placed) int { return a.row.Row - b.row.Row })

	var prev, last time.Time
	seen := make(map[int64]bool, len(rows))
	for n, r := range rows {
		if err := ctx.Err(); err != nil {
			return nil, time.Time{}, err
		}
		if n > 0 && r.row.Row == rows[n-1].row.Row {
			return nil, time.Time{}, fmt.Errorf("%w: raw row %d appears twice", marketdata.ErrInvalidSource, r.row.Row)
		}
		f := &files[r.month]
		date := r.row.Bar.Time.UTC()
		if date.Year() != f.Key.Year || date.Month() != f.Key.Month || !date.Equal(date.Truncate(24*time.Hour)) {
			return nil, time.Time{}, fmt.Errorf("%w: row %d is dated %s, not a day of %04d-%02d", marketdata.ErrInvalidSource,
				r.row.Row, date.Format(time.RFC3339), f.Key.Year, int(f.Key.Month))
		}
		bar, reason, err := classifyTrade(id.instrument, r.row, date)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("row %d: %w", r.row.Row, err)
		}
		if reason == "" {
			switch {
			case seen[date.Unix()]:
				reason = canonical.ReasonDuplicate
			case date.Before(prev):
				reason = canonical.ReasonOutOfOrder
			}
		}
		prev = date
		if date.After(last) {
			last = date
		}
		if reason != "" {
			f.Bad = append(f.Bad, canonical.BadRecord{Time: date, Reason: reason, RawRow: r.row.Row})
			continue
		}
		seen[date.Unix()] = true
		f.Bars = append(f.Bars, bar)
	}
	return files, last, nil
}

// classifyTrade decides one row on its own merits: a finished bar and no reason,
// or the first reason that applies (incomplete, non-session, invalid). A date the
// calendar does not cover is an error.
func classifyTrade(symbol string, r marketdata.SourceRow, date time.Time) (marketdata.Bar, canonical.Reason, error) {
	session, err := canonical.USEquitySession(date.Year(), date.Month(), date.Day())
	switch {
	case errors.Is(err, canonical.ErrNonSession):
		if !r.Complete {
			return marketdata.Bar{}, canonical.ReasonIncomplete, nil
		}
		return marketdata.Bar{}, canonical.ReasonNonSession, nil
	case err != nil:
		return marketdata.Bar{}, "", err
	case !r.Complete:
		return marketdata.Bar{}, canonical.ReasonIncomplete, nil
	}
	b := r.Bar
	b.Instrument, b.Time, b.AvailableAt = symbol, session.Open, session.Close
	if r.Unrepresentable || !b.Bid.IsZero() || !b.Ask.IsZero() || !b.Mid.IsZero() || b.Trade.IsZero() || b.Validate() != nil {
		return marketdata.Bar{}, canonical.ReasonInvalid, nil
	}
	return b, "", nil
}

// covers refuses a snapshot that leaves out a month the series has on disk: those
// files would stay behind from another snapshot.
func covers(id seriesID, files []canonical.File) error {
	existing, err := canonical.Months(id.source, id.instrument, canonical.Interval(id.interval))
	if err != nil {
		return err
	}
	have := make(map[canonical.Key]bool, len(files))
	for _, f := range files {
		have[f.Key] = true
	}
	for _, k := range existing {
		if !have[k] {
			return fmt.Errorf("%w: it has no rows for %04d-%02d, which has a canonical file", ErrSnapshotConflict, k.Year, int(k.Month))
		}
	}
	return nil
}
