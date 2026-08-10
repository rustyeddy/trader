package portfolio

import (
	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/order"
)

// AccountPosition pairs one account's Position with the account it
// belongs to, preserving provenance inside an Exposure.
type AccountPosition struct {
	AccountID id.AccountID
	Position  order.Position
}

// Exposure groups every account's Position in one economic instrument
// (instrument.ID), across every venue and broker it is held through.
// See the package doc comment for why this is a provenance-preserving
// grouping, not a summed notional or quantity.
type Exposure struct {
	instrumentID instrument.ID
	contributors []AccountPosition
}

// InstrumentID identifies the economic instrument this Exposure groups.
func (e Exposure) InstrumentID() instrument.ID { return e.instrumentID }

// Contributors returns a deep copy of the account positions grouped
// under this Exposure. Mutating the returned slice, or any *num.Price a
// Position reaches through AvgPrice, does not affect e.
func (e Exposure) Contributors() []AccountPosition {
	cloned := make([]AccountPosition, len(e.contributors))
	for i, c := range e.contributors {
		cloned[i] = AccountPosition{AccountID: c.AccountID, Position: clonePosition(c.Position)}
	}
	return cloned
}

// clonePosition returns a copy of p that shares no pointer state with
// it, mirroring account's identically named, independently maintained
// helper: portfolio has no reason to depend on account's unexported
// implementation details for this.
func clonePosition(p order.Position) order.Position {
	cloned := p
	if p.AvgPrice != nil {
		v := *p.AvgPrice
		cloned.AvgPrice = &v
	}
	return cloned
}

// buildExposures groups every position across accounts by
// instrument.ID, preserving each account's Positions() order and the
// order accounts were supplied in.
func buildExposures(accounts []account.Snapshot) []Exposure {
	instrumentOrder := make([]instrument.ID, 0)
	byInstrument := make(map[instrument.ID][]AccountPosition)

	for _, snapshot := range accounts {
		for _, pos := range snapshot.Positions() {
			iid := pos.Listing.InstrumentID()
			if _, ok := byInstrument[iid]; !ok {
				instrumentOrder = append(instrumentOrder, iid)
			}
			byInstrument[iid] = append(byInstrument[iid], AccountPosition{
				AccountID: snapshot.AccountID(),
				Position:  pos,
			})
		}
	}

	exposures := make([]Exposure, 0, len(instrumentOrder))
	for _, iid := range instrumentOrder {
		exposures = append(exposures, Exposure{instrumentID: iid, contributors: byInstrument[iid]})
	}
	return exposures
}
