package account

import (
	"context"
	"errors"
	"testing"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBroker implements brokers.Broker with just enough behavior for these
// tests; every method not exercised here panics if called.
type fakeBroker struct {
	accounts    []oanda.AccountRef
	accountsErr error
	summaries   map[string]*oanda.AccountSummary
	summaryErrs map[string]error
}

func (f *fakeBroker) GetAccountSummary(ctx context.Context, accountID string) (*oanda.AccountSummary, error) {
	if err, ok := f.summaryErrs[accountID]; ok {
		return nil, err
	}
	return f.summaries[accountID], nil
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

func TestGetAccounts_ReturnsBrokerAccounts(t *testing.T) {
	f := &fakeBroker{accounts: []oanda.AccountRef{{ID: "acc-1"}, {ID: "acc-2"}}}
	refs, err := GetAccounts(context.Background(), f)
	require.NoError(t, err)
	assert.Equal(t, f.accounts, refs)
}

func TestGetAccounts_NilBrokerErrors(t *testing.T) {
	_, err := GetAccounts(context.Background(), nil)
	require.Error(t, err)
}

func TestGetAccounts_BrokerErrorPropagates(t *testing.T) {
	f := &fakeBroker{accountsErr: errors.New("boom")}
	_, err := GetAccounts(context.Background(), f)
	require.Error(t, err)
}

func TestGetAccountSummary_ExplicitIDs(t *testing.T) {
	f := &fakeBroker{
		summaries: map[string]*oanda.AccountSummary{
			"acc-1": {ID: "acc-1", Balance: 100},
		},
	}
	results, err := GetAccountSummary(context.Background(), f, []string{"acc-1"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "acc-1", results[0].ID)
	assert.NoError(t, results[0].Err)
	assert.Equal(t, f.summaries["acc-1"], results[0].Summary)
}

func TestGetAccountSummary_EmptyIDsMeansAll(t *testing.T) {
	f := &fakeBroker{
		accounts: []oanda.AccountRef{{ID: "acc-1"}, {ID: "acc-2"}},
		summaries: map[string]*oanda.AccountSummary{
			"acc-1": {ID: "acc-1"},
			"acc-2": {ID: "acc-2"},
		},
	}
	results, err := GetAccountSummary(context.Background(), f, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "acc-1", results[0].ID)
	assert.Equal(t, "acc-2", results[1].ID)
}

func TestGetAccountSummary_PerAccountErrorTolerated(t *testing.T) {
	f := &fakeBroker{
		summaryErrs: map[string]error{"acc-1": errors.New("rate limited")},
	}
	results, err := GetAccountSummary(context.Background(), f, []string{"acc-1"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Nil(t, results[0].Summary)
	assert.EqualError(t, results[0].Err, "rate limited")
}

func TestGetAccountSummary_NilBrokerErrors(t *testing.T) {
	_, err := GetAccountSummary(context.Background(), nil, nil)
	require.Error(t, err)
}

func TestGetAccountSummary_AllAccountsDiscoveryErrorPropagates(t *testing.T) {
	f := &fakeBroker{accountsErr: errors.New("boom")}
	_, err := GetAccountSummary(context.Background(), f, nil)
	require.Error(t, err)
}
