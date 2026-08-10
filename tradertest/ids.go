package tradertest

import "github.com/rustyeddy/trader/id"

// MustAccountID, MustOrderID, and MustFillID are panic adapters over
// id.GenerateAccountID/GenerateOrderID/GenerateFillID — nothing more.
// They do not construct, own, or configure a *id.Generator; g is
// entirely caller-owned, built the same way any Trader consumer builds
// one: id.NewGenerator(clock.NewSimulated(start),
// id.NewDeterministic(seed1, seed2)) for deterministic tests. These
// exist only because every one of order, account, and portfolio's own
// M1 tests independently wrote the identical
// "generate-or-fail-the-test" wrapper around those three calls; they
// are not a reason to stop calling id.Generate* directly when a caller
// wants to handle the (rare) error itself instead of panicking.

// MustAccountID generates a new id.AccountID from g, panicking on error.
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
