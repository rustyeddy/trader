package trader

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLotBookAddDeleteLen_Phase2(t *testing.T) {
	t.Parallel()

	lb := &LotBook{}
	assert.Equal(t, 0, lb.Len())

	l1 := &Lot{TradeCommon: &TradeCommon{ID: "p1"}}
	l2 := &Lot{TradeCommon: &TradeCommon{ID: "p2"}}
	lb.Add(l1)
	lb.Add(l2)
	assert.Equal(t, 2, lb.Len())

	lb.Delete("p1")
	assert.Equal(t, 1, lb.Len())

	lb.Delete("missing")
	assert.Equal(t, 1, lb.Len())
}

func TestLotBookAll_ReturnsCopyNotAlias_Phase2(t *testing.T) {
	t.Parallel()

	lb := &LotBook{}
	l1 := &Lot{TradeCommon: &TradeCommon{ID: "p1"}}
	lb.Add(l1)

	copyMap := lb.All()
	require.Len(t, copyMap, 1)
	delete(copyMap, "p1")

	assert.Equal(t, 1, lb.Len())
	assert.NotNil(t, lb.All()["p1"])
}

func TestLotBookRange_VisitsAllAndStopsOnError_Phase2(t *testing.T) {
	t.Parallel()

	lb := &LotBook{}
	lb.Add(&Lot{TradeCommon: &TradeCommon{ID: "p1"}})
	lb.Add(&Lot{TradeCommon: &TradeCommon{ID: "p2"}})

	seen := map[string]bool{}
	err := lb.Range(func(lot *Lot) error {
		seen[lot.ID] = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, seen["p1"])
	assert.True(t, seen["p2"])

	stopErr := errors.New("stop")
	err = lb.Range(func(lot *Lot) error {
		if lot.ID == "p1" || lot.ID == "p2" {
			return stopErr
		}
		return nil
	})
	assert.ErrorIs(t, err, stopErr)
}

func TestLotBookAllNilMap_Phase2(t *testing.T) {
	t.Parallel()

	lb := &LotBook{}
	assert.Nil(t, lb.All())
}
