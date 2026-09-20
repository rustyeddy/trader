package marketdata

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/internal/marketdata/internal/provider/stooq"
)

// StooqImportResult summarizes a native Stooq archive import without
// exposing provider-internal record types across the Manager boundary.
type StooqImportResult struct {
	MonthsWritten int
	RowsImported  int
	FirstDate     time.Time
	LastDate      time.Time
}

// ImportStooqArchive imports one native Stooq archive file into the
// manager's configured managed raw root. The caller owns archive extraction;
// this method only adapts the source into Trader's existing raw partitions.
func (m *Manager) ImportStooqArchive(ctx context.Context, path, symbol string) (StooqImportResult, error) {
	if m.providerName != "stooq" {
		return StooqImportResult{}, fmt.Errorf("marketdata: Stooq archive import requires provider stooq, got %q", m.providerName)
	}
	if m.rawRoot == "" {
		return StooqImportResult{}, fmt.Errorf("marketdata: Stooq archive import requires a raw root")
	}
	result, err := stooq.ImportArchive(ctx, path, m.rawRoot, symbol)
	return StooqImportResult{MonthsWritten: result.MonthsWritten, RowsImported: result.RowsImported, FirstDate: result.FirstDate, LastDate: result.LastDate}, err
}
