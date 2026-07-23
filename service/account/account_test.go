package account

import (
	"context"
	"errors"
	"testing"

	"github.com/rustyeddy/trader/brokers/oanda"
)

// fakeBroker implements brokers.Broker with just enough behavior for
// ResolveAccountID's tests; every method not exercised here panics.
type fakeBroker struct {
	accounts    []oanda.AccountRef
	accountsErr error
}

func (f *fakeBroker) GetAccountSummary(ctx context.Context, accountID string) (*oanda.AccountSummary, error) {
	panic("not implemented")
}
func (f *fakeBroker) GetAccountDetails(ctx context.Context, accountID string) (*oanda.AccountDetails, error) {
	panic("not implemented")
}
func (f *fakeBroker) GetAccountChanges(ctx context.Context, accountID string, sinceID int64) (*oanda.AccountChangesResult, error) {
	panic("not implemented")
}
func (f *fakeBroker) GetAccounts(ctx context.Context) ([]oanda.AccountRef, error) {
	return f.accounts, f.accountsErr
}
func (f *fakeBroker) GetTransactions(ctx context.Context, accountID string, sinceID int64) ([]oanda.Transaction, int64, error) {
	panic("not implemented")
}
func (f *fakeBroker) StreamTransactions(ctx context.Context, accountID string, opts oanda.StreamOptions) (<-chan oanda.TxEvent, error) {
	panic("not implemented")
}
func (f *fakeBroker) GetOpenTrades(ctx context.Context, accountID string) ([]oanda.OpenTrade, error) {
	panic("not implemented")
}
func (f *fakeBroker) CloseTrade(ctx context.Context, accountID, tradeID string, units int64) (*oanda.CloseTradeResult, error) {
	panic("not implemented")
}
func (f *fakeBroker) UpdateTradeStop(ctx context.Context, accountID, tradeID string, stopPrice, takePrice float64) error {
	panic("not implemented")
}
func (f *fakeBroker) SubmitMarketOrder(ctx context.Context, accountID, instrument string, units int64, stopPrice float64) (*oanda.OrderResult, error) {
	panic("not implemented")
}

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

func TestResolveAccountID_ExplicitIDWinsWithoutQuerying(t *testing.T) {
	f := &fakeBroker{accountsErr: errors.New("should not be called")}
	got, err := ResolveAccountID(context.Background(), f, "acc-explicit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "acc-explicit" {
		t.Errorf("got %q, want acc-explicit", got)
	}
}

func TestResolveAccountID_SingleAccountAutoResolves(t *testing.T) {
	f := &fakeBroker{accounts: []oanda.AccountRef{{ID: "only-acc"}}}
	got, err := ResolveAccountID(context.Background(), f, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "only-acc" {
		t.Errorf("got %q, want only-acc", got)
	}
}

func TestResolveAccountID_NoAccountsErrors(t *testing.T) {
	f := &fakeBroker{}
	_, err := ResolveAccountID(context.Background(), f, "")
	if err == nil {
		t.Fatal("expected error for no accounts")
	}
}

func TestResolveAccountID_MultipleAccountsReturnsAmbiguousError(t *testing.T) {
	f := &fakeBroker{accounts: []oanda.AccountRef{{ID: "acc-1"}, {ID: "acc-2"}}}
	_, err := ResolveAccountID(context.Background(), f, "")
	var amb AmbiguousAccountError
	if !errors.As(err, &amb) {
		t.Fatalf("expected AmbiguousAccountError, got %v", err)
	}
	if len(amb.Accounts) != 2 {
		t.Errorf("expected 2 candidate accounts, got %v", amb.Accounts)
	}
}

func TestResolveAccountID_DiscoveryErrorPropagates(t *testing.T) {
	f := &fakeBroker{accountsErr: errors.New("boom")}
	_, err := ResolveAccountID(context.Background(), f, "")
	if err == nil {
		t.Fatal("expected error")
	}
}
