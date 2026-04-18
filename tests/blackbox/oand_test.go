//go:build blackbox

package blackbox

import (
	"testing"
)

func TestDataCommand_HelpListsSubcommands(t *testing.T) {
	out, _ := run(t, "data", "--help")

	if !contains(out, "Download tick data and build candles") {
		t.Fatalf("expected data help text, got:\n%s", out)
	}
	if !contains(out, "download-ticks") || !contains(out, "build-candles") || !contains(out, "sync") {
		t.Fatalf("expected data subcommands in help, got:\n%s", out)
	}
}
