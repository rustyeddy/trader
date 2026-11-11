package backtestcfg

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/rustyeddy/trader/instrument"
	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
)

// instrumentSet is one canonically resolved, de-duplicated, order-
// independent set of requested instruments: the CLI's own vocabulary
// (a --symbol string) resolved to instrument.ID once, then sorted by
// that ID's own canonical string form so that "--symbol GBPUSD
// --symbol EURUSD" and "--symbol EURUSD --symbol GBPUSD" produce the
// identical requirement/price ordering — flag order must never become
// semantically meaningful (issue #224 review, point 3), even though
// backtest.NewManifest's own Universe canonicalization would catch it
// one layer down regardless.
type instrumentSet struct {
	ids          []instrument.ID
	byKey        map[string]instrument.ID      // keyed by instrument.ID.String()
	oandaListing map[string]instrument.Listing // keyed by instrument.ID.String()
	simListing   map[string]instrument.Listing
}

// symbol is id's data-provider symbol, for messages.
func (s instrumentSet) symbol(id instrument.ID) string {
	return s.oandaListing[id.String()].Symbol()
}

// newSimResolver returns the simulated broker's own resolver, which the
// run registers each instrument into under provider "sim".
func newSimResolver() *instrument.MemoryResolver { return instrument.NewMemoryResolver() }

// resolveInstrumentSet parses flags.symbols into a canonical
// instrumentSet: each symbol is registered under both the oanda-side
// resolver (Manager's own bar-fetching resolver) and the sim-side
// resolver (the broker-side resolver Runner's default InputBuilder
// resolves order submissions through — two distinct providers for the
// same economic instrument, matching ADR-016's own separation),
// rejecting an explicit duplicate symbol as a clear CLI validation
// error (issue #224 review, point 3) rather than letting two identical
// DataRequirements reach Replay/Runner and fail deeper in the stack.
func resolveInstrumentSet(symbols []string, provider string, oandaResolver, simResolver *instrument.MemoryResolver) (instrumentSet, error) {
	if len(symbols) == 0 {
		return instrumentSet{}, fmt.Errorf("at least one --symbol is required")
	}

	set := instrumentSet{
		byKey:        make(map[string]instrument.ID, len(symbols)),
		oandaListing: make(map[string]instrument.Listing, len(symbols)),
		simListing:   make(map[string]instrument.Listing, len(symbols)),
	}
	seen := make(map[string]string, len(symbols)) // normalized symbol -> itself, recorded once seen so a duplicate can report it

	for _, raw := range symbols {
		symbol := strings.ToUpper(strings.TrimSpace(raw))

		// A repeated --symbol is a CLI validation error (issue #224
		// review, point 3), checked against the normalized symbol before
		// either resolver is touched. Registration itself is idempotent
		// (issue #448), but two identical DataRequirements would still
		// fail deeper in Replay/Runner.
		if first, dup := seen[symbol]; dup {
			return instrumentSet{}, fmt.Errorf("duplicate --symbol %q: already requested as %q", symbol, first)
		}
		seen[symbol] = symbol

		// One shared identification (issue #448), registered under both
		// the data provider and the simulated broker.
		identity, err := svcmarketdata.IdentifyInstrument(provider, svcmarketdata.InstrumentRequest{Symbol: symbol})
		if errors.Is(err, svcmarketdata.ErrNoListingDefault) {
			return instrumentSet{}, fmt.Errorf("unsupported equity %q for provider %q: add reference metadata before backtesting it", symbol, provider)
		}
		if err != nil {
			return instrumentSet{}, err
		}
		oandaListing, err := svcmarketdata.RegisterIdentity(oandaResolver, provider, identity)
		if err != nil {
			return instrumentSet{}, err
		}
		simListing, err := svcmarketdata.RegisterIdentity(simResolver, "sim", identity)
		if err != nil {
			return instrumentSet{}, err
		}
		instrumentID := oandaListing.InstrumentID()

		key := instrumentID.String()
		set.ids = append(set.ids, instrumentID)
		set.byKey[key] = instrumentID
		set.oandaListing[key] = oandaListing
		set.simListing[key] = simListing
	}

	sort.Slice(set.ids, func(i, j int) bool { return set.ids[i].String() < set.ids[j].String() })
	return set, nil
}
