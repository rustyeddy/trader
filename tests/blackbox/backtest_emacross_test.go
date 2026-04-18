//go:build blackbox

package blackbox

import (
	"testing"
)

func TestBacktestEmaCross_HelpListsCurrentFlags(t *testing.T) {
	out, _ := run(t, "backtest", "ema-cross", "--help")

	if !contains(out, "Run EMA cross strategy") {
		t.Fatalf("expected ema-cross help text, got:\n%s", out)
	}
	if !contains(out, "--risk-pct") {
		t.Fatalf("expected --risk-pct flag in help, got:\n%s", out)
	}
	if !contains(out, "--stop") || !contains(out, "--take") {
		t.Fatalf("expected --stop/--take flags in help, got:\n%s", out)
	}
}
