// Package service holds the application services (ADR-091): cross-domain
// orchestration that a transport such as cmd/trader calls. The transport supplies
// plain configuration values; a service coordinates providers and domain services
// to carry out one use case. It reads no environment and holds no secret.
//
// marketdata.go is the market-data use case so far: DownloadRaw fetches provider
// candles month by month into the raw store; BuildCanonical reads a raw
// provider partition, hands it to the market-data domain service as a
// marketdata.SourcePartition, and reports what each partition came to; LoadSeries
// asks the domain service for canonical bars as series and reports every month's
// state. One file is enough while the services are this small; a package per
// service waits until they are not.
package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	mdservice "github.com/rustyeddy/trader/marketdata/service"
	"github.com/rustyeddy/trader/provider/oanda"
)

// ErrPartitionsFailed reports that BuildCanonical finished but one or more
// partitions failed. Each failure was reported through the callback.
var ErrPartitionsFailed = errors.New("service: partitions failed")

// Action says what BuildCanonical did for one partition.
type Action string

// The actions. Built, Unchanged and Replaced come from the domain service;
// Missing means there is no raw partition for the month (nothing to build from,
// and not an error); Failed means the partition could not be built.
const (
	Built     Action = Action(mdservice.Built)
	Unchanged Action = Action(mdservice.Unchanged)
	Replaced  Action = Action(mdservice.Replaced)
	Missing   Action = "missing"
	Failed    Action = "failed"
)

// DownloadRequest is what one download asks for: every instrument over the UTC
// months that [From, To) touches, at one interval. The values come from the
// transport.
type DownloadRequest struct {
	Instruments []string
	// Interval is the lower-case token: m1, h1, h4 or d1.
	Interval string
	From, To time.Time
}

// DownloadResult is what one month partition's download came to.
type DownloadResult struct {
	Instrument string
	Year       int
	Month      time.Month
	// Path is the raw file's path relative to the data root.
	Path       string
	Fetched    int // candles the provider returned for the month's window
	Incomplete int // candles still forming, never stored
	Added      int // rows written that the file did not have
	Replaced   int // stored rows that were still forming, replaced by the closed candle
	Kept       int // complete rows already on file, left exactly as they were
	Differing  int // of Kept, rows whose provider values differ from the file's
}

// Credential supplies the provider's bearer token for each request. The transport
// implements it over wherever it reads the token from (the environment), so the
// service and the client never hold the token and no value of this type is ever
// printed.
type Credential interface {
	Token(ctx context.Context) (string, error)
}

// ProviderConfig is what the transport decides about reaching the provider: the
// service builds the OANDA client from it, so no transport imports a provider.
type ProviderConfig struct {
	// BaseURL is the provider's API host (absolute, https); the transport chooses
	// practice or live.
	BaseURL string
	// Credential supplies the token. Required.
	Credential Credential
	// RequestGap spaces the provider's requests; zero disables pacing.
	RequestGap time.Duration
}

// Download is DownloadRaw over the OANDA client that cfg describes: the use case
// the transport calls. It builds the client (refusing a bad base URL or a missing
// credential before any fetch) and then downloads as DownloadRaw does. log, which
// may be nil, also receives the client's request records; the token is never in
// them.
func Download(ctx context.Context, log *slog.Logger, cfg ProviderConfig, req DownloadRequest, report func(DownloadResult)) error {
	if cfg.Credential == nil {
		return errors.New("download: no credential")
	}
	client, err := oanda.NewClient(oanda.ClientConfig{
		BaseURL:            cfg.BaseURL,
		Credential:         cfg.Credential,
		Logger:             log,
		MinRequestInterval: cfg.RequestGap,
	})
	if err != nil {
		return err
	}
	return DownloadRaw(ctx, log, client, req, report)
}

// CandleFetcher is what DownloadRaw needs from a provider client.
type CandleFetcher interface {
	FetchCandles(ctx context.Context, req oanda.CandleRequest) ([]oanda.Record, error)
}

// DownloadRaw fetches req one UTC month partition at a time, instrument by
// instrument, merging each into the raw store (oanda.DownloadPartition owns the
// archive rules: a complete stored row is never changed, forming candles are not
// stored and a stored forming row is replaced once its candle closes), and
// calls report (when not nil) after each partition, in order. A request that is
// invalid fails before any fetch. The first partition that fails stops the run
// with an error that names it; the partitions already done stay done.
// Cancellation stops the run at once. log may be nil.
func DownloadRaw(ctx context.Context, log *slog.Logger, f CandleFetcher, req DownloadRequest, report func(DownloadResult)) error {
	if !req.To.After(req.From) {
		return errors.New("download: --to must be after --from")
	}
	interval := oanda.RawInterval(req.Interval)
	for _, symbol := range req.Instruments {
		if err := (oanda.PartitionKey{Symbol: symbol, Interval: interval, Year: req.From.UTC().Year(), Month: req.From.UTC().Month()}).Validate(); err != nil {
			return fmt.Errorf("--instrument %q / --interval %q: %w", symbol, req.Interval, err)
		}
	}
	for _, symbol := range req.Instruments {
		for ms := monthStart(req.From); ms.Before(req.To); ms = ms.AddDate(0, 1, 0) {
			if err := ctx.Err(); err != nil {
				return err
			}
			key := oanda.PartitionKey{Symbol: symbol, Interval: interval, Year: ms.Year(), Month: ms.Month()}
			from, to := laterOf(req.From.UTC(), ms), earlierOf(req.To.UTC(), ms.AddDate(0, 1, 0))
			r, err := oanda.DownloadPartition(ctx, log, f, key, from, to)
			if err != nil {
				return fmt.Errorf("%s %04d-%02d: %w", symbol, ms.Year(), int(ms.Month()), err)
			}
			if report != nil {
				report(DownloadResult{Instrument: symbol, Year: ms.Year(), Month: ms.Month(), Path: r.Path,
					Fetched: r.Fetched, Incomplete: r.Incomplete, Added: r.Added, Replaced: r.Replaced, Kept: r.Kept, Differing: r.Differing})
			}
		}
	}
	return nil
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlierOf(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// BuildRequest is what one build asks for: every instrument over the UTC months
// that [From, To) touches, at one interval. The values come from the transport.
type BuildRequest struct {
	Instruments []string
	// Interval is the lower-case token, h1 or d1.
	Interval string
	From, To time.Time
	// BuiltAt is recorded in each file as provenance; the zero value means now.
	BuiltAt time.Time
}

// PartitionResult is what one month partition came to.
type PartitionResult struct {
	Instrument string
	Year       int
	Month      time.Month
	// Path is the canonical file's path relative to the data root.
	Path   string
	Action Action
	// Good and Bad count the bars and the kept bad records of a built, unchanged
	// or replaced partition. Bad > 0 means the partition is not complete data.
	Good, Bad int
	// Err is why a Failed partition failed.
	Err error
}

// BuildCanonical converts the raw OANDA partitions in req to canonical files, one
// month at a time, instrument by instrument, and calls report (when not nil)
// after each. A partition that cannot be built is reported as Failed and the
// build carries on with the rest; at the end ErrPartitionsFailed is returned if
// any failed. Cancellation stops the run at once and returns the context's error.
// Raw is only read. log may be nil.
func BuildCanonical(ctx context.Context, log *slog.Logger, req BuildRequest, report func(PartitionResult)) error {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if !req.To.After(req.From) {
		return errors.New("build: --to must be after --from")
	}
	builtAt := req.BuiltAt
	if builtAt.IsZero() {
		builtAt = time.Now().UTC()
	}
	interval := oanda.RawInterval(req.Interval)
	for _, symbol := range req.Instruments {
		if err := (oanda.PartitionKey{Symbol: symbol, Interval: interval, Year: req.From.UTC().Year(), Month: req.From.UTC().Month()}).Validate(); err != nil {
			return fmt.Errorf("build: instrument %q, interval %q: %w", symbol, req.Interval, err)
		}
	}

	failed := 0
	for _, symbol := range req.Instruments {
		for ms := monthStart(req.From); ms.Before(req.To); ms = ms.AddDate(0, 1, 0) {
			if err := ctx.Err(); err != nil {
				return err
			}
			res := buildMonth(ctx, log, symbol, interval, ms, builtAt)
			if err := ctx.Err(); err != nil {
				return err
			}
			if res.Action == Failed {
				failed++
			}
			if report != nil {
				report(res)
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%w: %d", ErrPartitionsFailed, failed)
	}
	return nil
}

func buildMonth(ctx context.Context, log *slog.Logger, symbol string, interval oanda.RawInterval, ms time.Time, builtAt time.Time) PartitionResult {
	res := PartitionResult{Instrument: symbol, Year: ms.Year(), Month: ms.Month()}
	key := oanda.PartitionKey{Symbol: symbol, Interval: interval, Year: ms.Year(), Month: ms.Month()}
	src, err := oanda.ReadSourcePartition(ctx, key)
	if errors.Is(err, fs.ErrNotExist) {
		res.Action = Missing
		log.Info("no raw partition", "symbol", symbol, "year", res.Year, "month", int(res.Month))
		return res
	}
	if err != nil {
		return failed(log, res, err)
	}
	built, err := mdservice.Build(ctx, src, builtAt)
	if err != nil {
		return failed(log, res, err)
	}
	res.Path, res.Action, res.Good, res.Bad = built.Path, Action(built.Action), built.Good, built.Bad
	if built.Bad > 0 {
		log.Warn("partition has bad records and is not complete data", "path", built.Path, "good", built.Good, "bad", built.Bad)
	} else {
		log.Info("partition done", "path", built.Path, "action", string(built.Action), "good", built.Good)
	}
	return res
}

func failed(log *slog.Logger, res PartitionResult, err error) PartitionResult {
	res.Action, res.Err = Failed, err
	log.Error("partition failed", "symbol", res.Instrument, "year", res.Year, "month", int(res.Month), "error", err)
	return res
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// The month states and statuses of a load, as the domain service defines them, so
// a transport can show them without importing the domain service.
type (
	// MonthState is what one month's canonical file came to.
	MonthState = mdservice.MonthState
	// MonthStatus is the state of one UTC month of a load.
	MonthStatus = mdservice.MonthStatus
	// Gap is a run of missing bars inside a loaded series.
	Gap = mdservice.Gap
	// GapKind says whether the market calendar explains a gap.
	GapKind = mdservice.GapKind
)

// The gap kinds: a year-end holiday closure (reported, never an error) and bars
// missing that the calendar says should be there (an error unless missing data is
// allowed). The weekly close is never reported.
const (
	GapKnownClosure = mdservice.KnownClosure
	GapUnexplained  = mdservice.Unexplained
)

// The month states.
const (
	MonthLoaded  = mdservice.Loaded
	MonthBad     = mdservice.Bad
	MonthMissing = mdservice.Missing
	MonthStale   = mdservice.Stale
)

// ErrNotLoadable reports that LoadSeries finished but one or more instruments
// could not be loaded as asked. Each failure is in its SeriesResult.
var ErrNotLoadable = errors.New("service: series not loadable")

// LoadRequest is what one load asks for: every instrument's canonical bars at one
// interval whose opening time is in [From, To). The values come from the
// transport. By default a month that is missing or has bad records fails the
// instrument; AllowMissing and AllowBad accept them. A stale month never loads.
type LoadRequest struct {
	Instruments []string
	// Interval is the lower-case token, h1 or d1.
	Interval     string
	From, To     time.Time
	AllowBad     bool
	AllowMissing bool
}

// SeriesResult is what one instrument's load came to.
type SeriesResult struct {
	Instrument string
	// Series holds the bars when Err is nil, and is empty otherwise.
	Series marketdata.Series
	// Months is the status of every month the range touches, in order, even when
	// the load failed.
	Months []MonthStatus
	// Gaps are the interior gaps of the series: holiday closures and unexplained
	// gaps, never the weekly close. Present when the months loaded, whether or not
	// the gaps failed the load.
	Gaps []Gap
	// Err is why the instrument could not be loaded: an incomplete range, a gap
	// the calendar does not explain, or a failure to read.
	Err error
}

// LoadSeries loads the canonical series for req, one instrument at a time, from
// the OANDA canonical files. An instrument that cannot be loaded as asked is
// reported in its result and the rest carry on; at the end ErrNotLoadable is
// returned if any failed. Cancellation stops the run at once and returns the
// context's error. Nothing is written. log may be nil.
func LoadSeries(ctx context.Context, log *slog.Logger, req LoadRequest) ([]SeriesResult, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if len(req.Instruments) == 0 {
		return nil, errors.New("load: no instrument")
	}
	if !req.To.After(req.From) {
		return nil, errors.New("load: --to must be after --from")
	}
	var results []SeriesResult
	failed := 0
	for _, symbol := range req.Instruments {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		got, err := mdservice.Load(ctx, mdservice.LoadRequest{
			Source: "oanda", Instrument: symbol, Interval: req.Interval, From: req.From, To: req.To,
			AllowBad: req.AllowBad, AllowMissing: req.AllowMissing,
		})
		if cerr := ctx.Err(); cerr != nil {
			return results, cerr
		}
		res := SeriesResult{Instrument: symbol, Series: got.Series, Months: got.Months, Gaps: got.Gaps, Err: err}
		if err != nil {
			failed++
			log.Warn("series not loaded", "symbol", symbol, "error", err)
		} else {
			log.Info("series loaded", "symbol", symbol, "bars", len(got.Series.Bars), "months", len(got.Months), "gaps", len(got.Gaps))
		}
		results = append(results, res)
	}
	if failed > 0 {
		return results, fmt.Errorf("%w: %d of %d instruments", ErrNotLoadable, failed, len(results))
	}
	return results, nil
}
