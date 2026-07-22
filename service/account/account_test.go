package account

import "testing"

func TestNewBroker_OandaKnownName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := NewBroker("oanda", "practice", "some-token")
	if err != nil {
		t.Fatalf("expected no error constructing oanda broker, got %v", err)
	}
}

func TestNewBroker_OandaMissingTokenErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := NewBroker("oanda", "practice", "")
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestNewBroker_UnknownBrokerErrors(t *testing.T) {
	_, err := NewBroker("alpaca", "practice", "tok")
	if err == nil {
		t.Fatal("expected error for unknown broker")
	}
}

func TestIsKnownBroker(t *testing.T) {
	if !IsKnownBroker("oanda") {
		t.Error("expected oanda to be known")
	}
	if IsKnownBroker("alpaca") {
		t.Error("expected alpaca to be unknown")
	}
}

func TestDefaultAccountID_ConfiguredWins(t *testing.T) {
	refs := []AccountRef{{ID: "acc-1"}, {ID: "acc-2"}}
	if got := DefaultAccountID(refs, "acc-2"); got != "acc-2" {
		t.Errorf("got %q, want acc-2", got)
	}
}

func TestDefaultAccountID_FallsBackToFirstRef(t *testing.T) {
	refs := []AccountRef{{ID: "acc-1"}, {ID: "acc-2"}}
	if got := DefaultAccountID(refs, ""); got != "acc-1" {
		t.Errorf("got %q, want acc-1", got)
	}
}

func TestDefaultAccountID_EmptyRefsAndConfiguredReturnsEmpty(t *testing.T) {
	if got := DefaultAccountID(nil, ""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
