package account

import "github.com/rustyeddy/trader/instrument"

// ListingKey identifies one listing within an account: an economic
// instrument together with the provider and venue that list it.
//
// It is the identity at which an account holds one net position
// (runtimeorder.Position is one position per account/listing pair), and
// at which a listing is valued. Two listings of the same instrument
// from different providers or venues are different keys: they can
// carry different marks, multipliers, and settlement terms.
type ListingKey struct {
	InstrumentID instrument.ID
	Provider     string
	Venue        string
}

// KeyOf returns l's ListingKey.
func KeyOf(l instrument.Listing) ListingKey {
	return ListingKey{InstrumentID: l.InstrumentID(), Provider: l.Provider(), Venue: l.Venue()}
}
