package datamanager

import (
	"fmt"
)

// ErrStatMissingInstrument is the Value field in the error Stat returned when
// an analyzer has no instrument configured. Use this constant instead of a
// bare string literal when checking whether a Stat signals a missing instrument.
const ErrStatMissingInstrument = "missing instrument"

func missingInstrumentStats() []Stat {
	return []Stat{{Name: "error", Value: ErrStatMissingInstrument}}
}

func pipStat(name string, pips float64, precision int) Stat {
	return Stat{
		Name:  name,
		Value: fmt.Sprintf("%.*f pips", precision, pips),
		Pips:  pips,
	}
}
