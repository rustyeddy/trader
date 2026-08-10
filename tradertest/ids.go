package tradertest

import "github.com/rustyeddy/trader/id"

// MustAccountID generates a new id.AccountID from g, panicking on error.
// g is a caller-owned *id.Generator (see the package doc comment for
// why tradertest does not construct one for you) — typically
// id.NewGenerator(clock.NewSimulated(start), id.NewDeterministic(seed1, seed2))
// for deterministic tests.
func MustAccountID(g *id.Generator) id.AccountID {
	v, err := id.GenerateAccountID(g)
	if err != nil {
		panic(err)
	}
	return v
}

// MustOrderID generates a new id.OrderID from g, panicking on error.
func MustOrderID(g *id.Generator) id.OrderID {
	v, err := id.GenerateOrderID(g)
	if err != nil {
		panic(err)
	}
	return v
}

// MustFillID generates a new id.FillID from g, panicking on error.
func MustFillID(g *id.Generator) id.FillID {
	v, err := id.GenerateFillID(g)
	if err != nil {
		panic(err)
	}
	return v
}
