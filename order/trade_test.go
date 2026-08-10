package order

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTradeValidOpen(t *testing.T) {
	tr, err := NewTrade(Trade{
		AccountID:    mustAccountID(t),
		Listing:      mustEurUsdListing(t),
		Side:         Long,
		EntryFillIDs: []id.FillID{mustFillID(t)},
		OpenedAt:     time.Now(),
		RealizedPnL:  num.MustParseMoney("0", num.MustParseCurrency("USD")),
	})
	require.NoError(t, err)
	assert.True(t, tr.ClosedAt.IsZero(), "still open")
}

func TestNewTradeValidClosed(t *testing.T) {
	_, err := NewTrade(Trade{
		AccountID:    mustAccountID(t),
		Listing:      mustEurUsdListing(t),
		Side:         Short,
		EntryFillIDs: []id.FillID{mustFillID(t)},
		ExitFillIDs:  []id.FillID{mustFillID(t)},
		OpenedAt:     time.Now(),
		ClosedAt:     time.Now(),
		RealizedPnL:  num.MustParseMoney("125.50", num.MustParseCurrency("USD")),
	})
	require.NoError(t, err)
}

func TestNewTradeRejectsZeroAccountID(t *testing.T) {
	_, err := NewTrade(Trade{
		Listing:      mustEurUsdListing(t),
		Side:         Long,
		EntryFillIDs: []id.FillID{mustFillID(t)},
		OpenedAt:     time.Now(),
	})
	assert.ErrorIs(t, err, ErrInvalidTrade)
}

func TestNewTradeRejectsUnconstructedListing(t *testing.T) {
	_, err := NewTrade(Trade{
		AccountID:    mustAccountID(t),
		Side:         Long,
		EntryFillIDs: []id.FillID{mustFillID(t)},
		OpenedAt:     time.Now(),
	})
	assert.ErrorIs(t, err, ErrInvalidTrade)
}

func TestNewTradeRejectsFlatSide(t *testing.T) {
	_, err := NewTrade(Trade{
		AccountID:    mustAccountID(t),
		Listing:      mustEurUsdListing(t),
		Side:         Flat,
		EntryFillIDs: []id.FillID{mustFillID(t)},
		OpenedAt:     time.Now(),
	})
	assert.ErrorIs(t, err, ErrInvalidTrade)
}

func TestNewTradeRejectsNoEntryFills(t *testing.T) {
	_, err := NewTrade(Trade{
		AccountID: mustAccountID(t),
		Listing:   mustEurUsdListing(t),
		Side:      Long,
		OpenedAt:  time.Now(),
	})
	assert.ErrorIs(t, err, ErrInvalidTrade)
}

func TestNewTradeRejectsZeroEntryFillID(t *testing.T) {
	_, err := NewTrade(Trade{
		AccountID:    mustAccountID(t),
		Listing:      mustEurUsdListing(t),
		Side:         Long,
		EntryFillIDs: []id.FillID{{}},
		OpenedAt:     time.Now(),
	})
	assert.ErrorIs(t, err, ErrInvalidTrade)
}

func TestNewTradeRejectsZeroExitFillID(t *testing.T) {
	_, err := NewTrade(Trade{
		AccountID:    mustAccountID(t),
		Listing:      mustEurUsdListing(t),
		Side:         Long,
		EntryFillIDs: []id.FillID{mustFillID(t)},
		ExitFillIDs:  []id.FillID{{}},
		OpenedAt:     time.Now(),
	})
	assert.ErrorIs(t, err, ErrInvalidTrade)
}

func TestNewTradeRejectsClosedAtBeforeOpenedAt(t *testing.T) {
	opened := time.Now()
	_, err := NewTrade(Trade{
		AccountID:    mustAccountID(t),
		Listing:      mustEurUsdListing(t),
		Side:         Long,
		EntryFillIDs: []id.FillID{mustFillID(t)},
		OpenedAt:     opened,
		ClosedAt:     opened.Add(-time.Hour),
	})
	assert.ErrorIs(t, err, ErrInvalidTrade)
}

func TestNewTradeAllowsOpenTradeWithPartialExitFillsAndNoClosedAt(t *testing.T) {
	_, err := NewTrade(Trade{
		AccountID:    mustAccountID(t),
		Listing:      mustEurUsdListing(t),
		Side:         Long,
		EntryFillIDs: []id.FillID{mustFillID(t)},
		ExitFillIDs:  []id.FillID{mustFillID(t)},
		OpenedAt:     time.Now(),
	})
	require.NoError(t, err)
}

func TestNewTradeRejectsZeroOpenedAt(t *testing.T) {
	_, err := NewTrade(Trade{
		AccountID:    mustAccountID(t),
		Listing:      mustEurUsdListing(t),
		Side:         Long,
		EntryFillIDs: []id.FillID{mustFillID(t)},
	})
	assert.ErrorIs(t, err, ErrInvalidTrade)
}
