package trader

import (
	"errors"
	"fmt"
	"slices"

	instr "github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// errUnsupportedInstrument reports a catalog entry the engine cannot value yet:
// a kind other than a currency pair, a settlement currency different from the
// quote currency, or a contract multiplier other than 1 (ADR-088).
var errUnsupportedInstrument = errors.New("instrument not supported")

// errNotTradable reports an order for a market whose listing is not tradable.
// It is a configuration error, not a trading outcome.
var errNotTradable = errors.New("instrument is not tradable")

// errCatalog reports a catalog that is inconsistent: a conflicting mapping
// between engine keys, symbols and instrument IDs, or invalid trading rules.
var errCatalog = errors.New("invalid instrument catalog")

// tradingRules are the broker and account policy for one instrument: not
// properties of the economic Instrument, and not of its Spec.
type tradingRules struct {
	marginRate num.Rate
	minUnits   num.Quantity // smallest tradable size; zero means one unit
	maxUnits   num.Quantity // largest order size; zero means no limit
}

// marketConfig is what a catalog entry is built from: one engine key, the
// economic instrument, how a venue lists it, and its trading rules.
type marketConfig struct {
	key        string
	instrument instr.Instrument
	listing    instr.Listing
	rules      tradingRules
}

// market is one validated catalog entry. Its key is the one string the engine
// uses for the instrument: in bars, orders, positions, queues and reports.
type market struct {
	key        string
	instrument instr.Instrument
	listing    instr.Listing
	rules      tradingRules

	quote    num.Currency // the currency prices and profit are denominated in
	tick     num.Price    // from the listing's Spec
	unitStep num.Quantity // from the listing's Spec
}

// catalog is the per-run set of markets, built once and never changed. It has
// no global state, and a run that needs another instrument builds its own.
// Within a run one instrument has exactly one engine key (ADR-088).
type catalog struct {
	byKey    map[string]*market
	bySymbol map[string]*market // engine key or the listing's provider symbol
}

// newCatalog validates the entries and returns the catalog, or an error if any
// entry is unsupported or the entries conflict: the same instrument twice
// (whatever its keys), the same key twice, a listing for a different
// instrument, or a symbol, whether an engine key or a provider symbol,
// claimed by two entries. Conflicts are found here, when the run is
// configured, not when an order is submitted.
func newCatalog(entries ...marketConfig) (*catalog, error) {
	c := &catalog{byKey: make(map[string]*market), bySymbol: make(map[string]*market)}
	byID := make(map[instr.ID]*market)
	for _, e := range entries {
		m, err := newMarket(e)
		if err != nil {
			return nil, err
		}
		id := m.instrument.ID()
		if other, dup := byID[id]; dup {
			return nil, fmt.Errorf("%w: %s is configured twice, as %q and %q", errCatalog, id, other.key, m.key)
		}
		byID[id] = m
		c.byKey[m.key] = m
		for _, symbol := range []string{m.key, m.listing.Symbol()} {
			if other, taken := c.bySymbol[symbol]; taken && other != m {
				return nil, fmt.Errorf("%w: symbol %q is claimed by %q and %q", errCatalog, symbol, other.key, m.key)
			}
			c.bySymbol[symbol] = m
		}
	}
	return c, nil
}

func newMarket(e marketConfig) (*market, error) {
	if e.key == "" {
		return nil, fmt.Errorf("%w: an entry has no engine key", errCatalog)
	}
	id := e.instrument.ID()
	if id.IsZero() {
		return nil, fmt.Errorf("%w: %q has no instrument", errCatalog, e.key)
	}
	if !e.listing.InstrumentID().Equal(id) {
		return nil, fmt.Errorf("%w: %q lists %s, not its instrument %s", errCatalog, e.key, e.listing.InstrumentID(), id)
	}

	// The supported valuation contract (ADR-088): a currency pair settling in
	// its quote currency, with a multiplier of 1.
	quote, ok := e.instrument.Quote()
	spec := e.listing.Spec()
	if !ok {
		return nil, fmt.Errorf("%w: %q is a %s; only currency pairs are valued", errUnsupportedInstrument, e.key, e.instrument.Kind())
	}
	if !spec.SettlementCurrency().Equal(quote) {
		return nil, fmt.Errorf("%w: %q settles in %s, not its quote currency %s", errUnsupportedInstrument, e.key, spec.SettlementCurrency(), quote)
	}
	if spec.Multiplier().Cmp(rateOne) != 0 {
		return nil, fmt.Errorf("%w: %q has a contract multiplier of %s; only 1 is valued", errUnsupportedInstrument, e.key, spec.Multiplier())
	}

	r := e.rules
	if r.marginRate.Sign() <= 0 {
		return nil, fmt.Errorf("%w: %q needs a positive margin rate", errCatalog, e.key)
	}
	if !r.maxUnits.IsZero() && r.minUnits.Cmp(r.maxUnits) > 0 {
		return nil, fmt.Errorf("%w: %q has a minimum of %s above its maximum of %s", errCatalog, e.key, r.minUnits, r.maxUnits)
	}
	return &market{
		key: e.key, instrument: e.instrument, listing: e.listing, rules: r,
		quote: quote, tick: spec.TickSize(), unitStep: spec.QuantityIncrement(),
	}, nil
}

// lookup is the market under exactly this engine key. There is no fallback to
// another spelling: a key the catalog does not hold is not an instrument.
func (c *catalog) lookup(key string) (*market, bool) {
	m, ok := c.byKey[key]
	return m, ok
}

// keyFor is the engine key of the market a symbol names: the key itself or the
// listing's provider symbol. A symbol no market claims is an error.
func (c *catalog) keyFor(symbol string) (string, error) {
	m, ok := c.bySymbol[symbol]
	if !ok {
		return "", fmt.Errorf("%w: %q", errUnknownInstrument, symbol)
	}
	return m.key, nil
}

// checkPrices reports an error unless every price of b is a whole number of
// m's ticks.
func (m *market) checkPrices(b ohlc) error {
	for _, p := range []num.Price{b.o, b.h, b.l, b.c} {
		if ok, err := p.DivisibleBy(m.tick); err != nil || !ok {
			return fmt.Errorf("%s price %s is not a multiple of the tick %s", m.key, p, m.tick)
		}
	}
	return nil
}

// checkOrderPrices reports an error unless the open's reference or trigger
// price, stop, optional target and trailing distance are each a whole number of
// the market's ticks. Off-tick values are refused, never rounded: rounding would
// change the strategy's risk, and an off-tick stop could execute at a price the
// market cannot trade.
func (m *market) checkOrderPrices(o *order) error {
	for name, p := range map[string]num.Price{"price": o.price, "stop loss": o.stopLoss, "take profit": o.takeProfit, "trail": o.trail} {
		if p.IsZero() {
			continue // not set
		}
		if ok, err := p.DivisibleBy(m.tick); err != nil || !ok {
			return fmt.Errorf("%w: %s %s is not a multiple of %s's tick %s", errInvalidOrder, name, p, m.key, m.tick)
		}
	}
	return nil
}

// checkFrame is the engine's boundary check: every bar of the frame must carry
// exactly a configured engine key, with prices on that market's tick. A
// provider spelling is an error here, not a second instrument, so a bar that
// was not normalized can never be silently missed by the queues and positions
// held under the engine key. It changes nothing.
func (c *catalog) checkFrame(f frame) error {
	for _, b := range f.bars {
		m, ok := c.byKey[b.instrument]
		if !ok {
			return fmt.Errorf("%w: a bar for %q is not under an engine key; normalize the bars first", errUnknownInstrument, b.instrument)
		}
		if err := m.checkPrices(b.ohlc); err != nil {
			return fmt.Errorf("bar at %s: %w", b.Format(barTimeLayout), err)
		}
	}
	return nil
}

// normalize rewrites loaded bars to the engine key of their instrument, before
// the engine sees them, and checks every price is a whole number of the
// market's ticks. A provider spelling therefore never reaches the engine as a
// second instrument. Every bar is checked before any is changed, so a refused
// set is left exactly as it was.
func (c *catalog) normalize(md *marketData) error {
	key, err := c.keyFor(md.instrument)
	if err != nil {
		return fmt.Errorf("bars: %w", err)
	}
	m := c.byKey[key]
	for _, b := range md.data {
		if b.instrument != "" {
			if bk, err := c.keyFor(b.instrument); err != nil || bk != key {
				return fmt.Errorf("bars: a %q bar is in the %q set", b.instrument, key)
			}
		}
		if err := m.checkPrices(b.ohlc); err != nil {
			return fmt.Errorf("bars: %w", err)
		}
	}
	md.instrument = key
	for i := range md.data {
		md.data[i].instrument = key
	}
	return nil
}

// keys lists the engine keys in a fixed order.
func (c *catalog) keys() []string {
	keys := make([]string, 0, len(c.byKey))
	for k := range c.byKey {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// marginFor is the margin units of the market under key need at price p:
// (p * units) * marginRate. The whole-unit product is exact, so the only
// rounding is the one at the end, on a value of account size (ADR-087). Sizing
// refuses an unknown market, so only a bug can reach here with one.
func (c *catalog) marginFor(key string, units num.Quantity, p num.Price, currency num.Currency) (num.Money, error) {
	m, ok := c.byKey[key]
	if !ok {
		panic(fmt.Sprintf("margin for %q: %v", key, errUnknownInstrument))
	}
	notional, err := p.MulQuantity(units, currency)
	if err != nil {
		return num.Money{}, fmt.Errorf("margin for %s: %w", key, err)
	}
	margin, err := notional.MulRate(m.rules.marginRate)
	if err != nil {
		return num.Money{}, fmt.Errorf("margin for %s: %w", key, err)
	}
	return margin, nil
}

// defaultCatalog is the simulated broker's markets, the ones the EMAX scenarios
// were worked out for: EURUSD at 50:1 and GBPUSD at 20:1, one unit step, USD.
func defaultCatalog() (*catalog, error) {
	usd := usdCurrency
	pair := func(base, quote string, margin num.Rate) (marketConfig, error) {
		ins, err := instr.NewCurrencyPair(num.MustParseCurrency(base), num.MustParseCurrency(quote))
		if err != nil {
			return marketConfig{}, err
		}
		spec, err := instr.NewSpec(num.MustParsePrice("0.00001"), oneUnit, rateOne, usd)
		if err != nil {
			return marketConfig{}, err
		}
		key := base + quote
		listing, err := instr.NewListing(instr.ListingParams{
			Instrument: ins, Provider: "SIM", Symbol: key, Spec: spec, Tradable: true,
		})
		if err != nil {
			return marketConfig{}, err
		}
		return marketConfig{key: key, instrument: ins, listing: listing,
			rules: tradingRules{marginRate: margin, minUnits: oneUnit}}, nil
	}
	eur, err := pair("EUR", "USD", rateTwoPercent)
	if err != nil {
		return nil, err
	}
	gbp, err := pair("GBP", "USD", rateFivePercent)
	if err != nil {
		return nil, err
	}
	return newCatalog(eur, gbp)
}
