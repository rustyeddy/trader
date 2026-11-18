package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	mdservice "github.com/rustyeddy/trader/marketdata/service"
	"github.com/rustyeddy/trader/provider/oanda"
)

// ErrVerifyFailed reports that VerifyCanonical finished but one or more
// partitions disagree with their raw (or could not be read). Each is in its report.
var ErrVerifyFailed = errors.New("service: verification failed")

// The verification types, as the domain service defines them, so a transport can
// show them without importing it.
type (
	// Finding is one disagreement between a canonical file and its raw partition.
	Finding = mdservice.Finding
	// FindingKind names what is wrong.
	FindingKind = mdservice.FindingKind
)

// The partition states of a verification.
const (
	// VerifyOK: the file is what the raw says it should be.
	VerifyOK = "verified"
	// VerifyBad: the file disagrees with its raw, or the raw could not be read.
	VerifyBad = "failed"
	// VerifyNoCanonical: the raw exists and nothing was built from it (status and
	// build deal with that).
	VerifyNoCanonical = "no canonical"
	// VerifyNoRaw: there is no raw partition to check against.
	VerifyNoRaw = "no raw"
)

// VerifyRequest asks to check the canonical files of every instrument over the
// UTC months [From, To) touches, at one interval, against the raw archive.
type VerifyRequest struct {
	Instruments []string
	// Interval is the lower-case token, h1 or d1.
	Interval string
	From, To time.Time
	// Sample, when positive, checks at most that many months per instrument,
	// evenly spaced across the range and including its first and last; each month
	// chosen is checked in full. Zero checks every month.
	Sample int
}

// PartitionVerification is what checking one month partition came to.
type PartitionVerification struct {
	Instrument string
	Year       int
	Month      time.Month
	// Path is the canonical file's path relative to the data root.
	Path  string
	State string
	// RawRows, Bars and Bad count the raw rows and the file's bars and bad records.
	RawRows, Bars, Bad int
	// Explained counts the bad records the raw bears out, by reason: the expected
	// differences between raw and canonical.
	Explained map[string]int
	Findings  []Finding
	// Omitted counts findings beyond the ones kept.
	Omitted int
	// Err is why a partition could not be checked: its raw is unreadable.
	Err error
}

// VerifyCanonical checks canonical files against the raw OANDA partitions they
// were built from, one month at a time, instrument by instrument (see
// marketdata/service.Verify for what is checked), and calls report (when not nil)
// after each. A partition that disagrees, or whose raw cannot be read, is
// reported and the run carries on; at the end ErrVerifyFailed is returned if any
// did. A month with no raw, or with raw and no canonical file, is reported and is
// not a failure. Nothing is written. log may be nil.
func VerifyCanonical(ctx context.Context, log *slog.Logger, req VerifyRequest, report func(PartitionVerification)) error {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if !req.To.After(req.From) {
		return errors.New("verify: --to must be after --from")
	}
	if req.Sample < 0 {
		return errors.New("verify: --sample must not be negative")
	}
	interval := oanda.RawInterval(req.Interval)
	for _, symbol := range req.Instruments {
		if err := (oanda.PartitionKey{Symbol: symbol, Interval: interval, Year: req.From.UTC().Year(), Month: req.From.UTC().Month()}).Validate(); err != nil {
			return fmt.Errorf("verify: instrument %q, interval %q: %w", symbol, req.Interval, err)
		}
	}
	months := sampleMonths(monthsOf(req.From, req.To), req.Sample)

	failed := 0
	for _, symbol := range req.Instruments {
		for _, ms := range months {
			if err := ctx.Err(); err != nil {
				return err
			}
			res := verifyMonth(ctx, log, symbol, interval, ms)
			if err := ctx.Err(); err != nil {
				return err
			}
			if res.State == VerifyBad {
				failed++
			}
			if report != nil {
				report(res)
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%w: %d partitions", ErrVerifyFailed, failed)
	}
	return nil
}

func verifyMonth(ctx context.Context, log *slog.Logger, symbol string, interval oanda.RawInterval, ms time.Time) PartitionVerification {
	res := PartitionVerification{Instrument: symbol, Year: ms.Year(), Month: ms.Month()}
	key := oanda.PartitionKey{Symbol: symbol, Interval: interval, Year: ms.Year(), Month: ms.Month()}
	src, err := oanda.ReadSourcePartition(ctx, key)
	if errors.Is(err, fs.ErrNotExist) {
		res.State = VerifyNoRaw
		return res
	}
	if err != nil {
		res.State, res.Err = VerifyBad, err
		log.Error("raw partition unreadable", "symbol", symbol, "year", res.Year, "month", int(res.Month), "error", err)
		return res
	}
	v, err := mdservice.Verify(ctx, src)
	if err != nil {
		res.State, res.Err = VerifyBad, err
		log.Error("partition not verified", "symbol", symbol, "year", res.Year, "month", int(res.Month), "error", err)
		return res
	}
	res.Path, res.RawRows, res.Bars, res.Bad = v.Path, v.RawRows, v.Bars, v.Bad
	res.Explained, res.Findings, res.Omitted = v.Explained, v.Findings, v.Omitted
	switch v.State {
	case mdservice.Verified:
		res.State = VerifyOK
	case mdservice.NoCanonical:
		res.State = VerifyNoCanonical
	default:
		res.State = VerifyBad
		log.Warn("canonical disagrees with raw", "path", v.Path, "findings", len(v.Findings)+v.Omitted)
	}
	return res
}

// monthsOf lists the UTC months [from, to) touches.
func monthsOf(from, to time.Time) []time.Time {
	var out []time.Time
	for ms := monthStart(from); ms.Before(to); ms = ms.AddDate(0, 1, 0) {
		out = append(out, ms)
	}
	return out
}

// sampleMonths picks n of the months, evenly spaced and including the first and
// the last (just the first when n is 1); all of them when n is zero or at least
// their number. The choice depends only on the months and n, so a rerun checks the
// same ones.
func sampleMonths(months []time.Time, n int) []time.Time {
	if n <= 0 || n >= len(months) {
		return months
	}
	if n == 1 {
		return months[:1]
	}
	out := make([]time.Time, 0, n)
	for i := range n {
		out = append(out, months[i*(len(months)-1)/(n-1)])
	}
	return out
}
