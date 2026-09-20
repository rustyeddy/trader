package account

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReferenceValid(t *testing.T) {
	accountID := mustAccountID(t)
	r, err := NewReference(Reference{AccountID: accountID, Broker: "OANDA"})
	require.NoError(t, err)
	assert.Equal(t, accountID, r.AccountID)
	assert.Equal(t, "OANDA", r.Broker)
}

func TestNewReferenceZeroAccountID(t *testing.T) {
	_, err := NewReference(Reference{Broker: "OANDA"})
	require.ErrorIs(t, err, ErrInvalidReference)
}

func TestNewReferenceEmptyBroker(t *testing.T) {
	_, err := NewReference(Reference{AccountID: mustAccountID(t)})
	require.ErrorIs(t, err, ErrInvalidReference)
}
