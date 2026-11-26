package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	mdservice "github.com/rustyeddy/trader/marketdata/service"
	"github.com/rustyeddy/trader/provider/stooq"
)

// StooqBuildRequest is what one Stooq build asks for. The values come from the
// transport.
type StooqBuildRequest struct {
	// Instrument and Interval name the series; only SPY d1 is supported.
	Instrument, Interval string
	// Input is the file to read, relative to the data root: Stooq's daily bundle
	// ZIP, or one symbol's file in the same format.
	Input string
	// Member is the symbol's path inside a ZIP, and empty for another file.
	Member string
	// SnapshotAsOf is the date the input was obtained, YYYY-MM-DD: required the
	// first time these bytes are built.
	SnapshotAsOf string
	// BuiltAt is recorded in each file as provenance; the zero value means now.
	BuiltAt time.Time
}

// StooqBuild is what one Stooq build came to.
type StooqBuild struct {
	// Fingerprint is the hash of the input's bytes and SnapshotAsOf the date in
	// force for them.
	Fingerprint, SnapshotAsOf string
	// SnapshotPath is the archived input, relative to the data root.
	SnapshotPath string
}

// BuildStooq builds the canonical series of one Stooq daily history, the whole of
// it: it reads the input's bytes through the Stooq provider and hands them to the
// market-data domain service, which archives them and publishes every month
// (ADR-095). report, when not nil, is called for each published month in order,
// including those published before a failure. The input is only read. Nothing
// uses the network. log may be nil.
func BuildStooq(ctx context.Context, log *slog.Logger, req StooqBuildRequest, report func(PartitionResult)) (StooqBuild, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if req.Instrument != "SPY" || req.Interval != "d1" {
		return StooqBuild{}, fmt.Errorf("build: stooq supports only SPY d1, not %q %q", req.Instrument, req.Interval)
	}
	if req.Input == "" {
		return StooqBuild{}, errors.New("build: no input file")
	}
	builtAt := req.BuiltAt
	if builtAt.IsZero() {
		builtAt = time.Now().UTC()
	}
	in, err := stooq.ReadSnapshot(ctx, req.Input, req.Member)
	if err != nil {
		return StooqBuild{}, fmt.Errorf("build: %w", err)
	}
	partitions, err := stooq.Parse(req.Instrument, in.Data)
	if err != nil {
		return StooqBuild{}, fmt.Errorf("build: %s: %w", req.Input, err)
	}
	built, err := mdservice.BuildSeries(ctx, mdservice.Snapshot{
		Data: in.Data, AsOf: req.SnapshotAsOf, File: in.File, Member: in.Member, ArchiveSHA256: in.ArchiveSHA256,
		Partitions: partitions,
	}, builtAt)
	for _, m := range built.Months {
		if m.Bad > 0 {
			log.Warn("partition has bad records and is not complete data", "path", m.Path, "good", m.Good, "bad", m.Bad)
		} else {
			log.Info("partition done", "path", m.Path, "action", string(m.Action), "good", m.Good)
		}
		if report != nil {
			report(PartitionResult{Instrument: m.Instrument, Year: m.Year, Month: m.Month, Path: m.Path,
				Action: Action(m.Action), Good: m.Good, Bad: m.Bad})
		}
	}
	out := StooqBuild{Fingerprint: built.Fingerprint, SnapshotAsOf: built.AsOf, SnapshotPath: built.SnapshotPath}
	if err != nil {
		return out, fmt.Errorf("build: %w", err)
	}
	return out, nil
}
