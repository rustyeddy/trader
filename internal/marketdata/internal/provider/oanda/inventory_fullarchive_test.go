//go:build fullarchive

package oanda

// This file is excluded from normal `go test ./...` / `make check` by the
// fullarchive build tag (issue #75, ADR-020): inspecting the real,
// multi-decade preserved raw archive is an operator action, not a CI
// assertion — it can take a while, and its outcome depends on the state
// of a real archive on disk, not a fixture.

import (
	"context"
	"os"
	"testing"
)

// fullArchiveRoot names the raw OANDA archive TestInspectFullArchive
// inspects. It intentionally has no real default and ships empty: domain
// code under marketdata/ must not read a path from the environment or a
// command-line flag (config/arch_test.go enforces this mechanically, and
// os.Getenv or "flag" here would trip it), and marketdata/internal's Go
// visibility rule means this test cannot be relocated outside the
// marketdata/ tree to reach an exempt location either — oanda.Inspect is
// only reachable from within marketdata/. An operator wanting to run
// this test edits this one constant locally to point at a real archive,
// then runs:
//
//	go test -tags fullarchive ./marketdata/internal/provider/oanda/... \
//	    -run TestInspectFullArchive -v
//
// The edit is never committed; the test skips itself whenever the
// constant is empty, which is always true for a fresh checkout.
const fullArchiveRoot = ""

// TestInspectFullArchive runs Inspect against fullArchiveRoot and prints
// a summary an operator can read. It skips (not fails) when
// fullArchiveRoot is empty or not a readable directory.
func TestInspectFullArchive(t *testing.T) {
	if fullArchiveRoot == "" {
		t.Skip("fullArchiveRoot is empty; edit the constant in this file to point at a real raw OANDA archive to run this test")
	}
	if info, err := os.Stat(fullArchiveRoot); err != nil || !info.IsDir() {
		t.Skipf("fullArchiveRoot %q is not a readable directory: %v", fullArchiveRoot, err)
	}

	inv, err := Inspect(context.Background(), fullArchiveRoot)
	if err != nil {
		t.Fatalf("Inspect(%q): %v", fullArchiveRoot, err)
	}

	var ok, unreadable, malformed int
	var incomplete, duplicates int
	for _, p := range inv.Partitions {
		switch p.Status {
		case PartitionStatusOK:
			ok++
			incomplete += p.IncompleteCount
			duplicates += len(p.DuplicateTimes)
		case PartitionStatusUnreadable:
			unreadable++
			t.Logf("unreadable: %s: %v", p.Path, p.Err)
		case PartitionStatusMalformed:
			malformed++
			t.Logf("malformed: %s: %v", p.Path, p.Err)
		}
	}

	t.Logf("root: %s", inv.Root)
	t.Logf("partitions: %d ok, %d unreadable, %d malformed", ok, unreadable, malformed)
	t.Logf("rows: %d incomplete, %d duplicate timestamps (across all ok partitions)", incomplete, duplicates)
	t.Logf("skipped entries: %d", len(inv.Skipped))
	t.Logf("month gaps: %d", len(inv.Gaps))
	for _, g := range inv.Gaps {
		t.Logf("  gap: %s %s %04d-%02d", g.Symbol, g.Interval, g.Year, int(g.Month))
	}

	if unreadable > 0 || malformed > 0 {
		t.Errorf("archive has %d unreadable and %d malformed partitions; see log for details",
			unreadable, malformed)
	}
}
