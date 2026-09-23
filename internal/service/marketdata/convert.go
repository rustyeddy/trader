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
	if err := req.Validate(); err != nil {
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
	build, err := s.Build(ctx, BuildRequest{DatasetRequest: req.DatasetRequest})
	resp = ConvertResponse{Import: imported, Build: build}
	return resp, err
}
