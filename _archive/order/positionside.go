package order

import "fmt"

// PositionSide is a position's holding direction: Flat, Long, or Short.
// It is deliberately a separate type from Side: an order Side is a
// transaction direction (what an order does), while PositionSide is a
// holding direction (what an account has), and Flat has no order-side
// equivalent. Flat is PositionSide's zero value, so an unconstructed
// Position is safely flat rather than silently long or short.
type PositionSide uint8

const (
	// Flat means no net exposure.
	Flat PositionSide = iota
	// Long means net exposure is held via buying.
	Long
	// Short means net exposure is held via selling.
	Short
)

// String returns a human-readable PositionSide name.
func (s PositionSide) String() string {
	switch s {
	case Flat:
		return "flat"
	case Long:
		return "long"
	case Short:
		return "short"
	default:
		return fmt.Sprintf("PositionSide(%d)", uint8(s))
	}
}

// Valid reports whether the value belongs to the defined vocabulary.
func (s PositionSide) Valid() bool {
	switch s {
	case Flat, Long, Short:
		return true
	default:
		return false
	}
}
