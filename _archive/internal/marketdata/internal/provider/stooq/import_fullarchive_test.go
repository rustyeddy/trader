//go:build fullarchive

package stooq

// This file is excluded from normal `go test ./...` / `make check` by
// the fullarchive build tag (mirroring oanda's own
// inventory_fullarchive_test.go): importing and inspecting a real,
// multi-decade Stooq export is an operator action, not a CI assertion.

import (
	"context"
	"os"
	"testing"
)

// fullArchiveCSVPath names the real Stooq CSV export
// TestImportFullArchive imports. It intentionally has no real default
// and ships empty, for the identical reason
// oanda/inventory_fullarchive_test.go's fullArchiveRoot does: domain
// code under marketdata/ must not read a path from the environment or
// a flag, and this package's internal/ visibility means the test
// cannot be relocated outside the marketdata/ tree either. An operator
// wanting to run this test edits this constant locally (for example to
// "/srv/trading/data/raw/stooq/spy_us_d.csv") and fullArchiveSymbol to
// match, then runs:
//
//	go test -tags fullarchive ./marketdata/internal/provider/stooq/... \
//	    -run TestImportFullArchive -v
//
// The edit is never committed; the test skips itself whenever the
// constant is empty, which is always true for a fresh checkout.
const fullArchiveCSVPath = ""
const fullArchiveSymbol = "SPY"

// TestImportFullArchive imports fullArchiveCSVPath into a temporary raw
// root, re-inspects it, and prints a summary an operator can read. It
// skips (not fails) when fullArchiveCSVPath is empty or not a readable
// file.
func TestImportFullArchive(t *testing.T) {
	if fullArchiveCSVPath == "" {
		t.Skip("fullArchiveCSVPath is empty; edit the constants in this file to point at a real Stooq CSV export to run this test")
	}
	if info, err := os.Stat(fullArchiveCSVPath); err != nil || info.IsDir() {
		t.Skipf("fullArchiveCSVPath %q is not a readable file: %v", fullArchiveCSVPath, err)
	}

	rawRoot := t.TempDir()
	ctx := context.Background()

	result, err := Import(ctx, fullArchiveCSVPath, rawRoot, fullArchiveSymbol)
	if err != nil {
		t.Fatalf("Import(%q): %v", fullArchiveCSVPath, err)
	}
	t.Logf("imported %d rows across %d monthly partitions, %s through %s",
		result.RowsImported, result.MonthsWritten, result.FirstDate.Format("2006-01-02"), result.LastDate.Format("2006-01-02"))

	inv, err := Inspect(ctx, rawRoot)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	var ok, unreadable, malformed, totalRows int
	for _, p := range inv.Partitions {
		switch p.Status {
		case PartitionStatusOK:
			ok++
			totalRows += p.RowCount
		case PartitionStatusUnreadable:
			unreadable++
			t.Logf("unreadable: %s: %v", p.Path, p.Err)
		case PartitionStatusMalformed:
			malformed++
			t.Logf("malformed: %s: %v", p.Path, p.Err)
		}
	}
	t.Logf("re-inspected: %d partitions ok (%d rows), %d unreadable, %d malformed", ok, totalRows, unreadable, malformed)

	if unreadable > 0 || malformed > 0 {
		t.Errorf("archive has %d unreadable and %d malformed partitions after import; see log for details", unreadable, malformed)
	}
	if totalRows != result.RowsImported {
		t.Errorf("re-inspected row count %d does not match imported row count %d", totalRows, result.RowsImported)
	}
}
