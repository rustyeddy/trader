package execution

import (
	"errors"
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLotBookAddDeleteLen_Phase2 verifies expected behavior for this component.
func TestLotBookAddDeleteLen_Phase2(t *testing.T) {
	t.Parallel()

	lb := &LotBook{}
	assert.Equal(t, 0, lb.Len())

	l1 := &Lot{TradeCommon: &TradeCommon{ID: "p1"}}
	l2 := &Lot{TradeCommon: &TradeCommon{ID: "p2"}}
	require.NoError(t, lb.Add(l1))
	require.NoError(t, lb.Add(l2))
	assert.Equal(t, 2, lb.Len())

	assert.True(t, lb.Delete("p1"))
	assert.Equal(t, 1, lb.Len())

	assert.False(t, lb.Delete("missing"))
	assert.Equal(t, 1, lb.Len())
}

// TestLotBookAll_ReturnsCopyNotAlias_Phase2 verifies expected behavior for this component.
func TestLotBookAll_ReturnsCopyNotAlias_Phase2(t *testing.T) {
	t.Parallel()

	lb := &LotBook{}
	l1 := &Lot{TradeCommon: &TradeCommon{ID: "p1"}}
	require.NoError(t, lb.Add(l1))

	copyMap := lb.All()
	require.Len(t, copyMap, 1)
	delete(copyMap, "p1")

	assert.Equal(t, 1, lb.Len())
	assert.NotNil(t, lb.All()["p1"])
}

// TestLotBookAll_ReturnsClonedLots verifies expected behavior for this component.
func TestLotBookAll_ReturnsClonedLots(t *testing.T) {
	t.Parallel()

	lb := &LotBook{}
	require.NoError(t, lb.Add(&Lot{TradeCommon: &TradeCommon{ID: "p1", Instrument: "EURUSD"}, OriginalUnits: 10, RemainingUnits: 10}))

	all := lb.All()
	require.Len(t, all, 1)
	all["p1"].RemainingUnits = 1

	got := lb.Get("p1")
	require.NotNil(t, got)
	assert.Equal(t, types.Units(10), got.RemainingUnits)
}

// TestLotBookRange_VisitsAllAndStopsOnError_Phase2 verifies expected behavior for this component.
func TestLotBookRange_VisitsAllAndStopsOnError_Phase2(t *testing.T) {
	t.Parallel()

	lb := &LotBook{}
	require.NoError(t, lb.Add(&Lot{TradeCommon: &TradeCommon{ID: "p1"}, EntryTime: 2}))
	require.NoError(t, lb.Add(&Lot{TradeCommon: &TradeCommon{ID: "p2"}, EntryTime: 1}))

	var seen []string
	err := lb.Range(func(lot *Lot) error {
		seen = append(seen, lot.ID)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"p2", "p1"}, seen)

	stopErr := errors.New("stop")
	err = lb.Range(func(lot *Lot) error {
		if lot.ID == "p1" || lot.ID == "p2" {
			return stopErr
		}
		return nil
	})
	assert.ErrorIs(t, err, stopErr)
}

// TestLotBookAllNilMap_Phase2 verifies expected behavior for this component.
func TestLotBookAllNilMap_Phase2(t *testing.T) {
	t.Parallel()

	lb := &LotBook{}
	assert.Nil(t, lb.All())
}

// TestLotStateString verifies expected behavior for this component.
func TestLotStateString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "none", LotNone.String())
	assert.Equal(t, "open-requested", LotOpenRequested.String())
	assert.Equal(t, "open", LotOpen.String())
	assert.Equal(t, "close-requested", LotCloseRequested.String())
	assert.Equal(t, "closed", LotClosed.String())
	assert.Equal(t, "unknown", lotState(99).String())
}

// TestLotValidate verifies expected behavior for this component.
func TestLotValidate(t *testing.T) {
	t.Parallel()

	valid := &Lot{
		TradeCommon:    &TradeCommon{ID: "p1", Instrument: "EURUSD"},
		OriginalUnits:  10,
		RemainingUnits: 5,
	}
	require.NoError(t, valid.Validate())

	assert.Error(t, (*Lot)(nil).Validate())
	assert.Error(t, (&Lot{}).Validate())
	assert.Error(t, (&Lot{TradeCommon: &TradeCommon{ID: "p1"}, OriginalUnits: 10, RemainingUnits: 10}).Validate())
	assert.Error(t, (&Lot{TradeCommon: &TradeCommon{ID: "p1", Instrument: "EURUSD"}, OriginalUnits: 0, RemainingUnits: 10}).Validate())
	assert.Error(t, (&Lot{TradeCommon: &TradeCommon{ID: "p1", Instrument: "EURUSD"}, OriginalUnits: 10, RemainingUnits: 11}).Validate())
}

// TestLotBookAddValidationAndGet verifies expected behavior for this component.
func TestLotBookAddValidationAndGet(t *testing.T) {
	t.Parallel()

	lb := &LotBook{}
	assert.Error(t, lb.Add(nil))
	assert.Error(t, lb.Add(&Lot{}))
	assert.Error(t, lb.Add(&Lot{TradeCommon: &TradeCommon{}}))

	require.NoError(t, lb.Add(&Lot{TradeCommon: &TradeCommon{ID: "p1"}}))
	assert.True(t, lb.Has("p1"))
	got := lb.Get("p1")
	require.NotNil(t, got)
	got.ID = "mutated"
	assert.Equal(t, "p1", lb.Get("p1").ID)
}
