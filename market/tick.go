package market

import (
	"fmt"
)

// BA represents a trader domain type.
type BA struct {
	Bid Price
	Ask Price
}

// Validate is an internal helper for trader type processing.
func (ba BA) Validate() error {
	if ba.Bid <= 0 {
		return fmt.Errorf("bid must be > 0")
	}
	if ba.Ask <= 0 {
		return fmt.Errorf("ask must be > 0")
	}
	if ba.Ask < ba.Bid {
		return fmt.Errorf("ask must be >= bid")
	}
	return nil
}

// Mid returns the midpoint rounded half-up to the nearest scaled price unit.
func (ba BA) Mid() Price {
	sum := int64(ba.Bid) + int64(ba.Ask)
	return Price((sum + 1) / 2)
}

func (ba BA) Spread() Price {
	return ba.Ask - ba.Bid
}

// Tick represents a trader domain type.
type Tick struct {
	Instrument string
	Timestamp  Timestamp
	BA
}

// Validate is an internal helper for trader type processing.
func (t Tick) Validate() error {
	if NormalizeInstrument(t.Instrument) == "" {
		return fmt.Errorf("tick instrument must not be empty")
	}
	return t.BA.Validate()
}

// Mid is an internal helper for trader type processing.
func (t Tick) Mid() Price {
	return t.BA.Mid()
}
