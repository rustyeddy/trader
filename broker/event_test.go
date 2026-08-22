package broker_test

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseEvent(t *testing.T) broker.Event {
	t.Helper()
	return broker.Event{
		Metadata: id.Metadata{
			EventID:   mustEventID(t),
			Timestamp: time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
		},
		ObservedAt: time.Date(2026, 1, 2, 12, 0, 0, 100, time.UTC),
		Sequence:   1,
	}
}

func TestNewEventOrderValid(t *testing.T) {
	accountID := mustAccountID(t)
	o := mustSubmittedOrder(t, accountID)

	e := baseEvent(t)
	e.Kind = broker.EventKindOrder
	e.Order = &o

	got, err := broker.NewEvent(e)
	require.NoError(t, err)
	assert.Equal(t, broker.EventKindOrder, got.Kind)
	require.NotNil(t, got.Order)
	assert.Nil(t, got.Fill)
	assert.Nil(t, got.Account)
	assert.Nil(t, got.Status)
}

func TestNewEventFillValid(t *testing.T) {
	accountID := mustAccountID(t)
	o := mustSubmittedOrder(t, accountID)
	f, err := order.NewFill(order.Fill{
		FillID:        mustFillID(t),
		OrderID:       o.Request.OrderID,
		BrokerOrderID: o.BrokerOrderID,
		AccountID:     accountID,
		Listing:       o.Request.Listing,
		Side:          o.Request.Side,
		Price:         num.MustParsePrice("1.10000"),
		Quantity:      num.MustParseQuantity("1000"),
	})
	require.NoError(t, err)

	e := baseEvent(t)
	e.Kind = broker.EventKindFill
	e.Fill = &f

	got, err := broker.NewEvent(e)
	require.NoError(t, err)
	assert.Equal(t, broker.EventKindFill, got.Kind)
	require.NotNil(t, got.Fill)
}

func TestNewEventAccountValid(t *testing.T) {
	accountID := mustAccountID(t)
	snap := mustSnapshot(t, accountID, "sim")

	e := baseEvent(t)
	e.Kind = broker.EventKindAccount
	e.Account = &snap

	got, err := broker.NewEvent(e)
	require.NoError(t, err)
	assert.Equal(t, broker.EventKindAccount, got.Kind)
	require.NotNil(t, got.Account)
}

func TestNewEventStatusValid(t *testing.T) {
	status := broker.AccountStatusActive

	e := baseEvent(t)
	e.Kind = broker.EventKindStatus
	e.Status = &status

	got, err := broker.NewEvent(e)
	require.NoError(t, err)
	assert.Equal(t, broker.EventKindStatus, got.Kind)
	require.NotNil(t, got.Status)
	assert.Equal(t, broker.AccountStatusActive, *got.Status)
}

func TestNewEventStatusUnknownIsLegal(t *testing.T) {
	status := broker.AccountStatusUnknown

	e := baseEvent(t)
	e.Kind = broker.EventKindStatus
	e.Status = &status

	_, err := broker.NewEvent(e)
	require.NoError(t, err)
}

func TestNewEventRejectsZeroEventID(t *testing.T) {
	status := broker.AccountStatusActive
	e := baseEvent(t)
	e.Metadata.EventID = id.EventID{}
	e.Kind = broker.EventKindStatus
	e.Status = &status

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsZeroTimestamp(t *testing.T) {
	status := broker.AccountStatusActive
	e := baseEvent(t)
	e.Metadata.Timestamp = time.Time{}
	e.Kind = broker.EventKindStatus
	e.Status = &status

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsZeroObservedAt(t *testing.T) {
	status := broker.AccountStatusActive
	e := baseEvent(t)
	e.ObservedAt = time.Time{}
	e.Kind = broker.EventKindStatus
	e.Status = &status

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsZeroSequence(t *testing.T) {
	status := broker.AccountStatusActive
	e := baseEvent(t)
	e.Sequence = 0
	e.Kind = broker.EventKindStatus
	e.Status = &status

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsInvalidKind(t *testing.T) {
	status := broker.AccountStatusActive
	e := baseEvent(t)
	e.Kind = broker.EventKindUnknown
	e.Status = &status

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsNoPayload(t *testing.T) {
	e := baseEvent(t)
	e.Kind = broker.EventKindStatus

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsMultiplePayloads(t *testing.T) {
	accountID := mustAccountID(t)
	o := mustSubmittedOrder(t, accountID)
	status := broker.AccountStatusActive

	e := baseEvent(t)
	e.Kind = broker.EventKindOrder
	e.Order = &o
	e.Status = &status

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsKindPayloadMismatch(t *testing.T) {
	accountID := mustAccountID(t)
	o := mustSubmittedOrder(t, accountID)

	e := baseEvent(t)
	e.Kind = broker.EventKindFill
	e.Order = &o // wrong field for EventKindFill

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsOrderKindWithoutOrderPayload(t *testing.T) {
	invalid := broker.AccountStatus(0)
	e := baseEvent(t)
	e.Kind = broker.EventKindOrder
	e.Status = &invalid // populated == 1, but wrong field for EventKindOrder

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsAccountKindWithoutAccountPayload(t *testing.T) {
	invalid := broker.AccountStatus(0)
	e := baseEvent(t)
	e.Kind = broker.EventKindAccount
	e.Status = &invalid // populated == 1, but wrong field for EventKindAccount

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsStatusKindWithoutStatusPayload(t *testing.T) {
	accountID := mustAccountID(t)
	o := mustSubmittedOrder(t, accountID)
	e := baseEvent(t)
	e.Kind = broker.EventKindStatus
	e.Order = &o // populated == 1, but wrong field for EventKindStatus

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsInvalidOrder(t *testing.T) {
	e := baseEvent(t)
	e.Kind = broker.EventKindOrder
	invalid := order.Order{} // fails order.NewOrder's validation
	e.Order = &invalid

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsInvalidFill(t *testing.T) {
	e := baseEvent(t)
	e.Kind = broker.EventKindFill
	invalid := order.Fill{} // fails order.NewFill's validation
	e.Fill = &invalid

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestNewEventRejectsInvalidAccountStatus(t *testing.T) {
	invalid := broker.AccountStatus(200)
	e := baseEvent(t)
	e.Kind = broker.EventKindStatus
	e.Status = &invalid

	_, err := broker.NewEvent(e)
	require.ErrorIs(t, err, broker.ErrInvalidEvent)
}

func TestEventKindStringUnknownValue(t *testing.T) {
	assert.Equal(t, "EventKind(99)", broker.EventKind(99).String())
	assert.Equal(t, "order", broker.EventKindOrder.String())
	assert.Equal(t, "fill", broker.EventKindFill.String())
	assert.Equal(t, "account", broker.EventKindAccount.String())
	assert.Equal(t, "status", broker.EventKindStatus.String())
}

func TestAccountStatusStringUnknownValue(t *testing.T) {
	assert.Equal(t, "AccountStatus(99)", broker.AccountStatus(99).String())
	assert.Equal(t, "unknown", broker.AccountStatusUnknown.String())
	assert.Equal(t, "active", broker.AccountStatusActive.String())
	assert.Equal(t, "degraded", broker.AccountStatusDegraded.String())
	assert.Equal(t, "disconnected", broker.AccountStatusDisconnected.String())
}

// mustSubmittedOrder returns a valid, StatusWorking order.Order for
// accountID, standing in for what a broker would have produced from
// Account.Submit.
func mustSubmittedOrder(t *testing.T, accountID id.AccountID) order.Order {
	t.Helper()
	req := mustRequest(t, accountID)
	accepted := req.Quantity
	o, err := order.NewOrder(order.Order{
		Request:          req,
		BrokerOrderID:    "fake-" + req.OrderID.String(),
		AcceptedQuantity: &accepted,
		Status:           order.StatusWorking,
	})
	require.NoError(t, err)
	return o
}
