package marketdata

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	// OnResult, if set, is called with each instrument's result as soon
	// as it is recorded, in request order and on the calling goroutine,
	// so a transport can report progress on a long call. CanonicalizeDatasets
	// and UpdateDatasets call it; DatasetsCoverage does not.
	OnResult func(DatasetResult)
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
	// The step's full response, for callers that report it in detail
	// (the CLI). At most one is set: Convert for a stooq archive
	// conversion, Build for a build from raw, Update for an update. It
	// is set even when the step failed, holding its partial progress.
	Convert *ConvertResponse
	Build   *BuildResponse
	Update  *UpdateResponse
	Err     error
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
// an invalid request, a missing resolver, or ctx; a canceled ctx returns
// its error with the results gathered so far.
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
	return resp, ctx.Err()
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
	rng := req.Range
	if !req.hasRange() {
		// The canonical span, from the inventory already taken, rather
		// than letting coverage scan the raw tree a second time.
		span := res.Inventory.Canonical
		if span == nil || span.First.IsZero() {
			res.Coverage = marketruntime.Coverage{Instrument: res.Instrument, Interval: req.Interval}
			return nil
		}
		if rng, err = marketdata.NewTimeRange(span.First, span.End); err != nil {
			return err
		}
	}
	cov, err := s.coverage(ctx, CoverageRequest{DatasetRequest: DatasetRequest{Instrument: res.Instrument, Interval: req.Interval, Range: rng}})
	res.Coverage = cov.Coverage
	return err
}

// CanonicalizeOptions adjusts CanonicalizeDatasets.
type CanonicalizeOptions struct {
	// Force rebuilds partitions that are already current.
	Force bool
	// ArchivePath names the native archive to convert (stooq only), in
	// place of discovery under the Service's archive root. It is valid
	// only for a single-instrument request.
	ArchivePath string
}

// CanonicalizeDatasets builds canonical data from each instrument's
// provider-native data through the existing Plan/Build path:
//
//   - for a provider whose data arrives as a native archive
//     (ProviderInfo.NativeArchive: stooq), by converting the
//     instrument's native archive
//     (ConvertStooqArchive: opts.ArchivePath, or discovery under the
//     Service's archive root), or, when no archive is found, from raw
//     data already imported;
//   - for every other provider, from the raw data already present.
//
// It never downloads. An omitted range means each instrument's whole
// source: the archive's span, or [Raw.First, Raw.End). Each result is
// DatasetBuilt when partitions were published and DatasetCurrent when
// none needed to be. An instrument with no source data anywhere in the
// range fails with ErrNoRawData rather than reporting DatasetCurrent.
// See DatasetsCoverage for failure semantics.
func (s *Service) CanonicalizeDatasets(ctx context.Context, req DatasetsRequest, opts CanonicalizeOptions) (DatasetsResponse, error) {
	if opts.ArchivePath != "" && len(req.Instruments) != 1 {
		return DatasetsResponse{}, fmt.Errorf("%w: an archive path applies to exactly one instrument", ErrInvalidRequest)
	}
	return s.eachDataset(ctx, "canonicalize", req, func(ctx context.Context, res *DatasetResult, symbol string, before marketruntime.Inventory) error {
		return s.canonicalizeOne(ctx, req, opts, res, symbol, before)
	})
}

func (s *Service) canonicalizeOne(ctx context.Context, req DatasetsRequest, opts CanonicalizeOptions, res *DatasetResult, symbol string, before marketruntime.Inventory) error {
	dataset := DatasetRequest{Instrument: res.Instrument, Interval: req.Interval, Range: req.Range}
	if s.manager.Provider().NativeArchive {
		done, err := s.stooqConvert(ctx, res, dataset, symbol, opts.ArchivePath, opts.Force, before, DatasetBuilt)
		if done || err != nil {
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
	res.Range = dataset.Range
	build, err := s.Build(ctx, BuildRequest{DatasetRequest: dataset, Force: opts.Force})
	res.Build = &build
	res.recordPublished(build.Result, DatasetBuilt)
	if err != nil {
		return err
	}
	if res.PublishedPartitions == 0 && missingEverywhere(build.Plan, dataset.Range) {
		return fmt.Errorf("%w in [%s, %s)", ErrNoRawData,
			dataset.Range.Start().UTC().Format(time.RFC3339), dataset.Range.End().UTC().Format(time.RFC3339))
	}
	return nil
}

// stooqConvert converts the stooq archive into res. done reports that it
// handled the dataset (successfully or not, with err); !done with a nil
// err means no archive was found but raw data exists, so the caller
// should build from that raw data instead.
func (s *Service) stooqConvert(ctx context.Context, res *DatasetResult, dataset DatasetRequest, symbol, archivePath string, force bool, before marketruntime.Inventory, status DatasetStatus) (done bool, err error) {
	conv, err := s.ConvertStooqArchive(ctx, ConvertStooqArchiveRequest{DatasetRequest: dataset, Symbol: symbol, ArchivePath: archivePath, Force: force})
	noArchive := errors.Is(err, ErrArchiveNotFound) || errors.Is(err, ErrArchiveRootNotConfigured)
	if noArchive && before.Raw != nil {
		return false, nil
	}
	res.Convert = &conv
	res.Range = conv.Range
	res.recordPublished(conv.Build.Result, status)
	return true, err
}

// missingEverywhere reports whether plan found no raw data for any month
// of rng: every month needs a raw download for missing data. A derived
// interval (W1) never plans downloads, so it never matches.
func missingEverywhere(plan marketruntime.Plan, rng marketdata.TimeRange) bool {
	missing := map[[2]int]bool{}
	for _, a := range plan.Actions {
		if a.Kind == marketruntime.ActionDownloadRaw && a.Reason == "missing" {
			missing[[2]int{a.Year, int(a.Month)}] = true
		}
	}
	if len(missing) == 0 {
		return false
	}
	start, end := rng.Start().UTC(), rng.End().UTC()
	for m := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC); m.Before(end); m = m.AddDate(0, 1, 0) {
		if !missing[[2]int{m.Year(), int(m.Month())}] {
			return false
		}
	}
	return true
}

// UpdateDatasets brings each instrument's canonical data forward to the
// latest provider data through the existing update path. An omitted
// range means from the instrument's last canonical bar through now (the
// Manager's clock); an instrument with no canonical data then fails with
// ErrNoCanonicalData. The last canonical bar's month is included, so a
// partially built month is completed.
//
//   - a native-archive provider (stooq) has no live feed: its update
//     re-converts the instrument's
//     native archive over that range, picking up whatever newer data the
//     archive now holds; with no archive it rebuilds from imported raw
//     data, as CanonicalizeDatasets does.
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
	res.Range = dataset.Range
	if s.manager.Provider().NativeArchive {
		done, err := s.stooqConvert(ctx, res, dataset, symbol, "", false, before, DatasetUpdated)
		if done || err != nil {
			return err
		}
		build, err := s.Build(ctx, BuildRequest{DatasetRequest: dataset})
		res.Build = &build
		res.recordPublished(build.Result, DatasetUpdated)
		return err
	}
	upd, err := s.Update(ctx, UpdateRequest{DatasetRequest: dataset})
	res.Update = &upd
	res.recordPublished(upd.Build.Result, DatasetUpdated)
	return err
}

// recordPublished counts build's publications and sets the status: done
// when anything was published, DatasetCurrent otherwise. Callers record
// before checking the step's error, so a failed step still reports what
// it published.
func (res *DatasetResult) recordPublished(build marketruntime.BuildResult, done DatasetStatus) {
	res.PublishedPartitions = len(build.Published)
	res.PublishedBars = 0
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
// on that instrument's result, along with whatever the step had already
// done. It logs one aggregate record after validation, whichever way it
// exits; the operations it calls (Build, Update, ConvertStooqArchive) log
// their own step records, as Update's Sync and Build steps do.
//
// If ctx is canceled, the call returns ctx's error with the results
// gathered so far, including a symbol the cancellation interrupted.
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
		if req.OnResult != nil {
			req.OnResult(res)
		}
	}
	return resp, ctx.Err()
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
	runErr := run(ctx, res, resolved.Identity.Symbol, before)
	// The after-state is taken even when the step failed: a failed step
	// can already have imported raw data or published partitions.
	after, err := s.manager.Inventory(ctx, res.Instrument, interval)
	if err == nil {
		res.Raw, res.CanonicalAfter = after.Raw, after.Canonical
	}
	if runErr != nil {
		return runErr
	}
	return err
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
