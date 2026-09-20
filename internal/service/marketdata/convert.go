package marketdata

import (
	"context"
	"fmt"
	"log/slog"
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
	if req.Symbol == "" {
		return ConvertResponse{}, fmt.Errorf("%w: symbol is required", ErrInvalidRequest)
	}
	defer func() {
		s.logOutcome(ctx, slog.LevelInfo, "convert completed", "convert failed", req.DatasetRequest, err,
			"rows_imported", resp.Import.RowsImported, "published_partitions", len(resp.Build.Result.Published))
	}()
	imported, err := s.manager.ImportStooqArchive(ctx, req.ArchivePath, req.Symbol)
	if err != nil {
		return ConvertResponse{}, err
	}
	build, err := s.Build(ctx, BuildRequest{DatasetRequest: req.DatasetRequest})
	resp = ConvertResponse{Import: imported, Build: build}
	return resp, err
}
