package account

import "testing"

func TestDefaultSelection_NoneSetReturnsZeroValue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sel, err := DefaultSelection()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.Broker != "" || sel.AccountID != "" {
		t.Fatalf("expected zero-value selection, got %+v", sel)
	}
}

func TestSetDefaultThenDefaultSelection_RoundTrips(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SetDefault("oanda", "acc-123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sel, err := DefaultSelection()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.Broker != "oanda" || sel.AccountID != "acc-123" {
		t.Fatalf("expected oanda/acc-123, got %+v", sel)
	}
}
