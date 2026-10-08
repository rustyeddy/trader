package marketdata

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/marketdata/internal/provider/stooq"
)

// StooqImportResult summarizes a native Stooq archive import without
// exposing provider-internal record types across the Manager boundary.
type StooqImportResult struct {
	MonthsWritten int
	RowsImported  int
	FirstDate     time.Time
	LastDate      time.Time
	// RowsAdded counts the rows new to the raw archive: every row for
	// ImportStooqArchive, only rows after the previous last raw date for
	// MergeStooqArchive.
	RowsAdded int
	// FullReimport reports that MergeStooqArchive found the export
	// disagreed with the existing raw history and re-imported the symbol.
	FullReimport bool
}

// ImportStooqArchive imports one native Stooq archive file into the
// manager's configured managed raw root. The caller owns archive extraction;
// this method only adapts the source into Trader's existing raw partitions.
func (m *Manager) ImportStooqArchive(ctx context.Context, path string, id instrument.ID) (StooqImportResult, error) {
	return m.importStooqArchive(ctx, path, id, stooq.ImportArchive)
}

// MergeStooqArchive is the incremental form of ImportStooqArchive: it adds
// only the rows after the symbol's last raw date, leaves earlier
// partitions untouched, and never deletes existing rows. If the export
// disagrees with the stored history it re-imports the symbol in full
// (StooqImportResult.FullReimport), or fails with ErrStooqAdjustmentMismatch
// when the export is too short to do so safely. See stooq.MergeArchive.
func (m *Manager) MergeStooqArchive(ctx context.Context, path string, id instrument.ID) (StooqImportResult, error) {
	return m.importStooqArchive(ctx, path, id, stooq.MergeArchive)
}

// ErrStooqAdjustmentMismatch reports a MergeStooqArchive export that
// disagrees with the existing raw history and cannot safely replace it.
var ErrStooqAdjustmentMismatch = stooq.ErrAdjustmentMismatch

func (m *Manager) importStooqArchive(ctx context.Context, path string, id instrument.ID,
	do func(ctx context.Context, path, rawRoot, symbol string) (stooq.ImportResult, error),
) (StooqImportResult, error) {
	if !m.configured() {
		return StooqImportResult{}, fmt.Errorf("marketdata: Stooq archive import: %w: manager is not configured", ErrInvalidConfig)
	}
	if m.providerName != "stooq" {
		return StooqImportResult{}, fmt.Errorf("marketdata: Stooq archive import requires provider stooq, got %q", m.providerName)
	}
	if m.rawRoot == "" {
		return StooqImportResult{}, fmt.Errorf("marketdata: Stooq archive import requires a raw root")
	}
	listing, err := m.resolver.ResolveInstrument(id, m.providerName, "")
	if err != nil {
		return StooqImportResult{}, fmt.Errorf("marketdata: Stooq archive import: resolve listing: %w", err)
	}
	if err := m.writeLock.acquire(ctx); err != nil {
		return StooqImportResult{}, err
	}
	defer m.writeLock.release()
	result, err := do(ctx, path, m.rawRoot, listing.Symbol())
	return StooqImportResult{MonthsWritten: result.MonthsWritten, RowsImported: result.RowsImported, FirstDate: result.FirstDate, LastDate: result.LastDate, RowsAdded: result.RowsAdded, FullReimport: result.FullReimport}, err
}
