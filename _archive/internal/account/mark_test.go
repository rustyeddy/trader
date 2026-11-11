package account

import (
	"testing"
	"time"

	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoPositionParams is validParams with open EUR/USD and GBP/USD
// positions, in that order.
func twoPositionParams(t *testing.T) SnapshotParams {
	t.Helper()
	p := validParams(t)
	gbp := mustListing(t, "GBP", "USD", "OANDA", "GBP_USD")
	p.Positions = append(p.Positions, mustPosition(t, p.AccountID, gbp))
	return p
}

func markFor(p runtimeorder.Position, price string, at time.Time) PositionMark {
	return PositionMark{Listing: KeyOf(p.Listing), Price: num.MustParsePrice(price), AsOf: at}
}

func TestSnapshotMarks(t *testing.T) {
	p := twoPositionParams(t)
	earlier := p.AsOf.Add(-time.Hour)
	eurMark := markFor(p.Positions[0], "1.1", p.AsOf)
	gbpMark := markFor(p.Positions[1], "1.3", earlier)
	// Supplied out of Positions order; returned in Positions order.
	p.Marks = []PositionMark{gbpMark, eurMark}

	s, err := NewSnapshot(p)
	require.NoError(t, err)
	assert.Equal(t, []PositionMark{eurMark, gbpMark}, s.Marks())

	got, ok := s.Mark(KeyOf(p.Positions[1].Listing))
	require.True(t, ok)
	assert.Equal(t, gbpMark, got, "each mark keeps its own AsOf")

	// Marks returns a copy.
	s.Marks()[0].Price = num.MustParsePrice("99")
	assert.Equal(t, eurMark, s.Marks()[0])
}

func TestSnapshotMarksOptional(t *testing.T) {
	p := twoPositionParams(t)
	p.Marks = []PositionMark{markFor(p.Positions[1], "1.3", p.AsOf)}
	s, err := NewSnapshot(p)
	require.NoError(t, err)
	_, ok := s.Mark(KeyOf(p.Positions[0].Listing))
	assert.False(t, ok, "a position may have no mark")

	s, err = NewSnapshot(validParams(t))
	require.NoError(t, err)
	assert.Empty(t, s.Marks())
}

func TestSnapshotRejectsInvalidMarks(t *testing.T) {
	other := mustListing(t, "AUD", "USD", "OANDA", "AUD_USD")
	cases := []struct {
		name   string
		mutate func(p *SnapshotParams)
	}{
		{"no open position in listing", func(p *SnapshotParams) {
			p.Marks = []PositionMark{{Listing: KeyOf(other), Price: num.MustParsePrice("1"), AsOf: p.AsOf}}
		}},
		{"duplicate", func(p *SnapshotParams) {
			m := markFor(p.Positions[0], "1", p.AsOf)
			p.Marks = []PositionMark{m, m}
		}},
		{"zero price", func(p *SnapshotParams) {
			p.Marks = []PositionMark{{Listing: KeyOf(p.Positions[0].Listing), AsOf: p.AsOf}}
		}},
		{"zero as-of", func(p *SnapshotParams) {
			p.Marks = []PositionMark{{Listing: KeyOf(p.Positions[0].Listing), Price: num.MustParsePrice("1")}}
		}},
		{"as-of after snapshot", func(p *SnapshotParams) {
			p.Marks = []PositionMark{markFor(p.Positions[0], "1", p.AsOf.Add(time.Second))}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validParams(t)
			tc.mutate(&p)
			_, err := NewSnapshot(p)
			assert.ErrorIs(t, err, ErrInvalidSnapshot)
		})
	}
}

func TestKeyOf(t *testing.T) {
	l := mustEurUsdListing(t)
	k := KeyOf(l)
	assert.Equal(t, l.InstrumentID(), k.InstrumentID)
	assert.Equal(t, l.Provider(), k.Provider)
	assert.Equal(t, l.Venue(), k.Venue)
}
