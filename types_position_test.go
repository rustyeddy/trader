package trader

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPositionsAddDeleteLen_Phase2(t *testing.T) {
	t.Parallel()

	ps := &Positions{}
	assert.Equal(t, 0, ps.Len())

	p1 := &Position{TradeCommon: &TradeCommon{ID: "p1"}}
	p2 := &Position{TradeCommon: &TradeCommon{ID: "p2"}}
	ps.Add(p1)
	ps.Add(p2)
	assert.Equal(t, 2, ps.Len())

	ps.Delete("p1")
	assert.Equal(t, 1, ps.Len())

	ps.Delete("missing")
	assert.Equal(t, 1, ps.Len())
}

func TestPositionsPositions_ReturnsCopyNotAlias_Phase2(t *testing.T) {
	t.Parallel()

	ps := &Positions{}
	p1 := &Position{TradeCommon: &TradeCommon{ID: "p1"}}
	ps.Add(p1)

	copyMap := ps.Positions()
	require.Len(t, copyMap, 1)
	delete(copyMap, "p1")

	assert.Equal(t, 1, ps.Len())
	assert.NotNil(t, ps.Positions()["p1"])
}

func TestPositionsRange_VisitsAllAndStopsOnError_Phase2(t *testing.T) {
	t.Parallel()

	ps := &Positions{}
	ps.Add(&Position{TradeCommon: &TradeCommon{ID: "p1"}})
	ps.Add(&Position{TradeCommon: &TradeCommon{ID: "p2"}})

	seen := map[string]bool{}
	err := ps.Range(func(pos *Position) error {
		seen[pos.ID] = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, seen["p1"])
	assert.True(t, seen["p2"])

	stopErr := errors.New("stop")
	err = ps.Range(func(pos *Position) error {
		if pos.ID == "p1" || pos.ID == "p2" {
			return stopErr
		}
		return nil
	})
	assert.ErrorIs(t, err, stopErr)
}

func TestPositionsPositionsNilMap_Phase2(t *testing.T) {
	t.Parallel()

	ps := &Positions{}
	assert.Nil(t, ps.Positions())
}

func TestPositionTriggerMethods_CurrentBehaviorFalse_Phase2(t *testing.T) {
	t.Parallel()

	pos := &Position{TradeCommon: &TradeCommon{Side: Long, Units: 1}, FillPrice: PriceFromFloat(1.2)}
	assert.False(t, pos.TriggerStopLoss(PriceFromFloat(1.1)))
	assert.False(t, pos.triggerTakeProfit(PriceFromFloat(1.3)))
}
