package marketdata

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/rustyeddy/trader/marketdata"
)

// Convert imports one already-downloaded provider archive and then builds
// canonical data through the same Manager Plan/Build path used elsewhere.
func (s *Service) Convert(ctx context.Context, req ConvertRequest) (resp ConvertResponse, err error) {
	if err := validateConvertRequest(req); err != nil {
		return ConvertResponse{}, err
	}
	if req.ArchivePath == "" {
		return ConvertResponse{}, fmt.Errorf("%w: archive path is required", ErrInvalidRequest)
	}
	if req.Interval != marketdata.D1 {
		return ConvertResponse{}, fmt.Errorf("%w: stooq conversion supports only D1", ErrInvalidRequest)
	}
	defer func() {
		s.logOutcome(ctx, slog.LevelInfo, "convert completed", "convert failed", req.DatasetRequest, err,
			"rows_imported", resp.Import.RowsImported, "published_partitions", len(resp.Build.Result.Published))
	}()
	imported, err := s.manager.ImportStooqArchive(ctx, req.ArchivePath, req.Instrument)
	if err != nil {
		return ConvertResponse{}, err
	}
	if req.Range.Start().IsZero() && req.Range.End().IsZero() {
		end := imported.LastDate.UTC().AddDate(0, 0, 1)
		req.Range, err = marketdata.NewTimeRange(imported.FirstDate.UTC(), end)
	} else {
		start := req.Range.Start()
		end := req.Range.End()
		if start.Before(imported.FirstDate) {
			start = imported.FirstDate
		}
		lastEnd := imported.LastDate.UTC().AddDate(0, 0, 1)
		if end.After(lastEnd) {
			end = lastEnd
		}
		req.Range, err = marketdata.NewTimeRange(start, end)
	}
	if err != nil {
		return ConvertResponse{}, fmt.Errorf("%w: source range does not overlap requested range: %v", ErrInvalidRequest, err)
	}
	build, err := s.Build(ctx, BuildRequest{DatasetRequest: req.DatasetRequest, Force: req.Force})
	resp = ConvertResponse{Import: imported, Build: build}
	return resp, err
}

func validateConvertRequest(req ConvertRequest) error {
	if req.Instrument.IsZero() {
		return fmt.Errorf("%w: instrument is zero", ErrInvalidRequest)
	}
	if !req.Interval.Valid() {
		return fmt.Errorf("%w: interval is invalid", ErrInvalidRequest)
	}
	if (req.Range.Start().IsZero()) != (req.Range.End().IsZero()) {
		return fmt.Errorf("%w: range must be fully specified or omitted", ErrInvalidRequest)
	}
	if !req.Range.Start().IsZero() && !req.Range.End().After(req.Range.Start()) {
		return fmt.Errorf("%w: range is invalid", ErrInvalidRequest)
	}
	return nil
}
