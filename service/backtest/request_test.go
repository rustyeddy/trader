package backtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/num"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
)

func TestRunRequest_Validate_RequiresStrategy(t *testing.T) {
	req := validRunRequest(t)
	req.Strategy = nil
	require.ErrorIs(t, req.Validate(), svcbacktest.ErrInvalidRequest)
}

func TestRunRequest_Validate_RequiresSpan(t *testing.T) {
	req := validRunRequest(t)
	req.Span = zeroSpan(t)
	require.ErrorIs(t, req.Validate(), svcbacktest.ErrInvalidRequest)
}

func TestRunRequest_Validate_RequiresValidStartingCapital(t *testing.T) {
	req := validRunRequest(t)
	req.StartingCapital = num.Money{}
	require.ErrorIs(t, req.Validate(), svcbacktest.ErrInvalidRequest)
}

func TestRunRequest_Validate_RequiresPositiveStartingCapital(t *testing.T) {
	req := validRunRequest(t)
	req.StartingCapital = num.MustParseMoney("0", num.MustParseCurrency("USD"))
	require.ErrorIs(t, req.Validate(), svcbacktest.ErrInvalidRequest)

	req.StartingCapital = num.MustParseMoney("-100", num.MustParseCurrency("USD"))
	require.ErrorIs(t, req.Validate(), svcbacktest.ErrInvalidRequest)
}

func TestRunRequest_Validate_AcceptsWellFormedRequest(t *testing.T) {
	req := validRunRequest(t)
	assert.NoError(t, req.Validate())
}
