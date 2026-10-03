package marketdata

import (
	"context"
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	"github.com/rustyeddy/trader/marketdata"
)

var (
	// ErrNoRawData reports a canonicalization with no range given and no
	// readable raw data to take the range from.
	ErrNoRawData = errors.New("service/marketdata: no raw data to canonicalize")
	// ErrNoCanonicalData reports an update with no range given and no
	// canonical data to start from.
	ErrNoCanonicalData = errors.New("service/marketdata: no canonical data to update from; canonicalize first or give a range")
)

// DatasetsRequest names one interval of several instruments, the shape
// of the multi-symbol operations (DatasetsCoverage, CanonicalizeDatasets,
// UpdateDatasets; issue #439). Range is optional; each operation
// documents what an omitted range means for each instrument.
type DatasetsRequest struct {
	Instruments []InstrumentRequest
	Interval    marketdata.Interval
	// Range, when given, applies to every instrument. Give both ends or
	// neither.
	Range marketdata.TimeRange
}

// Validate reports whether r is well-formed, returning a wrapped
// ErrInvalidRequest for the first problem found.
func (r DatasetsRequest) Validate() error {
	if len(r.Instruments) == 0 {
		return fmt.Errorf("%w: at least one instrument is required", ErrInvalidRequest)
	}
	if !r.Interval.Valid() {
		return fmt.Errorf("%w: interval is invalid", ErrInvalidRequest)
	}
	start, end := r.Range.Start(), r.Range.End()
	if start.IsZero() != end.IsZero() {
		return fmt.Errorf("%w: range must be fully specified or omitted", ErrInvalidRequest)
	}
	if !start.IsZero() && !end.After(start) {
		return fmt.Errorf("%w: range is invalid", ErrInvalidRequest)
	}
	return nil
}

func (r DatasetsRequest) hasRange() bool { return !r.Range.Start().IsZero() }

// DatasetStatus is one instrument's outcome in a mutating multi-symbol
// operation.
type DatasetStatus string

const (
	// DatasetBuilt means canonicalization published canonical partitions.
	DatasetBuilt DatasetStatus = "built"
	// DatasetUpdated means an update published canonical partitions.
	DatasetUpdated DatasetStatus = "updated"
	// DatasetCurrent means the operation succeeded with nothing to
	// publish: the canonical data was already current.
	DatasetCurrent DatasetStatus = "current"
	// DatasetFailed means the operation failed for this instrument; Err
	// says why.
	DatasetFailed DatasetStatus = "failed"
)

// DatasetResult is one instrument's outcome in CanonicalizeDatasets or
// UpdateDatasets.
type DatasetResult struct {
	Request    InstrumentRequest
	Instrument instrument.ID // zero if resolution failed
	Status     DatasetStatus
	// Range is the range the operation acted on; zero if it failed
	// before choosing one, or if an update had nothing to do.
	Range marketdata.TimeRange
	// Raw and CanonicalAfter are the inventory after the operation,
	// CanonicalBefore the canonical inventory before it. Each is nil
	// when there was no such data (or the operation failed first).
	Raw             *marketruntime.DataSpan
	CanonicalBefore *marketruntime.DataSpan
	CanonicalAfter  *marketruntime.DataSpan
	// PublishedPartitions and PublishedBars count what the operation
	// published.
	PublishedPartitions int
	PublishedBars       int
	Err                 error
}

// DatasetsResponse holds one DatasetResult per requested instrument, in
// request order.
type DatasetsResponse struct {
	Results []DatasetResult
}

// CoverageResult is one instrument's outcome in DatasetsCoverage.
type CoverageResult struct {
	Request    InstrumentRequest
	Instrument instrument.ID
	// Coverage is empty (no partitions, zero Range) when no range was
	// given and the instrument has no canonical data; see Coverage.
	Coverage  marketruntime.Coverage
	Inventory marketruntime.Inventory
	Err       error
}

// DatasetsCoverageResponse holds one CoverageResult per requested
// instrument, in request order.
type DatasetsCoverageResponse struct {
	Results []CoverageResult
}

// DatasetsCoverage resolves each instrument and reports its coverage and
// inventory, read-only. An omitted range means each instrument's own
// canonical span (see Coverage). One instrument failing is recorded on
// its result and never hides the others; the call itself fails only for
// an invalid request, a missing resolver, or ctx, and then returns the
// results gathered so far.
func (s *Service) DatasetsCoverage(ctx context.Context, req DatasetsRequest) (resp DatasetsCoverageResponse, err error) {
	if err := req.Validate(); err != nil {
		return DatasetsCoverageResponse{}, err
	}
	failed := 0
	defer s.logDatasets(ctx, "coverage", req, &err, &failed, nil)
	if s.resolver == nil {
		return DatasetsCoverageResponse{}, ErrResolverNotConfigured
	}
	for _, r := range req.Instruments {
		if err := ctx.Err(); err != nil {
			return resp, err
		}
		res := CoverageResult{Request: r}
		res.Err = s.coverageOne(ctx, req, &res)
		if res.Err != nil {
			failed++
		}
		resp.Results = append(resp.Results, res)
	}
	return resp, nil
}

func (s *Service) coverageOne(ctx context.Context, req DatasetsRequest, res *CoverageResult) error {
	resolved, err := s.resolveInstrument(ctx, res.Request)
	if err != nil {
		return err
	}
	res.Instrument = resolved.Instrument
	if res.Inventory, err = s.manager.Inventory(ctx, res.Instrument, req.Interval); err != nil {
		return err
	}
	cov, err := s.coverage(ctx, CoverageRequest{DatasetRequest: DatasetRequest{Instrument: res.Instrument, Interval: req.Interval, Range: req.Range}})
	res.Coverage = cov.Coverage
	return err
}

// CanonicalizeDatasets builds canonical data from each instrument's
// provider-native data through the existing Plan/Build path:
//
//   - for stooq, by converting the instrument's native archive
//     (ConvertStooqArchive, under the Service's archive root), or, when
//     no archive is found, from raw data already imported;
//   - for every other provider, from the raw data already present.
//
// An omitted range means each instrument's whole source: the archive's
// span, or [Raw.First, Raw.End). Force rebuilds partitions that are
// already current. Each result is DatasetBuilt when partitions were
// published and DatasetCurrent when none needed to be. See
// DatasetsCoverage for failure semantics.
func (s *Service) CanonicalizeDatasets(ctx context.Context, req DatasetsRequest, force bool) (DatasetsResponse, error) {
	return s.eachDataset(ctx, "canonicalize", req, func(ctx context.Context, res *DatasetResult, symbol string, before marketruntime.Inventory) error {
		return s.canonicalizeOne(ctx, req, force, res, symbol, before)
	})
}

func (s *Service) canonicalizeOne(ctx context.Context, req DatasetsRequest, force bool, res *DatasetResult, symbol string, before marketruntime.Inventory) error {
	dataset := DatasetRequest{Instrument: res.Instrument, Interval: req.Interval, Range: req.Range}
	if s.provider == "stooq" {
		conv, err := s.ConvertStooqArchive(ctx, ConvertStooqArchiveRequest{DatasetRequest: dataset, Symbol: symbol, Force: force})
		if err == nil {
			res.Range = conv.Range
			res.recordPublished(conv.Build.Result, DatasetBuilt)
			return nil
		}
		noArchive := errors.Is(err, ErrArchiveNotFound) || errors.Is(err, ErrArchiveRootNotConfigured)
		if !noArchive || before.Raw == nil {
			return err
		}
		// No archive, but raw data was imported earlier: build from it.
	}
	if !req.hasRange() {
		if before.Raw == nil {
			return ErrNoRawData
		}
		rng, err := marketdata.NewTimeRange(before.Raw.First, before.Raw.End)
		if err != nil {
			return err
		}
		dataset.Range = rng
	}
	build, err := s.Build(ctx, BuildRequest{DatasetRequest: dataset, Force: force})
	if err != nil {
		return err
	}
	res.Range = dataset.Range
	res.recordPublished(build.Result, DatasetBuilt)
	return nil
}

// UpdateDatasets brings each instrument's canonical data forward to the
// latest provider data through the existing update path. An omitted
// range means from the instrument's last canonical bar through now (the
// Manager's clock); an instrument with no canonical data then fails with
// ErrNoCanonicalData. The last canonical bar's month is included, so a
// partially built month is completed.
//
//   - stooq has no live feed: its update re-converts the instrument's
//     native archive over that range, picking up whatever newer data the
//     archive now holds.
//   - every other provider runs Update: Plan, Sync, Build.
//
// Each result is DatasetUpdated when partitions were published and
// DatasetCurrent when none needed to be. See DatasetsCoverage for failure
// semantics.
func (s *Service) UpdateDatasets(ctx context.Context, req DatasetsRequest) (DatasetsResponse, error) {
	return s.eachDataset(ctx, "update", req, func(ctx context.Context, res *DatasetResult, symbol string, before marketruntime.Inventory) error {
		return s.updateOne(ctx, req, res, symbol, before)
	})
}

func (s *Service) updateOne(ctx context.Context, req DatasetsRequest, res *DatasetResult, symbol string, before marketruntime.Inventory) error {
	dataset := DatasetRequest{Instrument: res.Instrument, Interval: req.Interval, Range: req.Range}
	if !req.hasRange() {
		if before.Canonical == nil || before.Canonical.Last.IsZero() {
			return ErrNoCanonicalData
		}
		start, end := before.Canonical.Last, s.manager.Now()
		if !end.After(start) {
			res.Status = DatasetCurrent
			return nil
		}
		rng, err := marketdata.NewTimeRange(start, end)
		if err != nil {
			return err
		}
		dataset.Range = rng
	}
	if s.provider == "stooq" {
		conv, err := s.ConvertStooqArchive(ctx, ConvertStooqArchiveRequest{DatasetRequest: dataset, Symbol: symbol})
		if err != nil {
			return err
		}
		res.Range = conv.Range
		res.recordPublished(conv.Build.Result, DatasetUpdated)
		return nil
	}
	upd, err := s.Update(ctx, UpdateRequest{DatasetRequest: dataset})
	if err != nil {
		return err
	}
	res.Range = dataset.Range
	res.recordPublished(upd.Build.Result, DatasetUpdated)
	return nil
}

// recordPublished counts build's publications and sets the status: done
// when anything was published, DatasetCurrent otherwise.
func (res *DatasetResult) recordPublished(build marketruntime.BuildResult, done DatasetStatus) {
	res.PublishedPartitions = len(build.Published)
	for _, p := range build.Published {
		res.PublishedBars += p.BarCount
	}
	res.Status = DatasetCurrent
	if res.PublishedPartitions > 0 {
		res.Status = done
	}
}

// eachDataset runs op for each requested instrument: resolve it, take its
// inventory, run op, and take its inventory again. A failure is recorded
// on that instrument's result. It logs one aggregate record after
// validation, whichever way it exits; the operations it calls (Build,
// Update, ConvertStooqArchive) log their own step records, as Update's
// Sync and Build steps do.
func (s *Service) eachDataset(ctx context.Context, op string, req DatasetsRequest,
	run func(ctx context.Context, res *DatasetResult, symbol string, before marketruntime.Inventory) error,
) (resp DatasetsResponse, err error) {
	if err := req.Validate(); err != nil {
		return DatasetsResponse{}, err
	}
	failed, published := 0, 0
	defer s.logDatasets(ctx, op, req, &err, &failed, &published)
	if s.resolver == nil {
		return DatasetsResponse{}, ErrResolverNotConfigured
	}
	for _, r := range req.Instruments {
		if err := ctx.Err(); err != nil {
			return resp, err
		}
		res := DatasetResult{Request: r}
		if err := s.runDataset(ctx, req.Interval, &res, run); err != nil {
			res.Status, res.Err = DatasetFailed, err
			failed++
		}
		published += res.PublishedPartitions
		resp.Results = append(resp.Results, res)
	}
	return resp, nil
}

func (s *Service) runDataset(ctx context.Context, interval marketdata.Interval, res *DatasetResult,
	run func(ctx context.Context, res *DatasetResult, symbol string, before marketruntime.Inventory) error,
) error {
	resolved, err := s.resolveInstrument(ctx, res.Request)
	if err != nil {
		return err
	}
	res.Instrument = resolved.Instrument
	before, err := s.manager.Inventory(ctx, res.Instrument, interval)
	if err != nil {
		return err
	}
	res.CanonicalBefore = before.Canonical
	if err := run(ctx, res, resolved.Identity.Symbol, before); err != nil {
		return err
	}
	after, err := s.manager.Inventory(ctx, res.Instrument, interval)
	if err != nil {
		return err
	}
	res.Raw, res.CanonicalAfter = after.Raw, after.Canonical
	return nil
}

// logDatasets emits a multi-symbol operation's one aggregate record.
func (s *Service) logDatasets(ctx context.Context, op string, req DatasetsRequest, err *error, failed, published *int) {
	attrs := []any{"operation", op, "provider", s.provider, "interval", req.Interval.String(),
		"requested", len(req.Instruments), "failed", *failed}
	if published != nil {
		attrs = append(attrs, "published_partitions", *published)
	}
	if *err != nil {
		s.logger.ErrorContext(ctx, "datasets "+op+" failed", append(attrs, "error", *err)...)
		return
	}
	s.logger.InfoContext(ctx, "datasets "+op+" completed", attrs...)
}
